//go:build integration

package main

import (
	"context"
	"strings"
	"testing"
)

// makePayoutDue moves a booking so it ended 3.5 hours ago: past the two-hour
// problem window and the three-hour payout time.
func makePayoutDue(t *testing.T, bookingID string) {
	t.Helper()
	moveBookingEnd(t, bookingID, "3 hours 30 minutes")
}

// moveBookingEnd makes a 30-minute booking end the given interval ago.
func moveBookingEnd(t *testing.T, bookingID, ago string) {
	t.Helper()
	if _, err := itPool.Exec(context.Background(), `UPDATE bookings SET starts_at=now()-interval '30 minutes'-$2::interval,duration_minutes=30 WHERE id=$1`, bookingID, ago); err != nil {
		t.Fatal(err)
	}
}

func payoutJournalBalanced(t *testing.T, bookingID string) bool {
	t.Helper()
	return scalar[bool](t, `SELECT COALESCE(sum(CASE WHEN e.side='debit' THEN e.amount_minor ELSE -e.amount_minor END),1)=0 FROM ledger_entries e JOIN ledger_journals j ON j.id=e.journal_id JOIN seller_payouts po ON j.source_id=po.id::text||':'||po.reference WHERE j.source_type='payout' AND po.booking_id=$1`, bookingID)
}

func TestPayoutWaitsForTheDisputeWindowAndHoldsOnProblems(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	s := h.newSeller("fixed")
	buyer, bookingID, _ := paidBooking(t, h, fake, s, h.slotOn(s.handle, 1, 0))
	if got := scalar[string](t, `SELECT state||'/'||entitlement_minor FROM seller_payouts WHERE booking_id=$1`, bookingID); got != "scheduled/950000" {
		t.Fatalf("payout %s", got)
	}
	if got := scalar[string](t, `SELECT pa.settlement_route||'/'||si.state FROM payment_allocations pa JOIN settlement_items si ON si.allocation_id=pa.id WHERE pa.booking_id=$1`, bookingID); got != "approved_transfer/payout_scheduled" {
		t.Fatalf("settlement %s", got)
	}
	// An hour after the session: still inside the buyer's window.
	moveBookingEnd(t, bookingID, "1 hour")
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.transferCalls != 0 {
		t.Fatal("nothing may be paid before the payout time")
	}
	detail := buyer.expect(200, "GET", "/api/v1/bookings/"+bookingID, nil)
	if detail["can_report_problem"] != true {
		t.Fatalf("buyer should be able to report a problem: %v", detail["can_report_problem"])
	}
	buyer.expect(201, "POST", "/api/v1/bookings/"+bookingID+"/issue", map[string]string{"reason": "The seller left after ten minutes"})
	findMessage(t, h.deliverDue(s.client.email), "reported a problem")
	// Past the payout time, but the problem holds it.
	makePayoutDue(t, bookingID)
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.transferCalls != 0 {
		t.Fatal("a reported problem must hold the payout")
	}
	sellerView := s.client.expect(200, "GET", "/api/v1/bookings/"+bookingID, nil)["payout"].(map[string]any)
	if sellerView["hold"] != "problem_reported" {
		t.Fatalf("seller payout view %v", sellerView)
	}
	// WantMyTime refunds the buyer part of the payment; the rest is paid out.
	ops, _ := h.operator()
	ops.expect(200, "POST", "/api/v1/ops/bookings/"+bookingID+"/resolve-issue", map[string]any{"resolution": "Seller confirmed the call was cut short; partial refund.", "refund_minor": 300000})
	if got := scalar[string](t, `SELECT reason||'/'||seller_liability||'/'||seller_share_minor FROM refunds WHERE booking_id=$1`, bookingID); got != "problem_upheld/payable/285000" {
		t.Fatalf("refund %s", got)
	}
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state||'/'||amount_minor||'/'||fee_minor FROM seller_payouts WHERE booking_id=$1`, bookingID); got != "paid/665000/2500" {
		t.Fatalf("payout %s", got)
	}
	reference := scalar[string](t, `SELECT reference FROM seller_payouts WHERE booking_id=$1`, bookingID)
	fake.mu.Lock()
	sent := fake.transfers[reference]
	fake.mu.Unlock()
	if sent.amount != 665000 || sent.bank != "058" || sent.account != "0123456789" {
		t.Fatalf("transfer %+v", sent)
	}
	if !payoutJournalBalanced(t, bookingID) {
		t.Fatal("payout journal must balance")
	}
	if got := scalar[string](t, `SELECT si.state FROM payment_allocations pa JOIN settlement_items si ON si.allocation_id=pa.id WHERE pa.booking_id=$1`, bookingID); got != "paid_out" {
		t.Fatalf("settlement %s", got)
	}
	all := h.deliverDue("")
	var sellerMsgs, buyerMsgs []emailMessage
	for _, m := range all {
		if m.To == s.client.email {
			sellerMsgs = append(sellerMsgs, m)
		} else if m.To == buyer.email {
			buyerMsgs = append(buyerMsgs, m)
		}
	}
	if msg := findMessage(t, sellerMsgs, "is on its way to you"); !strings.Contains(msg.Text, "₦6,650") || !strings.Contains(msg.Text, "••6789") {
		t.Fatalf("payout email:\n%s", msg.Text)
	}
	if msg := findMessage(t, buyerMsgs, "Update on the reported problem"); !strings.Contains(msg.Text, "Your refund: ₦3,000") {
		t.Fatalf("resolution email:\n%s", msg.Text)
	}
	// Running again never pays twice.
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.transferCalls != 1 {
		t.Fatalf("transfers sent %d", fake.transferCalls)
	}
}

func TestProblemWindowCloses(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	s := h.newSeller("fixed")
	buyer, bookingID, _ := paidBooking(t, h, fake, s, h.slotOn(s.handle, 1, 0))
	moveBookingEnd(t, bookingID, "2 hours 5 minutes")
	res := buyer.do("POST", "/api/v1/bookings/"+bookingID+"/issue", map[string]string{"reason": "It was not what I expected"})
	if res.Status != 409 || !strings.Contains(string(res.Body), "PROBLEM_WINDOW_CLOSED") {
		t.Fatalf("late problem: %d %s", res.Status, res.Body)
	}
	if res = buyer.do("POST", "/api/v1/bookings/"+bookingID+"/no-show", "{}"); res.Status != 409 {
		t.Fatalf("late no-show: %d %s", res.Status, res.Body)
	}
	if detail := buyer.expect(200, "GET", "/api/v1/bookings/"+bookingID, nil); detail["can_report_problem"] != false {
		t.Fatal("the problem window should show as closed")
	}
	// A seller can still raise something with WantMyTime, and it doesn't hold their own payout.
	s.client.expect(201, "POST", "/api/v1/bookings/"+bookingID+"/issue", map[string]string{"reason": "Buyer asked for a refund by email"})
	makePayoutDue(t, bookingID)
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state FROM seller_payouts WHERE booking_id=$1`, bookingID); got != "paid" {
		t.Fatalf("payout %s", got)
	}
}

func TestPayoutWaitsForFundsAndForABankAccount(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	s := h.newSeller("fixed")
	_, bookingID, _ := paidBooking(t, h, fake, s, h.slotOn(s.handle, 1, 0))
	makePayoutDue(t, bookingID)
	// The buyer's payment hasn't settled into the balance yet.
	fake.mu.Lock()
	fake.balanceShort = true
	fake.mu.Unlock()
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state||'/'||last_error FROM seller_payouts WHERE booking_id=$1`, bookingID); !strings.HasPrefix(got, "processing/Waiting for the buyer's payment to settle") {
		t.Fatalf("payout %s", got)
	}
	if due := scalar[float64](t, `SELECT extract(epoch FROM next_attempt_at-now())/60 FROM seller_payouts WHERE booking_id=$1`, bookingID); due < 14 || due > 16 {
		t.Fatalf("retry in %.1f minutes, want about 15", due)
	}
	fake.mu.Lock()
	fake.balanceShort = false
	fake.mu.Unlock()
	if _, err := itPool.Exec(context.Background(), `UPDATE seller_payouts SET next_attempt_at=now() WHERE booking_id=$1`, bookingID); err != nil {
		t.Fatal(err)
	}
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state FROM seller_payouts WHERE booking_id=$1`, bookingID); got != "paid" {
		t.Fatalf("payout %s", got)
	}

	// A seller without a bank account waits instead of failing.
	s2 := h.newSeller("fixed")
	_, second, _ := paidBooking(t, h, fake, s2, h.slotOn(s2.handle, 1, 1))
	if _, err := itPool.Exec(context.Background(), `DELETE FROM seller_payout_accounts WHERE seller_id=(SELECT seller_id FROM bookings WHERE id=$1)`, second); err != nil {
		t.Fatal(err)
	}
	makePayoutDue(t, second)
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state||'/'||last_error FROM seller_payouts WHERE booking_id=$1`, second); got != "scheduled/Waiting for the seller to add a bank account." {
		t.Fatalf("payout %s", got)
	}
}

func TestRefundAfterPayoutIsRecoveredFromTheNextPayout(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	t.Setenv("REFUNDS_ENABLED", "true")
	fake.refundStatus = "success"
	s := h.newSeller("fixed")
	_, first, _ := paidBooking(t, h, fake, s, h.slotOn(s.handle, 1, 0))
	makePayoutDue(t, first)
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	// After the seller has been paid, WantMyTime refunds the buyer in full.
	ops, _ := h.operator()
	ops.expect(201, "POST", "/api/v1/ops/bookings/"+first+"/refund", map[string]any{"amount_minor": 1000000, "reason": "Chargeback threat; refunded as goodwill"})
	if err := h.api.processRefunds(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state||'/'||seller_liability FROM refunds WHERE booking_id=$1`, first); got != "processed/receivable" {
		t.Fatalf("refund %s", got)
	}
	if owed := s.client.expect(200, "GET", "/api/v1/me/refund-recoveries", nil); owed["outstanding_minor"] != float64(950000) {
		t.Fatalf("owed %v", owed)
	}
	// The next payout sends half and keeps half towards the debt.
	_, second, _ := paidBooking(t, h, fake, s, h.slotOn(s.handle, 1, 2))
	makePayoutDue(t, second)
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state||'/'||amount_minor||'/'||recovery_minor FROM seller_payouts WHERE booking_id=$1`, second); got != "paid/950000/475000" {
		t.Fatalf("payout %s", got)
	}
	reference := scalar[string](t, `SELECT reference FROM seller_payouts WHERE booking_id=$1`, second)
	fake.mu.Lock()
	sent := fake.transfers[reference].amount
	fake.mu.Unlock()
	if sent != 475000 {
		t.Fatalf("transferred %d", sent)
	}
	if owed := s.client.expect(200, "GET", "/api/v1/me/refund-recoveries", nil); owed["outstanding_minor"] != float64(475000) {
		t.Fatalf("owed after recovery %v", owed)
	}
	if !journalBalanced(t, "payout_recovery", scalar[string](t, `SELECT id::text FROM seller_payouts WHERE booking_id=$1`, second)) {
		t.Fatal("recovery journal must balance")
	}
	if msg := findMessage(t, h.deliverDue(s.client.email), "₦4,750 is on its way"); !strings.Contains(msg.Text, "Kept to repay an earlier refund: ₦4,750") {
		t.Fatalf("payout email:\n%s", msg.Text)
	}
}

func TestReversedPayoutCanBeRetried(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	s := h.newSeller("fixed")
	_, bookingID, _ := paidBooking(t, h, fake, s, h.slotOn(s.handle, 1, 0))
	makePayoutDue(t, bookingID)
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	reference := scalar[string](t, `SELECT reference FROM seller_payouts WHERE booking_id=$1`, bookingID)
	fake.mu.Lock()
	tr := fake.transfers[reference]
	tr.status = "failed"
	fake.transfers[reference] = tr
	fake.mu.Unlock()
	if res := sendKoraWebhook(t, h, "transfer.failed", map[string]any{"reference": reference, "status": "failed", "amount": 9500}); res.Status != 200 {
		t.Fatalf("webhook %d %s", res.Status, res.Body)
	}
	if got := scalar[string](t, `SELECT state FROM seller_payouts WHERE booking_id=$1`, bookingID); got != "failed" {
		t.Fatalf("payout after reversal %s", got)
	}
	if !journalBalanced(t, "payout_reversal", scalar[string](t, `SELECT id::text||':'||reference FROM seller_payouts WHERE booking_id=$1`, bookingID)) {
		t.Fatal("reversal journal must balance")
	}
	ops, _ := h.operator()
	list := ops.expect(200, "GET", "/api/v1/ops/payouts?state=attention", nil)["payouts"].([]any)
	if len(list) == 0 {
		t.Fatal("the failed payout should need attention")
	}
	payoutID := scalar[string](t, `SELECT id::text FROM seller_payouts WHERE booking_id=$1`, bookingID)
	ops.expect(200, "POST", "/api/v1/ops/payouts/"+payoutID+"/retry", map[string]string{"reason": "Seller confirmed the account is open again"})
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state||'/'||reference FROM seller_payouts WHERE id=$1`, payoutID); got != "paid/"+reference+"-g2" {
		t.Fatalf("retried payout %s", got)
	}
	// Paid, returned, paid again: the seller is owed nothing more.
	if got := scalar[int64](t, `SELECT COALESCE(sum(CASE WHEN e.side='credit' THEN e.amount_minor ELSE -e.amount_minor END),0) FROM ledger_entries e JOIN ledger_accounts a ON a.id=e.account_id WHERE a.account_code='seller_payable' AND a.scope_id=(SELECT seller_id FROM bookings WHERE id=$1)`, bookingID); got != 0 {
		t.Fatalf("seller payable %d", got)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM audit_events WHERE target_id=$1 AND action='payout.retry'`, payoutID); n != 1 {
		t.Fatal("retry must be audited")
	}
}

func TestSellerAddsPayoutAccount(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	s := h.newSeller("fixed")
	if _, err := itPool.Exec(context.Background(), `DELETE FROM seller_payout_accounts WHERE seller_id=(SELECT id FROM seller_profiles WHERE handle=$1)`, s.handle); err != nil {
		t.Fatal(err)
	}
	if acct := s.client.expect(200, "GET", "/api/v1/me/payout-account", nil); acct["account"] != nil || acct["payout_delay_minutes"] != float64(180) || acct["dispute_window_minutes"] != float64(120) {
		t.Fatalf("empty account %v", acct)
	}
	// Without a bank account, buyers can't pay.
	buyer := h.guestBuyer()
	quote := buyer.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "Ada", "duration_minutes": 30, "starts_at": h.slotOn(s.handle, 1, 0)}, "Idempotency-Key", idempotencyKey())
	if res := buyer.do("POST", "/api/v1/quotes/"+quote["id"].(string)+"/checkout", "{}"); res.Status != 503 || !strings.Contains(string(res.Body), "SELLER_PAYOUT_NOT_READY") {
		t.Fatalf("checkout without account: %d %s", res.Status, res.Body)
	}
	banks := s.client.expect(200, "GET", "/api/v1/payout-banks", nil)["banks"].([]any)
	if len(banks) != 2 {
		t.Fatalf("banks %v", banks)
	}
	s.client.expect(422, "POST", "/api/v1/me/payout-account/resolve", map[string]string{"bank_code": "058", "account_number": "12345"})
	s.client.expect(422, "POST", "/api/v1/me/payout-account/resolve", map[string]string{"bank_code": "058", "account_number": "0001234567"})
	resolved := s.client.expect(200, "POST", "/api/v1/me/payout-account/resolve", map[string]string{"bank_code": "058", "account_number": "0123456789"})
	if resolved["account_name"] != "ADA SELLER 6789" {
		t.Fatalf("resolved %v", resolved)
	}
	saved := s.client.expect(200, "PUT", "/api/v1/me/payout-account", map[string]string{"bank_code": "058", "account_number": "0123 456 789"})["account"].(map[string]any)
	if saved["bank_name"] != "Guaranty Trust Bank" || saved["account_last4"] != "6789" || saved["in_safety_hold"] != false {
		t.Fatalf("saved %v", saved)
	}
	// The account number is stored encrypted, never in the clear.
	var sellerID string
	var sealed []byte
	if err := itPool.QueryRow(context.Background(), `SELECT seller_id::text,account_sealed FROM seller_payout_accounts WHERE account_name='ADA SELLER 6789'`).Scan(&sellerID, &sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "0123456789") {
		t.Fatal("account number stored in the clear")
	}
	if number, err := h.api.openPayoutAccount(sellerID, sealed); err != nil || number != "0123456789" {
		t.Fatalf("sealed account opens to %q, %v", number, err)
	}
	// Changing it starts a 24-hour hold on payouts to the new account.
	changed := s.client.expect(200, "PUT", "/api/v1/me/payout-account", map[string]string{"bank_code": "044", "account_number": "9876543210"})["account"].(map[string]any)
	if changed["in_safety_hold"] != true || changed["account_last4"] != "3210" {
		t.Fatalf("changed %v", changed)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM audit_events WHERE actor_id=$1 AND action='payout_account.set'`, s.userID); n != 2 {
		t.Fatalf("account changes audited %d", n)
	}
}
