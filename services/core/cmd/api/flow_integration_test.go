//go:build integration

package main

import (
	"context"
	"strings"
	"testing"
)

// A buyer can hold a time and pay without typing an email code first.
func TestBuyerPaysWithoutEmailCode(t *testing.T) {
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	h := newHarness(t)
	s := h.newSeller("fixed")
	email := unique("quick") + "@buyer.test"
	buyer := h.client(email)
	buyer.expect(422, "POST", "/api/v1/bookings/start", map[string]string{"email": "not-an-email"})
	buyer.expect(200, "POST", "/api/v1/bookings/start", map[string]string{"email": strings.ToUpper(email)})
	quote := buyer.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "Quick Buyer", "duration_minutes": 30, "starts_at": h.slot(s.handle, 0)}, "Idempotency-Key", idempotencyKey())
	quoteID := quote["id"].(string)
	if got := buyer.expect(200, "GET", "/api/v1/quotes/"+quoteID, nil); got["seller_name"] != "Seller "+s.handle {
		t.Fatalf("quote shows seller %v", got["seller_name"])
	}
	bookingID := buyer.expect(201, "POST", "/api/v1/dev/quotes/"+quoteID+"/simulate-payment", "{}")["booking_id"].(string)
	var guestEmail string
	if err := itPool.QueryRow(context.Background(), `SELECT guest_email FROM bookings WHERE id=$1`, bookingID).Scan(&guestEmail); err != nil || guestEmail != email {
		t.Fatalf("booking email %q, %v", guestEmail, err)
	}
	buyer.expect(200, "GET", "/api/v1/bookings/"+bookingID, nil)

	// The booking-only session opens nothing else.
	buyer.expect(401, "GET", "/api/v1/me", nil)
	buyer.expect(401, "GET", "/api/v1/me/data-export", nil)
	if res := buyer.do("POST", "/api/v1/offers", map[string]any{"seller": s.handle, "name": "x", "duration_minutes": 30, "amount_minor": 1000}, "Idempotency-Key", idempotencyKey()); res.Status < 400 {
		t.Fatalf("unconfirmed session made an offer: %d", res.Status)
	}
	other := h.newSeller("fixed")
	var otherBooking string
	if err := itPool.QueryRow(context.Background(), `SELECT b.id::text FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE sp.handle<>$1 LIMIT 1`, s.handle).Scan(&otherBooking); err == nil {
		buyer.expect(404, "GET", "/api/v1/bookings/"+otherBooking, nil)
	}
	_ = other

	// Booking emails reach the unconfirmed address.
	sent := h.deliverDue(email)
	if len(sent) == 0 {
		t.Fatal("no booking email was sent to the buyer")
	}

	// Signing in later with a code claims the same account and its booking.
	later := h.client(email)
	later.signIn("login")
	var buyerUser, laterUser string
	if err := itPool.QueryRow(context.Background(), `SELECT buyer_user_id::text FROM bookings WHERE id=$1`, bookingID).Scan(&buyerUser); err != nil {
		t.Fatal(err)
	}
	laterUser = later.expect(200, "GET", "/api/v1/me", nil)["id"].(string)
	if buyerUser != laterUser {
		t.Fatal("confirming the email created a second account")
	}

	// A restricted account can't start a booking.
	if _, err := itPool.Exec(context.Background(), `UPDATE users SET status='restricted' WHERE id=$1`, laterUser); err != nil {
		t.Fatal(err)
	}
	h.client(email).expect(403, "POST", "/api/v1/bookings/start", map[string]string{"email": email})
}

// A seller opens for bookings the moment the bank confirms their account,
// unless an operator has put them on hold.
func TestSellerOpensWhenBankAccountIsConfirmed(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	s := h.newSeller("fixed")
	ctx := context.Background()
	if _, err := itPool.Exec(ctx, `DELETE FROM seller_payout_accounts WHERE seller_id=(SELECT id FROM seller_profiles WHERE handle=$1)`, s.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := itPool.Exec(ctx, `UPDATE seller_profiles SET readiness_state='incomplete' WHERE handle=$1`, s.handle); err != nil {
		t.Fatal(err)
	}
	anon := h.client("")
	if anon.expect(200, "GET", "/api/v1/people/"+s.handle, nil)["ready"] != false {
		t.Fatal("seller open before adding a bank account")
	}
	s.expect(200, "PUT", "/api/v1/me/payout-account", map[string]string{"bank_code": "058", "account_number": "0123456789"})
	if anon.expect(200, "GET", "/api/v1/people/"+s.handle, nil)["ready"] != true {
		t.Fatal("seller not open after the bank confirmed the account")
	}
	if n := scalar[int64](t, `SELECT count(*) FROM audit_events WHERE actor_id=$1 AND action='seller.opened_for_bookings'`, s.userID); n != 1 {
		t.Fatalf("opening audited %d times", n)
	}
	// An operator's hold survives a bank account change.
	ops, _ := h.operator()
	ops.expect(200, "POST", "/api/v1/ops/people/"+s.userID+"/payout-readiness", map[string]any{"ready": false, "reason": "Reports of misuse under review"})
	s.expect(200, "PUT", "/api/v1/me/payout-account", map[string]string{"bank_code": "044", "account_number": "9876543210"})
	if anon.expect(200, "GET", "/api/v1/people/"+s.handle, nil)["ready"] != false {
		t.Fatal("a bank account change lifted an operator's hold")
	}
}

// The buyer pays the bank's transfer fee on top of the price; the seller's
// share is 95% of the price, untouched by the fee.
func TestBuyerPaysTheTransferFee(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	s := h.newSeller("fixed")
	_, bookingID, reference := paidBooking(t, h, fake, s, h.slotOn(s.handle, 2, 0))
	var gross, deduction, entitlement, expected, buyerFee int64
	if err := itPool.QueryRow(context.Background(), `SELECT pa.gross_minor,pa.deduction_minor,pa.seller_entitlement_minor,att.expected_minor,att.buyer_fee_minor FROM payment_allocations pa JOIN payment_attempts att ON att.booking_id=pa.booking_id WHERE pa.booking_id=$1`, bookingID).Scan(&gross, &deduction, &entitlement, &expected, &buyerFee); err != nil {
		t.Fatal(err)
	}
	if gross != 1000000 || deduction != 50000 || entitlement != 950000 || expected != 1015229 || buyerFee != 15229 {
		t.Fatalf("gross %d deduction %d entitlement %d expected %d fee %d", gross, deduction, entitlement, expected, buyerFee)
	}
	if got := scalar[int64](t, `SELECT entitlement_minor FROM seller_payouts WHERE booking_id=$1`, bookingID); got != 950000 {
		t.Fatalf("payout entitlement %d", got)
	}
	attemptID := scalar[string](t, `SELECT id::text FROM payment_attempts WHERE merchant_reference=$1`, reference)
	if !journalBalanced(t, "payment_attempt", attemptID) {
		t.Fatal("payment journal must balance")
	}
	if got := scalar[int64](t, `SELECT e.amount_minor FROM ledger_entries e JOIN ledger_journals j ON j.id=e.journal_id JOIN ledger_accounts a ON a.id=e.account_id WHERE j.source_id=$1 AND a.account_code='platform_fee_revenue'`, attemptID); got != 65229 {
		t.Fatalf("platform revenue %d (5%% of the price plus the buyer's fee)", got)
	}
}
