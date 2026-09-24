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
// this is independent of CHECKOUTS_PAUSED.
func (a *API) providerEnvironmentConfigured() bool {
	if os.Getenv("PAYMENTS_ENABLED") != "true" || strings.TrimSpace(os.Getenv("FEE_POLICY_APPROVED")) != "true" || os.Getenv("PAYMENT_ROUTE") != "escrow_payout" {
		return false
	}
	bps, err := strconv.Atoi(os.Getenv("FEE_BPS"))
	if err != nil || bps < 0 || bps > 500 || os.Getenv("FEE_POLICY_MODE") != "all_in_seller_deduction" {
		return false
	}
	min, minErr := strconv.ParseInt(os.Getenv("MIN_CHARGE_MINOR"), 10, 64)
	max, maxErr := strconv.ParseInt(os.Getenv("MAX_CHARGE_MINOR"), 10, 64)
	if minErr != nil || maxErr != nil || min <= 0 || max < min {
		return false
	}
	if _, err = approvedChannels(); err != nil {
		return false
	}
	secret := os.Getenv("KORA_SECRET_KEY")
	if a.env == "production" {
		return os.Getenv("PAYMENT_ENV") == "live" && os.Getenv("LIVE_PAYMENTS_ENABLED") == "true" && strings.TrimSpace(os.Getenv("PAYMENT_APPROVAL_ID")) != "" && strings.HasPrefix(secret, "sk_live_")
	}
	return os.Getenv("PAYMENT_ENV") == "sandbox" && strings.HasPrefix(secret, "sk_test_")
}

// approvedChannels lists the ways buyers may pay, bank transfer first when it
// is approved: nearly every Nigerian pays by transfer, and transfers settle
// at once.
func approvedChannels() ([]string, error) {
	allowed := map[string]bool{"bank_transfer": true, "card": true, "pay_with_bank": true}
	out := []string{}
	seen := map[string]bool{}
	for _, item := range strings.Split(os.Getenv("APPROVED_PAYMENT_CHANNELS"), ",") {
		ch := strings.TrimSpace(item)
		if ch == "" {
			continue
		}
		if !allowed[ch] {
			return nil, fmt.Errorf("payment channel %q is not supported", ch)
		}
		if !seen[ch] {
			seen[ch] = true
			out = append(out, ch)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("approved payment channels are required")
	}
	for i, ch := range out {
		if ch == "bank_transfer" && i > 0 {
			out = append([]string{"bank_transfer"}, append(out[:i:i], out[i+1:]...)...)
			break
		}
	}
	return out, nil
}

func approvedPlatformFee(amount int64) (int64, int, error) {
	bps, err := strconv.Atoi(os.Getenv("FEE_BPS"))
	if err != nil || bps < 0 || bps > 500 {
		return 0, 0, errors.New("fee rate must be between zero and five percent")
	}
	platformFee := (amount/10000)*int64(bps) + ((amount%10000)*int64(bps))/10000
	min, err := strconv.ParseInt(os.Getenv("MIN_CHARGE_MINOR"), 10, 64)
	if err != nil || min <= 0 {
		return 0, 0, errors.New("minimum payment amount is not configured")
	}
	max, err := strconv.ParseInt(os.Getenv("MAX_CHARGE_MINOR"), 10, 64)
	if err != nil || max < min {
		return 0, 0, errors.New("maximum payment amount is not configured")
	}
	if amount < min || amount > max {
		return 0, 0, errors.New("payment amount is outside the approved range")
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

func (a *API) ensureChannelEconomics(ctx context.Context, amount, platformFee int64, channels []string) error {
	for _, channel := range channels {
		fee, err := store.New(a.db).ApprovedChannelFee(ctx, channel)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no approved fee schedule for channel %s", channel)
		}
		if err != nil {
			return err
		}
		bps, fixed, cap := int(fee.PercentBps), fee.FixedMinor, fee.CapMinor
		if bps < 0 || bps > 10000 || fixed < 0 || (cap != nil && *cap < 0) {
			return fmt.Errorf("channel %s has an invalid approved fee schedule", channel)
		}
		if expectedProcessorCost(amount, bps, fixed, cap) > platformFee {
			return fmt.Errorf("channel %s costs more than the approved platform fee", channel)
		}
	}
	return nil
}

func (a *API) collection() (*koraClient, error) { return newKoraClient(a.env) }

func webhookURL() string {
	return strings.TrimRight(envOr("PUBLIC_APP_ORIGIN", ""), "/") + "/api/v1/webhooks/kora"
}

// initializeQuoteCheckout starts paying for a held quote. By default the buyer
// gets a one-off bank account to transfer the exact price to, shown in WantMyTime;
// "card" opens Kora's hosted card page instead.
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
			problem(w, 422, "INVALID_METHOD", "Choose bank transfer or card.")
			return
		}
	}
	channels, err := approvedChannels()
	if err != nil {
		problem(w, 503, "PAYMENT_CHANNELS_UNAVAILABLE", "Approved payment channels are not configured.")
		return
	}
	method := in.Method
	if method == "" {
		method = channels[0]
	}
	if !contains(channels, method) {
		problem(w, 422, "METHOD_NOT_AVAILABLE", "That way of paying is not available.")
		return
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
	if quote.State != "held" || !quote.ExpiresAt.After(time.Now()) || quote.Paused || !quote.Ready {
		problem(w, 409, "CHECKOUT_NOT_READY", "The quote, seller readiness or time hold is no longer valid.")
		return
	}
	if !quote.HasPayoutAccount {
		problem(w, 503, "SELLER_PAYOUT_NOT_READY", "This seller has not added a bank account for payouts yet.")
		return
	}
	amount := quote.GrossMinor
	platformFee, bps, err := approvedPlatformFee(amount)
	if err != nil {
		problem(w, 422, "AMOUNT_NOT_APPROVED", err.Error())
		return
	}
	if err = a.ensureChannelEconomics(r.Context(), amount, platformFee, []string{method}); err != nil {
		problem(w, 503, "CHANNEL_COST_NOT_APPROVED", "This way of paying does not have an approved fee schedule within the five-percent limit.")
		return
	}
	// Reuse an attempt the buyer can still complete: the same transfer
	// account, or the same card page, rather than creating a second charge.
	var existingID, existingRef, existingState, existingURL string
	var existingDetails []byte
	var existingExpiry *time.Time
	err = a.db.QueryRow(r.Context(), `SELECT id::text,merchant_reference,canonical_state,COALESCE(authorization_url,''),transfer_details,instructions_expire_at FROM payment_attempts WHERE quote_id=$1 AND channel=$2 AND canonical_state='awaiting_payment' ORDER BY created_at DESC LIMIT 1`, quote.ID, method).Scan(&existingID, &existingRef, &existingState, &existingURL, &existingDetails, &existingExpiry)
	if err == nil {
		if method == "card" && existingURL != "" {
			jsonOut(w, 200, map[string]any{"payment_attempt_id": existingID, "reference": existingRef, "method": method, "authorization_url": existingURL, "state": "awaiting_payment"})
			return
		}
		if method != "card" && existingExpiry != nil && existingExpiry.After(time.Now().Add(2*time.Minute)) && len(existingDetails) > 0 {
			jsonOut(w, 200, map[string]any{"payment_attempt_id": existingID, "reference": existingRef, "method": method, "transfer": json.RawMessage(existingDetails), "state": "awaiting_payment"})
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
	letter := "t"
	if method == "card" {
		letter = "c"
	} else if method == "pay_with_bank" {
		letter = "b"
	}
	reference := fmt.Sprintf("wmt-%s-%s%d", strings.ReplaceAll(quote.ID, "-", ""), letter, previous+1)
	environment := environmentOf(a)
	attempt, err := queries.UpsertPaymentAttempt(r.Context(), store.UpsertPaymentAttemptParams{QuoteID: quote.ID, Environment: environment, Reference: reference, ExpectedMinor: amount, ApprovedFeeMinor: platformFee, FeeBasisPoints: int32(bps), Channel: method})
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
	req := checkoutRequest{Reference: reference, AmountMinor: amount, Currency: "NGN", Email: quote.BuyerEmail, Name: quote.BuyerName, WebhookURL: webhookURL(),
		Narration: "WantMyTime booking", QuoteID: quote.ID, Channels: []string{method},
		RedirectURL: origin + "/payment/return?quote_id=" + url.QueryEscape(quote.ID) + "&reference=" + url.QueryEscape(reference)}
	if method == "card" || method == "pay_with_bank" {
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
		jsonOut(w, 200, map[string]any{"payment_attempt_id": attempt.ID, "reference": reference, "method": method, "authorization_url": checkoutURL, "state": "awaiting_payment"})
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
	raw, _ := json.Marshal(details)
	if saved, saveErr := queries.SaveTransferInstructions(r.Context(), store.SaveTransferInstructionsParams{Details: raw, ExpiresAt: details.ExpiresAt, ID: attempt.ID}); saveErr != nil || saved != 1 {
		problem(w, 503, "PAYMENT_INITIALIZATION_UNKNOWN", "The transfer account could not be saved. Refresh and try again; do not transfer yet.")
		return
	}
	jsonOut(w, 200, map[string]any{"payment_attempt_id": attempt.ID, "reference": reference, "method": method, "transfer": details, "state": "awaiting_payment"})
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
