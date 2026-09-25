//go:build integration

package main

import (
	"context"
	"testing"
)

// payWithCard runs a 15-minute (₦5,000) card checkout paid by a card from the
// given country with the given provider fee, and returns the reference.
func payWithCard(t *testing.T, h *harness, fake *fakeKora, country string, fee int64) (reference, quoteID string) {
	t.Helper()
	s := h.newSeller("fixed")
	buyer := h.guestBuyer()
	quote := buyer.expect(201, "POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "Visitor", "duration_minutes": 15, "starts_at": h.slot(s.handle, 0)}, "Idempotency-Key", idempotencyKey())
	quoteID = quote["id"].(string)
	fake.mu.Lock()
	fake.fee, fake.cardCountry = fee, country
	fake.mu.Unlock()
	checkout := buyer.expect(200, "POST", "/api/v1/quotes/"+quoteID+"/checkout", map[string]string{"method": "card"})
	reference = checkout["reference"].(string)
	fake.payFull(reference)
	if res := sendKoraWebhook(t, h, "charge.success", map[string]any{"reference": reference, "status": "success", "amount": 5000, "currency": "NGN"}); res.Status != 200 {
		t.Fatalf("webhook: %d %s", res.Status, res.Body)
	}
	if err := h.api.processOneProviderEvent(context.Background()); err != nil {
		t.Fatal(err)
	}
	return reference, quoteID
}

func approveInternationalSchedule(t *testing.T) {
	t.Helper()
	// An example international card schedule: 3.9% + ₦100.
	if _, err := itPool.Exec(context.Background(), `INSERT INTO provider_fee_schedules(id,provider,currency,channel,percent_bps,fixed_minor,effective_from,approved_at) SELECT gen_random_uuid(),'kora','NGN','card_international',390,10000,now()-interval '1 day',now() WHERE NOT EXISTS (SELECT 1 FROM provider_fee_schedules WHERE channel='card_international')`); err != nil {
		t.Fatal(err)
	}
}

func TestInternationalCardFeeWithinScheduleBecomesBooking(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	t.Setenv("INTERNATIONAL_CARDS_ENABLED", "true")
	approveInternationalSchedule(t)
	// ₦5,000: platform fee 5% = ₦250 plus the buyer's fee; a US card costs 3.9% + ₦100 = ₦295.
	reference, quoteID := payWithCard(t, h, fake, "US", 29500)
	bookingID := scalar[string](t, `SELECT id::text FROM bookings WHERE quote_id=$1 AND payment_state='paid'`, quoteID)
	if got := scalar[string](t, `SELECT card_country FROM payment_attempts WHERE merchant_reference=$1`, reference); got != "US" {
		t.Fatalf("card_country = %q", got)
	}
	if cost, deduction := scalar[int64](t, `SELECT processor_cost_minor FROM payment_allocations WHERE booking_id=$1`, bookingID), scalar[int64](t, `SELECT deduction_minor FROM payment_allocations WHERE booking_id=$1`, bookingID); cost != 29500 || deduction != 25000 {
		t.Fatalf("allocation cost=%d deduction=%d", cost, deduction)
	}
	if got := scalar[int64](t, `SELECT seller_entitlement_minor FROM payment_allocations WHERE booking_id=$1`, bookingID); got != 475000 {
		t.Fatalf("seller must still receive the full entitlement, got %d", got)
	}
	balanced := scalar[bool](t, `SELECT sum(CASE WHEN side='debit' THEN amount_minor ELSE -amount_minor END)=0 FROM ledger_entries e JOIN ledger_journals j ON j.id=e.journal_id WHERE j.source_id=(SELECT id::text FROM payment_attempts WHERE merchant_reference=$1)`, reference)
	if !balanced {
		t.Fatal("ledger journal is not balanced")
	}
}

func TestInternationalCardFeesOutsidePolicyStopForReview(t *testing.T) {
	h := newHarness(t)
	fake := newFakeKora(t)
	enablePayments(t, fake)
	approveInternationalSchedule(t)

	// Switched off: a foreign card whose fee exceeds the platform fee is an exception.
	t.Setenv("INTERNATIONAL_CARDS_ENABLED", "false")
	// The buyer's transfer fee (₦77 here) adds to the cover, so ₦330 is over it.
	reference, quoteID := payWithCard(t, h, fake, "GB", 33000)
	if got := scalar[string](t, `SELECT kind FROM payment_exceptions WHERE payment_attempt_id=(SELECT id FROM payment_attempts WHERE merchant_reference=$1)`, reference); got != "settlement_mismatch" {
		t.Fatalf("exception kind = %s", got)
	}
	if n := scalar[int64](t, `SELECT count(*) FROM bookings WHERE quote_id=$1`, quoteID); n != 0 {
		t.Fatal("no booking expected")
	}
	if got := scalar[string](t, `SELECT card_country FROM payment_attempts WHERE merchant_reference=$1`, reference); got != "GB" {
		t.Fatalf("card country should be recorded even for exceptions, got %q", got)
	}

	// Switched on, but the fee is above the approved schedule (₦295 max).
	t.Setenv("INTERNATIONAL_CARDS_ENABLED", "true")
	reference, _ = payWithCard(t, h, fake, "GB", 40000)
	if n := scalar[int64](t, `SELECT count(*) FROM payment_exceptions WHERE payment_attempt_id=(SELECT id FROM payment_attempts WHERE merchant_reference=$1) AND kind='settlement_mismatch'`, reference); n != 1 {
		t.Fatal("a fee above the international schedule must stop for review")
	}

	// A Nigerian card never gets the international allowance.
	reference, _ = payWithCard(t, h, fake, "NG", 33000)
	if n := scalar[int64](t, `SELECT count(*) FROM payment_exceptions WHERE payment_attempt_id=(SELECT id FROM payment_attempts WHERE merchant_reference=$1) AND kind='settlement_mismatch'`, reference); n != 1 {
		t.Fatal("a local card fee above the platform fee must stop for review")
	}
}

func TestInternationalCardStatusVisibleToOperations(t *testing.T) {
	h := newHarness(t)
	t.Setenv("INTERNATIONAL_CARDS_ENABLED", "true")
	approveInternationalSchedule(t)
	ops, _ := h.operator()
	status := ops.expect(200, "GET", "/api/v1/ops/system", nil)
	intl := status["international_cards"].(map[string]any)
	if intl["enabled"] != true || intl["fee_schedule_approved"] != true {
		t.Fatalf("international card status: %v", intl)
	}
}
