package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

// verifyQuotePayment is the browser return path. Redirect parameters are only
// references; the provider API verification decides whether money was paid.
func (a *API) verifyQuotePayment(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && !guestHas(scope, "quote_ids", r.PathValue("id")) {
		problem(w, 404, "PAYMENT_NOT_FOUND", "This payment attempt is not available.")
		return
	}
	var body struct {
		Reference string `json:"reference"`
	}
	if err := decode(r, &body); err != nil || strings.TrimSpace(body.Reference) == "" {
		problem(w, 422, "REFERENCE_REQUIRED", "A payment reference is required.")
		return
	}
	owns, err := store.New(a.db).PaymentAttemptOwnedByBuyer(r.Context(), store.PaymentAttemptOwnedByBuyerParams{QuoteID: r.PathValue("id"), BuyerUserID: u.ID, Environment: environmentOf(a), Reference: body.Reference})
	if err != nil || !owns {
		problem(w, 404, "PAYMENT_NOT_FOUND", "This payment attempt is not available.")
		return
	}
	client, err := a.collection()
	if err != nil {
		problem(w, 503, "PAYMENTS_DISABLED", "Payment verification is not configured.")
		return
	}
	verified, err := client.queryCharge(r.Context(), body.Reference)
	if err != nil {
		problem(w, 503, "PAYMENT_STATUS_UNAVAILABLE", "Payment status could not be verified. You can safely check again shortly.")
		return
	}
	bookingID, err := a.applyVerifiedCharge(r.Context(), body.Reference, verified)
	if err != nil {
		problem(w, 503, "PAYMENT_STATUS_UNAVAILABLE", "The verified payment could not yet be recorded. The system will retry safely.")
		return
	}
	if bookingID == "" {
		state := verified.Status
		switch {
		case state == "success":
			state = "review"
		case state == "pending" && verified.PaidMinor > 0 && verified.PaidMinor < verified.AmountMinor:
			// A transfer short of the price is returned to the buyer by Kora.
			state = "underpaid"
		}
		jsonOut(w, 200, map[string]any{"state": state, "booking_id": nil})
		return
	}
	jsonOut(w, 200, map[string]any{"state": "paid", "booking_id": bookingID})
}

func (a *API) providerEventWorker(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := a.processOneProviderEvent(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				a.log().ErrorContext(ctx, "provider event processing deferred", "error", err.Error())
			}
			a.beat(ctx, "provider_events", err)
		}
	}
}

func (a *API) processOneProviderEvent(ctx context.Context) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)
	event, err := q.ClaimNextProviderEvent(ctx, environmentOf(a))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = q.MarkProviderEventProcessing(ctx, event.ID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if event.ProviderReference == nil || *event.ProviderReference == "" {
		return a.failProviderEvent(ctx, event.ID, "REFERENCE_MISSING", false)
	}
	reference := *event.ProviderReference
	client, err := a.collection()
	if err != nil {
		return a.failProviderEvent(ctx, event.ID, "CREDENTIALS_UNAVAILABLE", true)
	}
	verified, err := client.queryCharge(ctx, reference)
	if err != nil {
		return a.failProviderEvent(ctx, event.ID, "PROVIDER_VERIFY_FAILED", true)
	}
	if _, err = a.applyVerifiedCharge(ctx, reference, verified); err != nil {
		return a.failProviderEvent(ctx, event.ID, "PAYMENT_APPLY_FAILED", true)
	}
	return store.New(a.db).MarkProviderEventProcessed(ctx, event.ID)
}

func (a *API) failProviderEvent(ctx context.Context, id, code string, retry bool) error {
	return store.New(a.db).FailProviderEvent(ctx, store.FailProviderEventParams{Retry: retry, ErrorCode: code, ID: id})
}

// paymentOutcome records a verified charge that cannot become a booking: it
// opens a payment exception and marks the attempt, in the caller's transaction.
func (a *API) recordPaymentException(ctx context.Context, q *store.Queries, attempt store.LockPaymentAttemptByReferenceRow, txn *string, kind, reference string, amount int64, currency, reason string) error {
	if err := addPaymentException(ctx, q, attempt.ID, derefString(attempt.QuoteID), kind, reference, amount, currency, reason); err != nil {
		return err
	}
	return q.MarkPaymentAttemptException(ctx, store.MarkPaymentAttemptExceptionParams{TransactionID: txn, ID: attempt.ID})
}

// applyVerifiedCharge turns a charge confirmed with the provider into a paid
// booking, or records why it cannot be one. It is idempotent.
func (a *API) applyVerifiedCharge(ctx context.Context, reference string, provider verifiedCharge) (string, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)
	attempt, err := q.LockPaymentAttemptByReference(ctx, store.LockPaymentAttemptByReferenceParams{Environment: environmentOf(a), Reference: reference})
	if err != nil {
		return "", err
	}
	if attempt.QuoteID == nil || attempt.ApprovedFeeMinor == nil || attempt.FeeBasisPoints == nil {
		return "", errors.New("payment attempt is missing its immutable checkout snapshot")
	}
	quoteID := *attempt.QuoteID
	expected := attempt.ExpectedMinor
	expectedCurrency := strings.TrimSpace(attempt.Currency)
	platformFee := *attempt.ApprovedFeeMinor
	bps := *attempt.FeeBasisPoints
	if platformFee < 0 || platformFee > expected || bps < 0 || bps > 500 {
		return "", errors.New("payment attempt has invalid immutable fee snapshot")
	}
	var txn *string
	if provider.TransactionID != "" {
		id := provider.TransactionID
		txn = &id
	}
	exception := func(kind string, amount int64, currency, reason string) (string, error) {
		if err := a.recordPaymentException(ctx, q, attempt, txn, kind, reference, amount, currency, reason); err != nil {
			return "", err
		}
		return "", tx.Commit(ctx)
	}
	if provider.Status != "success" {
		state := provider.Status
		if state == "" {
			state = "failed"
		}
		if err = q.RecordPaymentAttemptStatus(ctx, store.RecordPaymentAttemptStatusParams{State: state, TransactionID: txn, ID: attempt.ID}); err != nil {
			return "", err
		}
		return "", tx.Commit(ctx)
	}
	if provider.Reference != reference || provider.Currency != expectedCurrency || provider.AmountMinor != expected {
		kind := "wrong_amount"
		if provider.Currency != expectedCurrency {
			kind = "wrong_currency"
		}
		return exception(kind, provider.AmountMinor, provider.Currency, fmt.Sprintf("The provider confirmed %d %s; the booking needs %d %s.", provider.AmountMinor, provider.Currency, expected, expectedCurrency))
	}
	channel := provider.Channel
	if channel == "" {
		channel = attempt.Channel
	}
	if err = q.RecordPaymentChannel(ctx, store.RecordPaymentChannelParams{Channel: channel, PaidMinor: provider.PaidMinor, ID: attempt.ID}); err != nil {
		return "", err
	}
	if provider.CardCountry != "" {
		country := provider.CardCountry
		if err = q.RecordPaymentCard(ctx, store.RecordPaymentCardParams{CardCountry: &country, ID: attempt.ID}); err != nil {
			return "", err
		}
	}
	if provider.FeeMinor < 0 {
		return exception("settlement_mismatch", provider.FeeMinor, expectedCurrency, "Verified processor cost is negative.")
	}
	if provider.FeeMinor > platformFee {
		// A card issued abroad costs more than a local one, and the checkout
		// could not know which card the buyer would use. When international
		// cards are enabled and the fee fits the approved international
		// schedule, the platform absorbs the difference instead of leaving a
		// paid buyer without a booking. Anything else needs review.
		ok, reason := a.acceptInternationalCardFee(ctx, q, provider, expected)
		if !ok {
			return exception("settlement_mismatch", provider.FeeMinor, expectedCurrency, reason)
		}
		a.log().InfoContext(ctx, "international card fee absorbed", "reference", reference, "card_country", provider.CardCountry, "processor_fee_minor", provider.FeeMinor, "platform_fee_minor", platformFee)
	}
	if attempt.BookingID == nil {
		// Another attempt for this quote (say, a card after starting a
		// transfer) may already have paid for it: this one is a second charge.
		if existing, lookupErr := q.BookingIDForQuote(ctx, quoteID); lookupErr == nil && existing != "" {
			if err = addPaymentException(ctx, q, attempt.ID, quoteID, "duplicate_charge", reference, provider.AmountMinor, provider.Currency, "The buyer paid twice for one booking (two payment attempts). Refund this one."); err != nil {
				return "", err
			}
			if err = q.MarkPaymentAttemptException(ctx, store.MarkPaymentAttemptExceptionParams{TransactionID: txn, ID: attempt.ID}); err != nil {
				return "", err
			}
			return "", tx.Commit(ctx)
		} else if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
			return "", lookupErr
		}
	}
	existing, err := q.BookingIDForQuote(ctx, quoteID)
	if err == nil {
		if err = q.MarkPaymentAttemptSuccess(ctx, store.MarkPaymentAttemptSuccessParams{TransactionID: txn, ID: attempt.ID}); err != nil {
			return "", err
		}
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	quote, err := q.LockQuoteForPayment(ctx, quoteID)
	if err != nil {
		return "", err
	}
	hold, err := q.LockHoldForQuote(ctx, quoteID)
	if errors.Is(err, pgx.ErrNoRows) {
		hold = store.LockHoldForQuoteRow{}
	} else if err != nil {
		return "", err
	}
	now := time.Now()
	if quote.State != "held" || !quote.ExpiresAt.After(now) || !hold.Active || hold.ExpiresAt == nil || !hold.ExpiresAt.After(now) {
		return exception("payment_without_slot", expected, expectedCurrency, "Verified charge arrived after the slot hold expired or was released.")
	}
	sellerAvailable, err := q.LockSellerAvailability(ctx, quote.SellerID)
	if err != nil {
		return "", err
	}
	if !sellerAvailable {
		return exception("payment_without_slot", expected, expectedCurrency, "Verified charge belongs to a seller who is no longer available.")
	}
	if quote.OfferID != nil {
		// The quote hold was created while the agreement was open, and the hold
		// is still valid (checked above). Only a changed agreement blocks it.
		offerState, offerErr := q.LockOfferState(ctx, *quote.OfferID)
		if offerErr != nil {
			return "", offerErr
		}
		if offerState != "agreed" && offerState != "expired" {
			return exception("offer_conflict", expected, expectedCurrency, "Verified charge belongs to an offer that was withdrawn, declined or already converted.")
		}
	}
	bookingID, err := randomUUID()
	if err != nil {
		return "", err
	}
	buyerEmail, err := q.VerifiedEmailForUser(ctx, quote.BuyerUserID)
	if err != nil {
		return "", err
	}
	if _, err = q.CreatePaidBookingFromQuote(ctx, store.CreatePaidBookingFromQuoteParams{BookingID: bookingID, BuyerEmail: buyerEmail, QuoteID: quoteID}); err != nil {
		return "", err
	}
	sellerID := quote.SellerID
	if err = q.RecordServerProductEvent(ctx, store.RecordServerProductEventParams{EventName: "verified_booking_paid", SubjectHash: analyticsSubjectHash(quote.BuyerUserID), Environment: environmentOf(a), SellerID: &sellerID}); err != nil {
		return "", err
	}
	if err = q.CreatePaymentAllocation(ctx, store.CreatePaymentAllocationParams{BookingID: bookingID, GrossMinor: expected, DeductionMinor: platformFee, SellerEntitlementMinor: expected - platformFee, ProcessorCostMinor: provider.FeeMinor, Environment: environmentOf(a), Reference: reference, FeeBasisPoints: bps}); err != nil {
		return "", err
	}
	if err = q.CreateSettlementItemForBooking(ctx, store.CreateSettlementItemForBookingParams{Reference: reference, BookingID: bookingID}); err != nil {
		return "", err
	}
	if err = scheduleSellerPayout(ctx, tx, "kora", bookingID, sellerID, expected-platformFee, expectedCurrency, fundsAvailableAt(channel, time.Now())); err != nil {
		return "", err
	}
	if err = q.MarkPaymentAttemptSuccess(ctx, store.MarkPaymentAttemptSuccessParams{BookingID: &bookingID, TransactionID: txn, ID: attempt.ID}); err != nil {
		return "", err
	}
	if err = q.ConvertQuote(ctx, quoteID); err != nil {
		return "", err
	}
	if quote.OfferID != nil {
		converted, convertErr := q.ConvertOffer(ctx, store.ConvertOfferParams{BookingID: bookingID, ID: *quote.OfferID})
		if convertErr != nil || converted != 1 {
			return "", errors.New("agreed offer changed before payment confirmation")
		}
	}
	if err = q.ConfirmHoldAsBooking(ctx, store.ConfirmHoldAsBookingParams{BookingID: bookingID, QuoteID: quoteID}); err != nil {
		return "", err
	}
	if err = enqueueBookingNotifications(ctx, tx, bookingID); err != nil {
		return "", err
	}
	lines := []ledgerLine{{AccountCode: "provider_receivable", Side: "debit", Amount: expected - provider.FeeMinor}}
	if provider.FeeMinor > 0 {
		lines = append(lines, ledgerLine{AccountCode: "processor_fee_expense", Side: "debit", Amount: provider.FeeMinor})
	}
	if platformFee > 0 {
		lines = append(lines, ledgerLine{AccountCode: "platform_fee_revenue", Side: "credit", Amount: platformFee})
	}
	if expected-platformFee > 0 {
		lines = append(lines, ledgerLine{AccountCode: "seller_payable", ScopeID: &sellerID, Side: "credit", Amount: expected - platformFee})
	}
	if _, err = postLedgerJournal(ctx, tx, "payment_attempt", attempt.ID, expectedCurrency, "verified payment", lines); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return bookingID, nil
}

// internationalCardsEnabled reports whether foreign-issued cards are accepted.
// International card payments must also be enabled on the Kora account.
func internationalCardsEnabled() bool {
	return os.Getenv("INTERNATIONAL_CARDS_ENABLED") == "true"
}

// acceptInternationalCardFee decides whether a processor fee above the
// platform fee is an expected international-card fee.
func (a *API) acceptInternationalCardFee(ctx context.Context, q *store.Queries, provider verifiedCharge, amount int64) (bool, string) {
	if provider.Channel != "card" || provider.CardCountry == "NG" {
		return false, "Verified processor cost exceeds the approved platform fee snapshot."
	}
	if !internationalCardsEnabled() {
		return false, "An international card was charged while international cards are disabled; its fee exceeds the platform fee snapshot."
	}
	fee, err := q.ApprovedChannelFee(ctx, "card_international")
	if err != nil {
		return false, "An international card was charged but no approved international card fee schedule exists."
	}
	if provider.FeeMinor > expectedProcessorCost(amount, int(fee.PercentBps), fee.FixedMinor, fee.CapMinor) {
		return false, "The international card fee exceeds the approved international fee schedule."
	}
	return true, ""
}

func environmentOf(a *API) string {
	if a.env == "production" {
		return "live"
	}
	return "sandbox"
}

func addPaymentException(ctx context.Context, q *store.Queries, attemptID, quoteID, kind, reference string, amount int64, currency, reason string) error {
	return q.AddPaymentException(ctx, store.AddPaymentExceptionParams{PaymentAttemptID: attemptID, Kind: kind, Reference: reference, AmountMinor: amount, Currency: strings.TrimSpace(currency), Reason: reason, QuoteID: quoteID})
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// fundsAvailableAt is when a payment's money is in the balance and can be
// paid out: at once for a bank transfer, after card settlement (the next
// working day) for a card. CARD_SETTLEMENT_HOURS overrides the card wait.
func fundsAvailableAt(channel string, paid time.Time) time.Time {
	if channel == "bank_transfer" {
		return paid
	}
	hours, err := strconv.Atoi(os.Getenv("CARD_SETTLEMENT_HOURS"))
	if err != nil || hours < 0 || hours > 24*7 {
		hours = 24
	}
	at := paid.Add(time.Duration(hours) * time.Hour)
	// Settlement does not run at weekends.
	for at.Weekday() == time.Saturday || at.Weekday() == time.Sunday {
		at = at.Add(24 * time.Hour)
	}
	return at
}
