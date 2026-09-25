package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

func (a *API) providerCheckoutConfigured() bool {
	return os.Getenv("CHECKOUTS_PAUSED") != "true" && a.providerEnvironmentConfigured()
}

// providerEnvironmentConfigured gates everything that talks to Kora. Existing
// payment events must stay processable while new checkouts are paused, so
// this is independent of CHECKOUTS_PAUSED. At least one enabled market must
// have its payment channels and limits set; each currency is checked again
// when a buyer pays (checkoutReadyFor).
func (a *API) providerEnvironmentConfigured() bool {
	if os.Getenv("PAYMENTS_ENABLED") != "true" || strings.TrimSpace(os.Getenv("FEE_POLICY_APPROVED")) != "true" || !paymentRouteHoldPayout() {
		return false
	}
	bps, err := strconv.Atoi(os.Getenv("FEE_BPS"))
	if err != nil || bps < 0 || bps > 500 || os.Getenv("FEE_POLICY_MODE") != "all_in_seller_deduction" {
		return false
	}
	anyMarket := false
	for _, m := range enabledMarkets() {
		if _, chErr := approvedChannelsFor(m.Currency); chErr != nil {
			continue
		}
		if _, _, limitErr := chargeLimits(m.Currency); limitErr != nil {
			continue
		}
		anyMarket = true
	}
	if !anyMarket {
		return false
	}
	secret := os.Getenv("KORA_SECRET_KEY")
	if a.env == "production" {
		return os.Getenv("PAYMENT_ENV") == "live" && os.Getenv("LIVE_PAYMENTS_ENABLED") == "true" && strings.TrimSpace(os.Getenv("PAYMENT_APPROVAL_ID")) != "" && strings.HasPrefix(secret, "sk_live_")
	}
	return os.Getenv("PAYMENT_ENV") == "sandbox" && strings.HasPrefix(secret, "sk_test_")
}

// approvedPlatformFee is WantMyTime's share of a price, within the charge
// limits for the currency.
func approvedPlatformFee(amount int64, currency string) (int64, int, error) {
	bps, err := strconv.Atoi(os.Getenv("FEE_BPS"))
	if err != nil || bps < 0 || bps > 500 {
		return 0, 0, errors.New("fee rate must be between zero and five percent")
	}
	platformFee := (amount/10000)*int64(bps) + ((amount%10000)*int64(bps))/10000
	min, max, err := chargeLimits(currency)
	if err != nil {
		return 0, 0, err
	}
	if amount < min || amount > max {
		return 0, 0, errors.New("this price is outside the range WantMyTime can take payment for")
	}
	return platformFee, bps, nil
}

func expectedProcessorCost(amount int64, bps int, fixed int64, cap *int64) int64 {
	maxInt64 := int64(^uint64(0) >> 1)
	if amount < 0 || bps < 0 || fixed < 0 {
		return maxInt64
	}
	base := (amount/10000)*int64(bps) + ((amount%10000)*int64(bps)+9999)/10000
	if fixed > 0 && base > maxInt64-fixed {
		return maxInt64
	}
	cost := base + fixed
	if cap != nil && cost > *cap {
		cost = *cap
	}
	return cost
}

func (a *API) channelFee(ctx context.Context, currency, channel string) (store.ApprovedChannelFeeRow, error) {
	return store.New(a.db).ApprovedChannelFee(ctx, store.ApprovedChannelFeeParams{Currency: strings.ToUpper(strings.TrimSpace(currency)), Channel: channel})
}

// buyerTransferFee estimates the fee Kora will add for the buyer on a price,
// from the approved fee schedule for the channel. It is only an estimate
// shown before paying: Kora itself adds its current fee at checkout.
func (a *API) buyerTransferFee(ctx context.Context, currency, channel string, price int64) (int64, error) {
	fee, err := a.channelFee(ctx, currency, channel)
	if err != nil {
		return 0, err
	}
	bps, fixed, cap := int(fee.PercentBps), fee.FixedMinor, fee.CapMinor
	if bps < 0 || bps >= 10000 || fixed < 0 || (cap != nil && *cap < 0) {
		return 0, fmt.Errorf("channel %s has an invalid approved fee schedule", channel)
	}
	return expectedProcessorCost(price, bps, fixed, cap), nil
}

// methodFees is the fee a buyer would add for each way of paying, so the
// checkout can show the total before they choose. Methods without an
// approved fee schedule are left out.
func (a *API) methodFees(ctx context.Context, currency string, price int64) map[string]int64 {
	out := map[string]int64{}
	if price <= 0 || !a.checkoutReadyFor(currency) {
		return out
	}
	for _, channel := range paymentMethodsFor(currency) {
		if fee, err := a.buyerTransferFee(ctx, currency, channel, price); err == nil {
			out[channel] = fee
		}
	}
	return out
}

// transferFeeEstimate is the fee a buyer would add for the default payment
// method, or 0 when payments aren't set up.
func (a *API) transferFeeEstimate(ctx context.Context, currency string, price int64) int64 {
	methods := paymentMethodsFor(currency)
	if len(methods) == 0 {
		return 0
	}
	return a.methodFees(ctx, currency, price)[methods[0]]
}

func (a *API) collection() (*koraClient, error) { return newKoraClient(a.env) }

func webhookURL() string {
	return strings.TrimRight(envOr("PUBLIC_APP_ORIGIN", ""), "/") + "/api/v1/webhooks/kora"
}

// initializeQuoteCheckout starts paying for a held quote. For a Nigerian
// seller the buyer gets a one-off bank account to transfer the exact amount
// to, shown in WantMyTime; pay with bank and mobile money open Kora's
// hosted payment page instead.
func (a *API) initializeQuoteCheckout(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && !guestHas(scope, "quote_ids", r.PathValue("id")) {
		problem(w, 404, "NOT_FOUND", "This quote is not available.")
		return
	}
	if !a.providerCheckoutConfigured() {
		problem(w, 503, "PAYMENTS_DISABLED", "Checkout is disabled until provider credentials, approved channel costs and commercial controls are configured.")
		return
	}
	var in struct {
		Method string `json:"method"`
	}
	if r.ContentLength != 0 {
		if decode(r, &in) != nil {
			problem(w, 422, "INVALID_METHOD", "Choose how you want to pay.")
			return
		}
	}
	client, err := a.collection()
	if err != nil {
		problem(w, 503, "PAYMENTS_DISABLED", "Payment credentials do not match this environment.")
		return
	}
	queries := store.New(a.db)
	quote, err := queries.CheckoutQuote(r.Context(), store.CheckoutQuoteParams{QuoteID: r.PathValue("id"), BuyerUserID: u.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This quote is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be prepared.")
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(quote.Currency))
	channels, err := approvedChannelsFor(currency)
	if err != nil {
		problem(w, 503, "PAYMENT_CHANNELS_UNAVAILABLE", "Payments in this currency are not switched on yet. Your time is still held.")
		return
	}
	method := in.Method
	if method == "" {
		method = channels[0]
	}
	if !contains(channels, method) {
		problem(w, 422, "METHOD_NOT_AVAILABLE", "That way of paying is not available for this booking.")
		return
	}
	if quote.State != "held" || !quote.ExpiresAt.After(time.Now()) || quote.Paused || !quote.Ready {
		problem(w, 409, "CHECKOUT_NOT_READY", "The quote, seller readiness or time hold is no longer valid.")
		return
	}
	if !quote.HasPayoutAccount {
		problem(w, 503, "SELLER_PAYOUT_NOT_READY", "This seller has not added a payout account yet.")
		return
	}
	price := quote.GrossMinor
	platformFee, bps, err := approvedPlatformFee(price, currency)
	if err != nil {
		problem(w, 422, "AMOUNT_NOT_APPROVED", err.Error())
		return
	}
	// The buyer pays Kora's fee on top, at Kora's current rate: Kora adds it
	// itself. An approved fee schedule, when there is one, only gives the
	// buyer an estimate before a hosted payment page shows the exact total.
	estimate, estErr := a.buyerTransferFee(r.Context(), currency, method, price)
	if estErr != nil {
		estimate = 0
	}
	hosted := method != "bank_transfer"
	// Reuse an attempt the buyer can still complete: the same transfer
	// account, or the same payment page, rather than creating a second charge.
	var existingID, existingRef, existingState, existingURL string
	var existingDetails []byte
	var existingExpiry *time.Time
	var existingTotal, existingFee int64
	err = a.db.QueryRow(r.Context(), `SELECT id::text,merchant_reference,canonical_state,COALESCE(authorization_url,''),transfer_details,instructions_expire_at,expected_minor,buyer_fee_minor FROM payment_attempts WHERE quote_id=$1 AND channel=$2 AND canonical_state='awaiting_payment' ORDER BY created_at DESC LIMIT 1`, quote.ID, method).Scan(&existingID, &existingRef, &existingState, &existingURL, &existingDetails, &existingExpiry, &existingTotal, &existingFee)
	if err == nil {
		if hosted && existingURL != "" {
			jsonOut(w, 200, map[string]any{"payment_attempt_id": existingID, "reference": existingRef, "method": method, "authorization_url": existingURL, "state": "awaiting_payment", "price_minor": price, "fee_minor": estimate, "total_minor": price + estimate, "fee_is_estimate": true, "currency": currency})
			return
		}
		if !hosted && existingExpiry != nil && existingExpiry.After(time.Now().Add(2*time.Minute)) && len(existingDetails) > 0 {
			jsonOut(w, 200, map[string]any{"payment_attempt_id": existingID, "reference": existingRef, "method": method, "transfer": json.RawMessage(existingDetails), "state": "awaiting_payment", "price_minor": price, "fee_minor": existingFee, "total_minor": existingTotal, "fee_is_estimate": false, "currency": currency})
			return
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be prepared.")
		return
	}
	// Extend the hold before the buyer starts paying.
	if !a.extendHold(r.Context(), w, quote.ID, time.Now().Add(checkoutHoldDuration())) {
		return
	}
	var previous int
	if err = a.db.QueryRow(r.Context(), `SELECT count(*) FROM payment_attempts WHERE quote_id=$1`, quote.ID).Scan(&previous); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be prepared.")
		return
	}
	letter := map[string]string{"bank_transfer": "t", "pay_with_bank": "b", "mobile_money": "m"}[method]
	reference := fmt.Sprintf("wmt-%s-%s%d", strings.ReplaceAll(quote.ID, "-", ""), letter, previous+1)
	environment := environmentOf(a)
	attempt, err := queries.UpsertPaymentAttempt(r.Context(), store.UpsertPaymentAttemptParams{QuoteID: quote.ID, Environment: environment, Reference: reference, ExpectedMinor: price, Currency: currency, ApprovedFeeMinor: platformFee, FeeBasisPoints: int32(bps), Channel: method, BuyerFeeMinor: 0})
	if err != nil {
		problem(w, 503, "PAYMENT_INTENT_UNAVAILABLE", "The checkout could not be saved safely.")
		return
	}
	if claimed, claimErr := queries.ClaimPaymentInitialization(r.Context(), attempt.ID); claimErr != nil || claimed != 1 {
		problem(w, 409, "PAYMENT_ALREADY_STARTING", "This payment is already being set up. Refresh the page.")
		return
	}
	origin := strings.TrimRight(envOr("PUBLIC_APP_ORIGIN", ""), "/")
	if origin == "" {
		problem(w, 503, "PUBLIC_ORIGIN_REQUIRED", "The payment return address is not configured.")
		return
	}
	if a.env == "production" {
		if parsed, e := url.Parse(origin); e != nil || parsed.Scheme != "https" || parsed.Host == "" {
			problem(w, 503, "PUBLIC_ORIGIN_REQUIRED", "A secure approved return domain is required.")
			return
		}
	}
	req := checkoutRequest{Reference: reference, AmountMinor: price, Currency: currency, Email: quote.BuyerEmail, Name: quote.BuyerName, WebhookURL: webhookURL(),
		Narration: "WantMyTime booking", QuoteID: quote.ID, Channels: []string{method},
		RedirectURL: origin + "/payment/return?quote_id=" + url.QueryEscape(quote.ID) + "&reference=" + url.QueryEscape(reference)}
	if hosted {
		checkoutURL, startErr := client.startCheckout(r.Context(), req)
		if startErr != nil {
			_ = queries.MarkInitializationUnknown(r.Context(), attempt.ID)
			problem(w, 503, "PAYMENT_INITIALIZATION_UNKNOWN", "The payment page could not be opened. Your time is still held; try again.")
			return
		}
		if saved, saveErr := queries.SavePaymentAuthorization(r.Context(), store.SavePaymentAuthorizationParams{AuthorizationUrl: checkoutURL, AccessCode: "", ID: attempt.ID}); saveErr != nil || saved != 1 {
			problem(w, 503, "PAYMENT_INITIALIZATION_UNKNOWN", "The payment page opened but could not be saved. Refresh and try again.")
			return
		}
		jsonOut(w, 200, map[string]any{"payment_attempt_id": attempt.ID, "reference": reference, "method": method, "authorization_url": checkoutURL, "state": "awaiting_payment", "price_minor": price, "fee_minor": estimate, "total_minor": price + estimate, "fee_is_estimate": true, "currency": currency})
		return
	}
	details, err := client.startBankTransfer(r.Context(), req)
	if err != nil {
		_ = queries.MarkInitializationUnknown(r.Context(), attempt.ID)
		problem(w, 503, "PAYMENT_INITIALIZATION_UNKNOWN", "A transfer account could not be created. Your time is still held; try again.")
		return
	}
	// Keep the time until shortly after the account expires, so a transfer
	// made at the last minute still gets its booking (up to three hours).
	until := details.ExpiresAt.Add(10 * time.Minute)
	if limit := time.Now().Add(3 * time.Hour); until.After(limit) {
		until = limit
	}
	if !a.extendHold(r.Context(), w, quote.ID, until) {
		return
	}
	// Kora quoted the exact total with its fee: record it on the attempt.
	if _, err = a.db.Exec(r.Context(), `UPDATE payment_attempts SET expected_minor=$1, buyer_fee_minor=$2, updated_at=now() WHERE id=$3`, details.AmountMinor, details.FeeMinor, attempt.ID); err != nil {
		problem(w, 503, "PAYMENT_INITIALIZATION_UNKNOWN", "The transfer account could not be saved. Refresh and try again; do not transfer yet.")
		return
	}
	raw, _ := json.Marshal(details)
	if saved, saveErr := queries.SaveTransferInstructions(r.Context(), store.SaveTransferInstructionsParams{Details: raw, ExpiresAt: details.ExpiresAt, ID: attempt.ID}); saveErr != nil || saved != 1 {
		problem(w, 503, "PAYMENT_INITIALIZATION_UNKNOWN", "The transfer account could not be saved. Refresh and try again; do not transfer yet.")
		return
	}
	jsonOut(w, 200, map[string]any{"payment_attempt_id": attempt.ID, "reference": reference, "method": method, "transfer": details, "state": "awaiting_payment", "price_minor": price, "fee_minor": details.FeeMinor, "total_minor": details.AmountMinor, "fee_is_estimate": false, "currency": currency})
}

func (a *API) extendHold(ctx context.Context, w http.ResponseWriter, quoteID string, until time.Time) bool {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be prepared.")
		return false
	}
	defer tx.Rollback(ctx)
	extended, err := extendCheckoutHold(ctx, tx, quoteID, until)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be prepared.")
		return false
	}
	if !extended {
		problem(w, 409, "CHECKOUT_NOT_READY", "This time hold has expired. Choose another time.")
		return false
	}
	if err = tx.Commit(ctx); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be prepared.")
		return false
	}
	return true
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// --- webhook ------------------------------------------------------------------------

// koraWebhook takes Kora's signed notifications. Charges are queued for
// independent verification; refund and transfer notices bring the next
// status check forward. Nothing is trusted from the body beyond the reference.
func (a *API) koraWebhook(w http.ResponseWriter, r *http.Request) {
	if !a.providerEnvironmentConfigured() {
		problem(w, 503, "PAYMENTS_DISABLED", "Provider webhook intake is disabled until an approved payment environment is configured.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		problem(w, 413, "WEBHOOK_TOO_LARGE", "Provider event exceeds the intake limit.")
		return
	}
	if !validKoraSignature(raw, r.Header.Get("x-korapay-signature"), os.Getenv("KORA_SECRET_KEY")) {
		problem(w, 401, "INVALID_PROVIDER_SIGNATURE", "Provider signature is invalid.")
		return
	}
	var event struct {
		Event string `json:"event"`
		Data  struct {
			Reference        string    `json:"reference"`
			PaymentReference string    `json:"payment_reference"`
			Status           string    `json:"status"`
			Amount           koraMoney `json:"amount"`
			Currency         string    `json:"currency"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &event) != nil || event.Event == "" {
		problem(w, 400, "INVALID_PROVIDER_EVENT", "Provider event could not be decoded.")
		return
	}
	reference := strings.TrimSpace(event.Data.Reference)
	chargeEvent := strings.HasPrefix(event.Event, "charge.")
	if chargeEvent && !validReference(reference) {
		problem(w, 400, "INVALID_PROVIDER_EVENT", "Provider event identity is missing.")
		return
	}
	state := "ignored"
	if chargeEvent {
		state = "queued"
	}
	payload, _ := json.Marshal(map[string]any{"reference": reference, "status": event.Data.Status, "amount_minor": int64(event.Data.Amount), "currency": event.Data.Currency, "payment_reference": event.Data.PaymentReference})
	digest := sha256.Sum256(raw)
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "WEBHOOK_NOT_STORED", "Provider event was not acknowledged because it could not be durably stored.")
		return
	}
	defer tx.Rollback(r.Context())
	if err = store.New(tx).InsertProviderEvent(r.Context(), store.InsertProviderEventParams{Environment: environmentOf(a), Digest: digest[:], State: state, EventType: event.Event, Reference: reference, Payload: payload}); err != nil {
		problem(w, 503, "WEBHOOK_NOT_STORED", "Provider event was not acknowledged because it could not be durably stored.")
		return
	}
	switch {
	case strings.HasPrefix(event.Event, "refund."):
		_, err = tx.Exec(r.Context(), `UPDATE refunds SET next_attempt_at=now(),updated_at=now() WHERE provider_refund_id=$1 AND state='submitted'`, reference)
	case strings.HasPrefix(event.Event, "transfer."):
		_, err = tx.Exec(r.Context(), `UPDATE seller_payouts SET next_attempt_at=now() WHERE reference=$1 AND state='processing'`, reference)
	}
	if err != nil {
		problem(w, 503, "WEBHOOK_NOT_STORED", "Provider event was not acknowledged because it could not be durably stored.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "WEBHOOK_NOT_STORED", "Provider event was not acknowledged because it could not be durably stored.")
		return
	}
	if event.Event == "transfer.failed" {
		// A payout already recorded as paid that later fails was returned by
		// the bank; confirmed with Kora before anything changes.
		if err = a.checkPayoutReversal(r.Context(), reference); err != nil {
			a.log().ErrorContext(r.Context(), "payout reversal check failed", "reference", reference, "error", err.Error())
		}
	}
	w.WriteHeader(http.StatusOK)
}

// paymentRouteHoldPayout reports whether the collect, hold and pay-out route is
// configured. "escrow_payout" is the older name and is still accepted.
func paymentRouteHoldPayout() bool {
	r := strings.TrimSpace(os.Getenv("PAYMENT_ROUTE"))
	return r == "hold_payout" || r == "escrow_payout"
}
