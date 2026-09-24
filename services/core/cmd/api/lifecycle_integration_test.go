//go:build integration

package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// slotOn returns the n-th free 30-minute start daysAhead days from now (Lagos).
func (h *harness) slotOn(handle string, daysAhead, n int) string {
	h.t.Helper()
	loc, _ := time.LoadLocation("Africa/Lagos")
	date := time.Now().In(loc).AddDate(0, 0, daysAhead).Format("2006-01-02")
	slots := h.client("").expect(200, "GET", "/api/v1/people/"+handle+"/slots?date="+date+"&duration=30", nil)["slots"].([]any)
	if len(slots) <= n {
		h.t.Fatalf("expected at least %d slots on %s", n+1, date)
	}
	return slots[n].(map[string]any)["starts_at"].(string)
}

// paidBooking pays for a 30-minute booking by bank transfer through the fake
// Kora and returns the buyer, booking ID and payment reference.
func paidBooking(t *testing.T, h *harness, fake *fakeKora, s seller, startsAt string) (*client, string, string) {
	t.Helper()
	buyer := h.guestBuyer()
	quote := buyer.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "Ada Buyer", "duration_minutes": 30, "starts_at": startsAt}, "Idempotency-Key", idempotencyKey())
	quoteID := quote["id"].(string)
	checkout := buyer.expect(200, "POST", "/api/v1/quotes/"+quoteID+"/checkout", "{}")
	reference := checkout["reference"].(string)
	fake.pay(reference, 1000000)
	if res := sendKoraWebhook(t, h, "charge.success", map[string]any{"reference": reference, "status": "success", "amount": 10000, "currency": "NGN"}); res.Status != 200 {
		t.Fatalf("webhook: %d %s", res.Status, res.Body)
	}
	if err := h.api.processOneProviderEvent(context.Background()); err != nil {
		t.Fatal(err)
	}
	return buyer, scalar[string](t, `SELECT id::text FROM bookings WHERE quote_id=$1 AND payment_state='paid'`, quoteID), reference
}

func journalBalanced(t *testing.T, sourceType, sourceID string) bool {
	t.Helper()
	return scalar[bool](t, `SELECT COALESCE(sum(CASE WHEN side='debit' THEN amount_minor ELSE -amount_minor END),1)=0 FROM ledger_entries e JOIN ledger_journals j ON j.id=e.journal_id WHERE j.source_type=$1 AND j.source_id=$2`, sourceType, sourceID)
}

func TestCancellationPolicyArithmetic(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	booked := now.Add(-48 * time.Hour) // outside the one-hour grace period
	cases := []struct {
		policy string
		notice time.Duration
		want   int64
	}{
		{"flexible", 25 * time.Hour, 100}, {"flexible", 23 * time.Hour, 0},
		{"moderate", 73 * time.Hour, 100}, {"moderate", 30 * time.Hour, 50}, {"moderate", 2 * time.Hour, 0},
		{"strict", 8 * 24 * time.Hour, 50}, {"strict", 6 * 24 * time.Hour, 0},
		{"unknown", 25 * time.Hour, 100}, // falls back to flexible
	}
	for _, c := range cases {
		if got := buyerRefundPercent(c.policy, booked, now.Add(c.notice), now); got != c.want {
			t.Errorf("%s with %s notice: %d%%, want %d%%", c.policy, c.notice, got, c.want)
		}
	}
	// Grace period: cancelling within an hour of booking, a day or more ahead.
	if got := buyerRefundPercent("strict", now.Add(-30*time.Minute), now.Add(48*time.Hour), now); got != 100 {
		t.Errorf("grace period refund %d%%", got)
	}
	if got := buyerRefundPercent("strict", now.Add(-30*time.Minute), now.Add(3*time.Hour), now); got != 0 {
		t.Errorf("grace period must not apply to imminent bookings, got %d%%", got)
	}
	drop := nextRefundDrop("moderate", booked, now.Add(100*time.Hour), now)
	if !drop.Equal(now.Add(28 * time.Hour)) {
		t.Errorf("moderate refund should drop 72h before start, got %s", drop)
	}
	if p, s := refundShares(500000, 1000000, 50000); p != 25000 || s != 475000 {
		t.Errorf("refund shares %d/%d", p, s)
	}
	if p, s := refundShares(333333, 1000000, 50000); p+s != 333333 || p != 16666 {
		t.Errorf("rounding keeps the total: %d/%d", p, s)
	}
}

func TestBuyerCancellationRefundsPerPolicy(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	t.Setenv("REFUNDS_ENABLED", "true")
	s := h.newSeller("fixed")
	s.client.expect(200, "PUT", "/api/v1/me/cancellation-policy", map[string]string{"policy": "moderate"})
	profile := h.client("").expect(200, "GET", "/api/v1/people/"+s.handle, nil)
	if profile["cancellation_policy"].(map[string]any)["key"] != "moderate" {
		t.Fatalf("public profile policy: %v", profile["cancellation_policy"])
	}
	buyer, bookingID, reference := paidBooking(t, h, fake, s, h.slotOn(s.handle, 3, 0))
	if got := scalar[string](t, `SELECT cancellation_policy FROM bookings WHERE id=$1`, bookingID); got != "moderate" {
		t.Fatalf("booking policy snapshot %s", got)
	}
	// A later policy change never touches the existing booking.
	s.client.expect(200, "PUT", "/api/v1/me/cancellation-policy", map[string]string{"policy": "strict"})
	// Put the booking 30 hours out, booked two hours ago: moderate gives 50%.
	if _, err := itPool.Exec(context.Background(), `UPDATE bookings SET starts_at=now()+interval '30 hours',created_at=now()-interval '2 hours' WHERE id=$1`, bookingID); err != nil {
		t.Fatal(err)
	}
	preview := buyer.expect(200, "GET", "/api/v1/bookings/"+bookingID+"/cancellation-preview", nil)
	if preview["refund_percent"] != float64(50) || preview["refund_minor"] != float64(500000) || preview["policy"] != "moderate" {
		t.Fatalf("preview %v", preview)
	}
	if res := buyer.do("POST", "/api/v1/bookings/"+bookingID+"/cancel", map[string]any{"expected_refund_minor": 1000000}); res.Status != 409 || !strings.Contains(string(res.Body), "REFUND_CHANGED") {
		t.Fatalf("stale refund confirmation: %d %s", res.Status, res.Body)
	}
	buyer.expect(200, "POST", "/api/v1/bookings/"+bookingID+"/cancel", map[string]any{"expected_refund_minor": 500000, "reason": "Plans changed"})
	if got := scalar[string](t, `SELECT state||'/'||cancelled_by_role FROM bookings WHERE id=$1`, bookingID); got != "cancelled/buyer" {
		t.Fatalf("booking %s", got)
	}
	if active := scalar[bool](t, `SELECT active FROM slot_reservations WHERE booking_id=$1`, bookingID); active {
		t.Fatal("the time should be released")
	}
	buyer.expect(409, "POST", "/api/v1/bookings/"+bookingID+"/cancel", map[string]any{"expected_refund_minor": 0})

	refundID := scalar[string](t, `SELECT id::text FROM refunds WHERE booking_id=$1`, bookingID)
	if got := scalar[string](t, `SELECT state||':'||platform_share_minor||':'||seller_share_minor FROM refunds WHERE id=$1`, refundID); got != "queued:25000:475000" {
		t.Fatalf("refund %s", got)
	}
	// Kora accepts it as processing, then reports it done.
	if err := h.api.processRefunds(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state FROM refunds WHERE id=$1`, refundID); got != "submitted" {
		t.Fatalf("refund state after submission %s", got)
	}
	providerRef := scalar[string](t, `SELECT provider_refund_id FROM refunds WHERE id=$1`, refundID)
	fake.mu.Lock()
	rf := fake.refunds[providerRef]
	if rf == nil || rf.payment != reference || rf.amount != 500000 {
		t.Fatalf("unexpected Kora refund %s %+v", providerRef, rf)
	}
	rf.status = "success"
	fake.mu.Unlock()
	if res := sendKoraWebhook(t, h, "refund.success", map[string]any{"reference": providerRef, "status": "success", "amount": 5000, "currency": "NGN"}); res.Status != 200 {
		t.Fatalf("refund webhook %d %s", res.Status, res.Body)
	}
	if err := h.api.processRefunds(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT r.state||'/'||b.payment_state FROM refunds r JOIN bookings b ON b.id=r.booking_id WHERE r.id=$1`, refundID); got != "processed/partially_refunded" {
		t.Fatalf("after processing: %s", got)
	}
	if !journalBalanced(t, "refund", refundID) {
		t.Fatal("refund journal must balance")
	}
	// The seller hadn't been paid yet, so their share of the refund comes out
	// of the held payout rather than becoming a debt.
	if got := scalar[string](t, `SELECT seller_liability FROM refunds WHERE id=$1`, refundID); got != "payable" {
		t.Fatalf("liability %s", got)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM seller_recoveries WHERE refund_id=$1`, refundID); n != 0 {
		t.Fatal("no recovery is needed before the payout")
	}
	makePayoutDue(t, bookingID)
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state||'/'||amount_minor FROM seller_payouts WHERE booking_id=$1`, bookingID); got != "paid/475000" {
		t.Fatalf("payout %s", got)
	}
	all := h.deliverDue("")
	var msgs, sellerMsgs []emailMessage
	for _, m := range all {
		if m.To == buyer.email {
			msgs = append(msgs, m)
		} else if m.To == s.client.email {
			sellerMsgs = append(sellerMsgs, m)
		}
	}
	cancelled := findMessage(t, msgs, "You cancelled your booking")
	if !strings.Contains(cancelled.Text, "Your refund: ₦5,000") || len(cancelled.Attachments) != 1 || !strings.Contains(string(cancelled.Attachments[0].Content), "METHOD:CANCEL") {
		t.Fatalf("cancellation email:\n%s", cancelled.Text)
	}
	findMessage(t, msgs, "Your refund of ₦5,000 is on its way")
	if msg := findMessage(t, sellerMsgs, "cancelled your booking"); !strings.Contains(msg.Text, "You keep ₦4,750") {
		t.Fatalf("seller cancellation email:\n%s", msg.Text)
	}
}

func TestSellerCancellationBeforePayoutCancelsIt(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	t.Setenv("REFUNDS_ENABLED", "true")
	fake.refundStatus = "success"
	s := h.newSeller("fixed")
	_, first, _ := paidBooking(t, h, fake, s, h.slotOn(s.handle, 2, 0))
	preview := s.client.expect(200, "GET", "/api/v1/bookings/"+first+"/cancellation-preview", nil)
	if preview["role"] != "seller" || preview["refund_percent"] != float64(100) {
		t.Fatalf("seller preview %v", preview)
	}
	s.client.expect(200, "POST", "/api/v1/bookings/"+first+"/cancel", map[string]any{"expected_refund_minor": 1000000, "reason": "Unwell"})
	if err := h.api.processRefunds(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state||'/'||seller_liability FROM refunds WHERE booking_id=$1`, first); got != "processed/payable" {
		t.Fatalf("refund %s", got)
	}
	refundID := scalar[string](t, `SELECT id::text FROM refunds WHERE booking_id=$1`, first)
	if !journalBalanced(t, "refund", refundID) {
		t.Fatal("refund journal must balance")
	}
	owed := s.client.expect(200, "GET", "/api/v1/me/refund-recoveries", nil)
	if owed["outstanding_minor"] != float64(0) {
		t.Fatalf("nothing is owed when the payout was still held: %v", owed)
	}
	makePayoutDue(t, first)
	if err := h.api.processPayouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state FROM seller_payouts WHERE booking_id=$1`, first); got != "cancelled" {
		t.Fatalf("payout %s", got)
	}
	if fake.transferCalls != 0 {
		t.Fatal("nothing may be transferred for a fully refunded booking")
	}
	// The seller's payable balance for the booking is back to zero.
	if got := scalar[int64](t, `SELECT COALESCE(sum(CASE WHEN e.side='credit' THEN e.amount_minor ELSE -e.amount_minor END),0) FROM ledger_entries e JOIN ledger_accounts a ON a.id=e.account_id WHERE a.account_code='seller_payable' AND a.scope_id=(SELECT seller_id FROM bookings WHERE id=$1)`, first); got != 0 {
		t.Fatalf("seller payable %d", got)
	}
}

func TestRefundsWaitForOperatorsWhenAutomaticRefundsAreOff(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	s := h.newSeller("fixed")
	_, bookingID, _ := paidBooking(t, h, fake, s, h.slotOn(s.handle, 2, 0))
	s.client.expect(200, "POST", "/api/v1/bookings/"+bookingID+"/cancel", map[string]any{"expected_refund_minor": 1000000})
	refundID := scalar[string](t, `SELECT id::text FROM refunds WHERE booking_id=$1`, bookingID)
	if got := scalar[string](t, `SELECT state FROM refunds WHERE id=$1`, refundID); got != "pending_approval" {
		t.Fatalf("state %s", got)
	}
	if err := h.api.processRefunds(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.refundCalls != 0 {
		t.Fatal("no refund may be sent to Kora without approval")
	}
	ops, _ := h.operator()
	list := ops.expect(200, "GET", "/api/v1/ops/refunds", nil)
	if list["automatic_refunds"] != false {
		t.Fatalf("list %v", list)
	}
	if res := ops.do("POST", "/api/v1/ops/refunds/"+refundID+"/approve", map[string]string{"reason": "Seller cancelled, confirmed"}); res.Status != 409 {
		t.Fatalf("approve with automatic refunds off: %d", res.Status)
	}
	ops.expect(200, "POST", "/api/v1/ops/refunds/"+refundID+"/record", map[string]string{"reason": "Refunded in Kora dashboard, ref RF-1"})
	if got := scalar[string](t, `SELECT state FROM refunds WHERE id=$1`, refundID); got != "processed" {
		t.Fatalf("state %s", got)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM audit_events WHERE target_id=$1 AND action='refund.record'`, refundID); n != 1 {
		t.Fatal("recording must be audited")
	}
	if res := ops.do("POST", "/api/v1/ops/refunds/"+refundID+"/record", map[string]string{"reason": "Refunded in Kora dashboard, ref RF-1"}); res.Status != 409 {
		t.Fatalf("recording twice: %d", res.Status)
	}
}

func TestNoShowReportsResolve(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	s := h.newSeller("fixed")
	buyer := h.guestBuyer()
	_, bookingID := h.holdAndSimulate(buyer, s.handle, h.slot(s.handle, 0))
	buyer.expect(409, "POST", "/api/v1/bookings/"+bookingID+"/no-show", "{}") // too early
	if _, err := itPool.Exec(context.Background(), `UPDATE bookings SET starts_at=now()-interval '20 minutes' WHERE id=$1`, bookingID); err != nil {
		t.Fatal(err)
	}
	buyer.expect(201, "POST", "/api/v1/bookings/"+bookingID+"/no-show", "{}")
	findMessage(t, h.deliverDue(s.client.email), "says you didn't join")
	buyer.expect(409, "POST", "/api/v1/bookings/"+bookingID+"/no-show", "{}") // only one report
	// Nobody disputes; once the window passes the report stands.
	if _, err := itPool.Exec(context.Background(), `UPDATE no_show_reports SET resolves_at=now()-interval '1 minute' WHERE booking_id=$1`, bookingID); err != nil {
		t.Fatal(err)
	}
	if err := h.api.resolveNoShows(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, `SELECT state FROM bookings WHERE id=$1`, bookingID); got != "no_show_seller" {
		t.Fatalf("booking %s", got)
	}
	if got := scalar[string](t, `SELECT reason||'/'||state FROM refunds WHERE booking_id=$1`, bookingID); got != "seller_no_show/not_required" {
		t.Fatalf("refund %s", got)
	}

	// A disputed report is decided by operations.
	buyer2 := h.guestBuyer()
	_, second := h.holdAndSimulate(buyer2, s.handle, h.slot(s.handle, 6))
	if _, err := itPool.Exec(context.Background(), `UPDATE bookings SET starts_at=now()-interval '40 minutes' WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	s.client.expect(201, "POST", "/api/v1/bookings/"+second+"/no-show", "{}")
	buyer2.expect(200, "POST", "/api/v1/bookings/"+second+"/no-show/dispute", map[string]string{"reason": "I joined and waited 20 minutes"})
	ops, _ := h.operator()
	reportID := scalar[string](t, `SELECT id::text FROM no_show_reports WHERE booking_id=$1`, second)
	ops.expect(200, "POST", "/api/v1/ops/no-shows/"+reportID+"/resolve", map[string]string{"outcome": "both_attended", "reason": "Both sides sent call screenshots"})
	if got := scalar[string](t, `SELECT b.state||'/'||ns.state FROM bookings b JOIN no_show_reports ns ON ns.booking_id=b.id WHERE b.id=$1`, second); got != "confirmed/rejected" {
		t.Fatalf("after review: %s", got)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM audit_events WHERE target_id=$1 AND action='no_show.resolved'`, reportID); n != 1 {
		t.Fatal("resolution must be audited")
	}
}

func TestReviewsAfterSessions(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	s := h.newSeller("fixed")
	buyer := h.guestBuyer()
	_, bookingID := h.holdAndSimulate(buyer, s.handle, h.slot(s.handle, 0))
	if n := scalar[int64](t, `SELECT count(*) FROM notification_outbox WHERE booking_id=$1 AND kind='review_request_buyer'`, bookingID); n != 1 {
		t.Fatal("a review request should be scheduled")
	}
	buyer.expect(409, "POST", "/api/v1/bookings/"+bookingID+"/review", map[string]any{"rating": 5}) // not yet
	if _, err := itPool.Exec(context.Background(), `UPDATE bookings SET starts_at=now()-interval '2 hours' WHERE id=$1`, bookingID); err != nil {
		t.Fatal(err)
	}
	s.client.expect(404, "POST", "/api/v1/bookings/"+bookingID+"/review", map[string]any{"rating": 5}) // sellers can't
	buyer.expect(422, "POST", "/api/v1/bookings/"+bookingID+"/review", map[string]any{"rating": 6})
	review := buyer.expect(201, "POST", "/api/v1/bookings/"+bookingID+"/review", map[string]any{"rating": 4, "body": "Clear, practical advice."})
	buyer.expect(409, "POST", "/api/v1/bookings/"+bookingID+"/review", map[string]any{"rating": 5})
	findMessage(t, h.deliverDue(s.client.email), "New review: ★★★★☆ from Buyer")
	s.client.expect(200, "POST", "/api/v1/reviews/"+review["id"].(string)+"/reply", map[string]string{"body": "Thank you!"})
	s.client.expect(409, "POST", "/api/v1/reviews/"+review["id"].(string)+"/reply", map[string]string{"body": "Again"})

	public := h.client("").expect(200, "GET", "/api/v1/people/"+s.handle+"/reviews", nil)
	items := public["reviews"].([]any)
	if public["count"] != float64(1) || public["average"] != float64(4) || len(items) != 1 {
		t.Fatalf("public reviews %v", public)
	}
	first := items[0].(map[string]any)
	if first["reviewer"] != "Buyer" || first["seller_reply"] != "Thank you!" || first["body"] != "Clear, practical advice." {
		t.Fatalf("review %v", first)
	}
	if rating := h.client("").expect(200, "GET", "/api/v1/people/"+s.handle, nil)["rating"].(map[string]any); rating["count"] != float64(1) {
		t.Fatalf("profile rating %v", rating)
	}
	detail := buyer.expect(200, "GET", "/api/v1/bookings/"+bookingID, nil)
	if detail["can_review"] != false || detail["review"] == nil {
		t.Fatalf("booking detail review fields %v", detail)
	}
	ops, _ := h.operator()
	ops.expect(200, "POST", "/api/v1/ops/reviews/"+review["id"].(string)+"/hide", map[string]string{"reason": "Contains a phone number"})
	if public = h.client("").expect(200, "GET", "/api/v1/people/"+s.handle+"/reviews", nil); public["count"] != float64(0) {
		t.Fatalf("hidden review still public %v", public)
	}
	// The review request is no longer sent once a review exists.
	if _, err := itPool.Exec(context.Background(), `UPDATE notification_outbox SET due_at=now() WHERE booking_id=$1 AND kind='review_request_buyer'`, bookingID); err != nil {
		t.Fatal(err)
	}
	h.deliverDue(buyer.email)
	if got := scalar[string](t, `SELECT state FROM notification_outbox WHERE booking_id=$1 AND kind='review_request_buyer'`, bookingID); got != "cancelled" {
		t.Fatalf("review request state %s", got)
	}
}
