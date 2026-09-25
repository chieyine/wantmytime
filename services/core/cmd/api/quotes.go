package main

import (
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	_ "image/jpeg"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

func (a *API) localPaymentSimulatorEnabled() bool {
	return a.env == "local" && os.Getenv("LOCAL_PAYMENT_SIMULATOR") == "true"
}

func (a *API) createBooking(w http.ResponseWriter, r *http.Request) {
	problem(w, 503, "PAYMENTS_DISABLED", "Bookings require an approved payment provider. No personal details were stored.")
}

type quoteRequest struct {
	Seller   string    `json:"seller"`
	Name     string    `json:"name"`
	Duration int       `json:"duration_minutes"`
	StartsAt time.Time `json:"starts_at"`
}

func (a *API) createQuote(w http.ResponseWriter, r *http.Request) {
	if !a.localPaymentSimulatorEnabled() && !a.providerCheckoutConfigured() {
		problem(w, 503, "PAYMENTS_DISABLED", "Booking checkout is unavailable until provider and commercial approval are complete.")
		return
	}
	u, ok, guest, guestScope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && guestScope.Purpose != "guest_booking" {
		problem(w, 403, "GUEST_SCOPE_DENIED", "This guest session cannot start a booking.")
		return
	}
	var in quoteRequest
	if decode(r, &in) != nil {
		problem(w, 400, "INVALID_BODY", "Check the booking details.")
		return
	}
	in.Seller = normalizeHandle(in.Seller)
	in.Name = strings.TrimSpace(in.Name)
	if !validHandle(in.Seller) || !validName(in.Name) || (in.Duration != 15 && in.Duration != 30 && in.Duration != 60) || in.StartsAt.IsZero() {
		problem(w, 422, "INVALID_BOOKING", "Choose a valid link, name, conversation length and time.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 16 || len(key) > 128 {
		problem(w, 422, "IDEMPOTENCY_KEY_REQUIRED", "Refresh the page and try again.")
		return
	}
	canonical, _ := json.Marshal(in)
	bodyDigest := digest(string(canonical))
	// Make sure the seller's Google Calendar busy times are recent before
	// holding a time (bounded wait; the cached copy decides on failure).
	if sellerID, lookupErr := store.New(a.db).SellerIDForHandle(r.Context(), in.Seller); lookupErr == nil {
		a.refreshBusyIfStale(r.Context(), sellerID)
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "A time could not be held.")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT id FROM users WHERE id=$1 FOR UPDATE`, u.ID); err != nil {
		problem(w, 503, "DATABASE_ERROR", "A time could not be held.")
		return
	}
	var existingID, state string
	var existingDigest []byte
	err = tx.QueryRow(r.Context(), `SELECT id::text,state,request_digest FROM quotes WHERE buyer_user_id=$1 AND idempotency_key=$2`, u.ID, key).Scan(&existingID, &state, &existingDigest)
	if err == nil {
		if subtle.ConstantTimeCompare(existingDigest, bodyDigest) != 1 {
			problem(w, 409, "IDEMPOTENCY_CONFLICT", "This request key was already used for different booking details.")
			return
		}
		var amount int64
		var expires time.Time
		if err = tx.QueryRow(r.Context(), `SELECT gross_minor,expires_at FROM quotes WHERE id=$1`, existingID).Scan(&amount, &expires); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The booking quote could not be loaded.")
			return
		}
		if guest && !guestHas(guestScope, "quote_ids", existingID) {
			if err = addGuestResource(r.Context(), tx, requestSessionHash(r), "quote_ids", existingID); err != nil {
				problem(w, 503, "GUEST_SCOPE_ERROR", "The restricted booking access could not be saved.")
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The booking quote could not be loaded.")
			return
		}
		jsonOut(w, 200, map[string]any{"id": existingID, "state": state, "gross_minor": strconv.FormatInt(amount, 10), "expires_at": expires})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "A time could not be held.")
		return
	}
	var sellerID, zone, mode string
	var paused, ready bool
	var notice, horizon, buffer int
	err = tx.QueryRow(r.Context(), `SELECT id::text,timezone,paused,(readiness_state='ready'),minimum_notice_minutes,booking_horizon_days,buffer_minutes,mode FROM seller_profiles WHERE handle=$1 AND publication_state='published' FOR UPDATE`, in.Seller).Scan(&sellerID, &zone, &paused, &ready, &notice, &horizon, &buffer, &mode)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This link is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "A time could not be held.")
		return
	}
	if paused || !ready {
		problem(w, 409, "SELLER_UNAVAILABLE", "This link is not ready to take bookings.")
		return
	}
	if mode != "fixed" {
		problem(w, 409, "OFFER_MODE_ONLY", "This link takes offers instead of fixed-price bookings. Send an offer to continue.")
		return
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		problem(w, 503, "INVALID_SELLER_TIMEZONE", "Availability is not configured correctly.")
		return
	}
	starts := in.StartsAt.UTC()
	minStart := time.Now().Add(time.Duration(notice) * time.Minute)
	maxStart := time.Now().AddDate(0, 0, horizon)
	if starts.Before(minStart) || starts.After(maxStart) {
		problem(w, 409, "SLOT_UNAVAILABLE", "That time is outside the current booking window. Choose another time.")
		return
	}
	var pricingID string
	var base int64
	var durations []int
	err = tx.QueryRow(r.Context(), `SELECT id::text,base_30_minor,durations FROM pricing_versions WHERE seller_id=$1 ORDER BY created_at DESC LIMIT 1`, sellerID).Scan(&pricingID, &base, &durations)
	if err != nil || !hasDuration(durations, in.Duration) || base <= 0 {
		problem(w, 409, "PRICE_UNAVAILABLE", "That conversation length is no longer available.")
		return
	}
	day := starts.In(location)
	if !validScheduledTime(r.Context(), tx, sellerID, zone, day, starts, in.Duration, buffer, nil) {
		problem(w, 409, "SLOT_UNAVAILABLE", "That time is no longer available. Choose another time.")
		return
	}
	gross := (base*int64(in.Duration) + 15) / 30
	if withinLimit, limitErr := claimBuyerHoldCapacity(r.Context(), tx, u.ID, sellerID); limitErr != nil {
		problem(w, 503, "DATABASE_ERROR", "A time could not be held.")
		return
	} else if !withinLimit {
		problem(w, 429, "TOO_MANY_HOLDS", "You are already holding several times. Finish or let one of them expire first.")
		return
	}
	quoteID, err := randomUUID()
	if err != nil {
		problem(w, 500, "QUOTE_ERROR", "A booking quote could not be created.")
		return
	}
	expires := time.Now().Add(10 * time.Minute)
	occupiedTo := starts.Add(time.Duration(in.Duration+buffer) * time.Minute)
	if _, err = tx.Exec(r.Context(), `UPDATE quotes SET state='expired' WHERE seller_id=$1 AND state='held' AND expires_at<=now()`, sellerID); err != nil {
		problem(w, 503, "DATABASE_ERROR", "A time could not be held.")
		return
	}
	if err = releaseExpiredHolds(r.Context(), tx, sellerID, starts, occupiedTo); err != nil {
		problem(w, 503, "DATABASE_ERROR", "A time could not be held.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO quotes(id,seller_id,buyer_user_id,pricing_version_id,buyer_name,duration_minutes,starts_at,gross_minor,state,expires_at,idempotency_key,request_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'held',$9,$10,$11)`, quoteID, sellerID, u.ID, pricingID, in.Name, in.Duration, starts, gross, expires, key, bodyDigest); err != nil {
		problem(w, 503, "DATABASE_ERROR", "A booking quote could not be saved.")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO slot_reservations(id,seller_id,occupied_from,occupied_to,active,reservation_kind,quote_id,expires_at) VALUES(gen_random_uuid(),$1,$2,$3,true,'hold',$4,$5)`, sellerID, starts, occupiedTo, quoteID, expires)
	if pgErrCode(err) == "23P01" {
		problem(w, 409, "SLOT_UNAVAILABLE", "That time was just taken. Choose another time.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "A booking hold could not be saved.")
		return
	}
	if guest {
		if err = addGuestResource(r.Context(), tx, requestSessionHash(r), "quote_ids", quoteID); err != nil {
			problem(w, 503, "GUEST_SCOPE_ERROR", "Restricted booking access could not be saved.")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "A booking hold could not be saved.")
		return
	}
	jsonOut(w, 201, map[string]any{"id": quoteID, "state": "held", "gross_minor": strconv.FormatInt(gross, 10), "currency": "NGN", "duration_minutes": in.Duration, "starts_at": starts, "expires_at": expires, "local_simulator": a.localPaymentSimulatorEnabled()})
}

func (a *API) createOfferQuote(w http.ResponseWriter, r *http.Request) {
	if !a.localPaymentSimulatorEnabled() && !a.providerCheckoutConfigured() {
		problem(w, 503, "PAYMENTS_DISABLED", "Offer checkout is unavailable until provider and commercial approval are complete.")
		return
	}
	u, ok, guest, guestScope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && (guestScope.Purpose != "guest_offer" || !guestHas(guestScope, "offer_ids", r.PathValue("id"))) {
		problem(w, 404, "NOT_FOUND", "This offer is not available.")
		return
	}
	var in struct {
		StartsAt time.Time `json:"starts_at"`
	}
	if decode(r, &in) != nil || in.StartsAt.IsZero() {
		problem(w, 422, "INVALID_SLOT", "Choose an available time.")
		return
	}
	id := r.PathValue("id")
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 16 || len(key) > 128 {
		problem(w, 422, "IDEMPOTENCY_KEY_REQUIRED", "Refresh the page and try again.")
		return
	}
	canonical, _ := json.Marshal(map[string]any{"offer_id": id, "starts_at": in.StartsAt.UTC()})
	requestDigest := digest(string(canonical))
	var offerSellerID string
	if a.db.QueryRow(r.Context(), `SELECT seller_id::text FROM offers WHERE id=$1 AND buyer_user_id=$2`, id, u.ID).Scan(&offerSellerID) == nil {
		a.refreshBusyIfStale(r.Context(), offerSellerID)
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be started.")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT id FROM users WHERE id=$1 FOR UPDATE`, u.ID); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be started.")
		return
	}
	var existing string
	var existingDigest []byte
	var state string
	err = tx.QueryRow(r.Context(), `SELECT id::text,state,request_digest FROM quotes WHERE buyer_user_id=$1 AND idempotency_key=$2`, u.ID, key).Scan(&existing, &state, &existingDigest)
	if err == nil {
		if subtle.ConstantTimeCompare(existingDigest, requestDigest) != 1 {
			problem(w, 409, "IDEMPOTENCY_CONFLICT", "This request key was already used for different checkout details.")
			return
		}
		var amount int64
		var expires time.Time
		if err = tx.QueryRow(r.Context(), `SELECT gross_minor,expires_at FROM quotes WHERE id=$1`, existing).Scan(&amount, &expires); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The checkout quote could not be loaded.")
			return
		}
		if guest && !guestHas(guestScope, "quote_ids", existing) {
			if err = addGuestResource(r.Context(), tx, requestSessionHash(r), "quote_ids", existing); err != nil {
				problem(w, 503, "GUEST_SCOPE_ERROR", "Restricted checkout access could not be saved.")
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Checkout could not be started.")
			return
		}
		jsonOut(w, 200, map[string]any{"id": existing, "state": state, "gross_minor": strconv.FormatInt(amount, 10), "expires_at": expires})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be started.")
		return
	}
	var sellerID, zone, pricingID, sellerHandle string
	var stateOffer string
	var paused, ready bool
	var notice, horizon, buffer, duration int
	var expiresAgreement *time.Time
	var amount int64
	err = tx.QueryRow(r.Context(), `SELECT o.seller_id::text,sp.handle,sp.timezone,sp.paused,(sp.readiness_state='ready'),sp.minimum_notice_minutes,sp.booking_horizon_days,sp.buffer_minutes,o.duration_minutes,o.state,o.checkout_expires_at,latest.amount_minor FROM offers o JOIN seller_profiles sp ON sp.id=o.seller_id JOIN LATERAL (SELECT amount_minor FROM offer_versions WHERE offer_id=o.id ORDER BY version DESC LIMIT 1) latest ON true WHERE o.id=$1 AND o.buyer_user_id=$2 FOR UPDATE OF o`, id, u.ID).Scan(&sellerID, &sellerHandle, &zone, &paused, &ready, &notice, &horizon, &buffer, &duration, &stateOffer, &expiresAgreement, &amount)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This offer is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be started.")
		return
	}
	if stateOffer == "agreed" && (expiresAgreement == nil || !expiresAgreement.After(time.Now())) {
		_, _ = tx.Exec(r.Context(), `UPDATE offers SET state='expired' WHERE id=$1`, id)
		_ = tx.Commit(r.Context())
		problem(w, 409, "OFFER_EXPIRED", "The agreement checkout window has expired.")
		return
	}
	if stateOffer != "agreed" || paused || !ready {
		problem(w, 409, "OFFER_NOT_READY", "This offer is not ready for checkout.")
		return
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		problem(w, 503, "INVALID_SELLER_TIMEZONE", "Availability is not configured correctly.")
		return
	}
	starts := in.StartsAt.UTC()
	now := time.Now()
	if starts.Before(now.Add(time.Duration(notice)*time.Minute)) || starts.After(now.AddDate(0, 0, horizon)) {
		problem(w, 409, "SLOT_UNAVAILABLE", "That time is outside the current booking window.")
		return
	}
	if !validScheduledTime(r.Context(), tx, sellerID, zone, starts.In(location), starts, duration, buffer, nil) {
		problem(w, 409, "SLOT_UNAVAILABLE", "That time is no longer available. Choose another time.")
		return
	}
	var active bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM quotes q JOIN slot_reservations sr ON sr.quote_id=q.id WHERE q.offer_id=$1 AND q.state='held' AND q.expires_at>now() AND sr.active)`, id).Scan(&active)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be started.")
		return
	}
	if active {
		problem(w, 409, "CHECKOUT_ALREADY_ACTIVE", "This offer already has an active time hold.")
		return
	}
	err = tx.QueryRow(r.Context(), `SELECT id::text FROM pricing_versions WHERE seller_id=$1 ORDER BY created_at DESC LIMIT 1`, sellerID).Scan(&pricingID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Pricing could not be loaded.")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE quotes SET state='expired' WHERE offer_id=$1 AND state='held' AND expires_at<=now()`, id); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be started.")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE slot_reservations sr SET active=false FROM quotes q WHERE sr.quote_id=q.id AND q.offer_id=$1 AND q.state='expired' AND sr.active AND sr.reservation_kind='hold'`, id); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be started.")
		return
	}
	quoteID, err := randomUUID()
	if err != nil {
		problem(w, 500, "QUOTE_ERROR", "Checkout could not be started.")
		return
	}
	expires := time.Now().Add(10 * time.Minute)
	occupiedTo := starts.Add(time.Duration(duration+buffer) * time.Minute)
	if err = releaseExpiredHolds(r.Context(), tx, sellerID, starts, occupiedTo); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be started.")
		return
	}
	if withinLimit, limitErr := claimBuyerHoldCapacity(r.Context(), tx, u.ID, sellerID); limitErr != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout could not be started.")
		return
	} else if !withinLimit {
		problem(w, 429, "TOO_MANY_HOLDS", "You are already holding several times. Finish or let one of them expire first.")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO quotes(id,seller_id,buyer_user_id,pricing_version_id,offer_id,buyer_name,duration_minutes,starts_at,gross_minor,state,expires_at,idempotency_key,request_digest) VALUES($1,$2,$3,$4,$5,(SELECT buyer_name FROM offers WHERE id=$5),$6,$7,$8,'held',$9,$10,$11)`, quoteID, sellerID, u.ID, pricingID, id, duration, starts, amount, expires, key, requestDigest)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Checkout quote could not be saved.")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO slot_reservations(id,seller_id,occupied_from,occupied_to,active,reservation_kind,quote_id,expires_at) VALUES(gen_random_uuid(),$1,$2,$3,true,'hold',$4,$5)`, sellerID, starts, occupiedTo, quoteID, expires)
	if pgErrCode(err) == "23P01" {
		problem(w, 409, "SLOT_UNAVAILABLE", "That time was just taken. Choose another time.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The time hold could not be saved.")
		return
	}
	// The agreement must stay open at least as long as the hold it created.
	if _, err = tx.Exec(r.Context(), `UPDATE offers SET checkout_expires_at=GREATEST(checkout_expires_at,$2) WHERE id=$1 AND state='agreed'`, id, expires); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The time hold could not be saved.")
		return
	}
	if guest {
		if err = addGuestResource(r.Context(), tx, requestSessionHash(r), "quote_ids", quoteID); err != nil {
			problem(w, 503, "GUEST_SCOPE_ERROR", "Restricted checkout access could not be saved.")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The time hold could not be saved.")
		return
	}
	jsonOut(w, 201, map[string]any{"id": quoteID, "state": "held", "gross_minor": strconv.FormatInt(amount, 10), "currency": "NGN", "duration_minutes": duration, "starts_at": starts, "expires_at": expires, "seller": sellerHandle, "local_simulator": a.localPaymentSimulatorEnabled()})
}

func (a *API) getQuote(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if guest && !guestHas(scope, "quote_ids", id) {
		problem(w, 404, "NOT_FOUND", "This quote is not available.")
		return
	}
	if _, err := hex.DecodeString(strings.ReplaceAll(id, "-", "")); err != nil || len(id) != 36 {
		problem(w, 404, "NOT_FOUND", "This quote is not available.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The quote could not be loaded.")
		return
	}
	defer tx.Rollback(r.Context())
	var state string
	var expires time.Time
	var amount int64
	var duration int
	var starts time.Time
	var handle, sellerName string
	var bookingID, bookingPaymentState *string
	err = tx.QueryRow(r.Context(), `SELECT q.state,q.expires_at,q.gross_minor,q.duration_minutes,q.starts_at,sp.handle,su.display_name FROM quotes q JOIN seller_profiles sp ON sp.id=q.seller_id JOIN users su ON su.id=sp.user_id WHERE q.id=$1 AND q.buyer_user_id=$2 FOR UPDATE OF q`, id, u.ID).Scan(&state, &expires, &amount, &duration, &starts, &handle, &sellerName)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This quote is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The quote could not be loaded.")
		return
	}
	if state == "held" && !expires.After(time.Now()) {
		if _, err = tx.Exec(r.Context(), `UPDATE quotes SET state='expired' WHERE id=$1 AND state='held'`, id); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The quote could not be loaded.")
			return
		}
		if _, err = tx.Exec(r.Context(), `UPDATE slot_reservations SET active=false WHERE quote_id=$1 AND active AND reservation_kind='hold'`, id); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The quote could not be loaded.")
			return
		}
		state = "expired"
	}
	var policyKey string
	if err = tx.QueryRow(r.Context(), `SELECT sp.cancellation_policy FROM quotes q JOIN seller_profiles sp ON sp.id=q.seller_id WHERE q.id=$1`, id).Scan(&policyKey); err != nil {
		policyKey = "flexible"
	}
	err = tx.QueryRow(r.Context(), `SELECT id::text,payment_state FROM bookings WHERE quote_id=$1`, id).Scan(&bookingID, &bookingPaymentState)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "The quote could not be loaded.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The quote could not be loaded.")
		return
	}
	jsonOut(w, 200, map[string]any{"id": id, "state": state, "gross_minor": strconv.FormatInt(amount, 10), "duration_minutes": duration, "starts_at": starts, "expires_at": expires, "seller": handle, "seller_name": sellerName, "transfer_fee_minor": a.transferFeeEstimate(r.Context(), amount), "booking_id": bookingID, "booking_payment_state": bookingPaymentState, "local_simulator": a.localPaymentSimulatorEnabled(), "provider_checkout_enabled": a.providerCheckoutConfigured(), "international_cards": internationalCardsEnabled(), "cancellation_policy": policyOrDefault(policyKey), "payment_methods": paymentMethods(), "problem_window_minutes": int(disputeWindow() / time.Minute)})
}

// paymentMethods lists how the buyer may pay, bank transfer first.
func paymentMethods() []string {
	methods, err := approvedChannels()
	if err != nil {
		return []string{}
	}
	return methods
}

func (a *API) simulatePayment(w http.ResponseWriter, r *http.Request) {
	if !a.localPaymentSimulatorEnabled() {
		problem(w, 404, "NOT_FOUND", "This route is not available.")
		return
	}
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if guest && !guestHas(scope, "quote_ids", id) {
		problem(w, 404, "NOT_FOUND", "This quote is not available.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The local checkout could not be completed.")
		return
	}
	defer tx.Rollback(r.Context())
	var sellerID string
	var amount int64
	var duration int
	var starts, expires time.Time
	var name string
	var state string
	var offerID *string
	err = tx.QueryRow(r.Context(), `SELECT seller_id::text,gross_minor,duration_minutes,starts_at,expires_at,buyer_name,state,offer_id::text FROM quotes WHERE id=$1 AND buyer_user_id=$2 FOR UPDATE`, id, u.ID).Scan(&sellerID, &amount, &duration, &starts, &expires, &name, &state, &offerID)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This quote is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The local checkout could not be completed.")
		return
	}
	if state == "converted" {
		var bookingID string
		if err = tx.QueryRow(r.Context(), `SELECT id::text FROM bookings WHERE quote_id=$1`, id).Scan(&bookingID); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The confirmed booking could not be loaded.")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The confirmed booking could not be loaded.")
			return
		}
		jsonOut(w, 200, map[string]any{"booking_id": bookingID, "simulated": true})
		return
	}
	if state != "held" || !expires.After(time.Now()) {
		_, _ = tx.Exec(r.Context(), `UPDATE quotes SET state='expired' WHERE id=$1`, id)
		_, _ = tx.Exec(r.Context(), `UPDATE slot_reservations SET active=false WHERE quote_id=$1 AND active`, id)
		_ = tx.Commit(r.Context())
		problem(w, 409, "QUOTE_EXPIRED", "This time hold expired. Choose another time.")
		return
	}
	var sellerAvailable bool
	if err = tx.QueryRow(r.Context(), `SELECT u.status='active' AND NOT sp.paused FROM seller_profiles sp JOIN users u ON u.id=sp.user_id WHERE sp.id=$1`, sellerID).Scan(&sellerAvailable); err != nil || !sellerAvailable {
		_, _ = tx.Exec(r.Context(), `UPDATE quotes SET state='expired' WHERE id=$1`, id)
		_, _ = tx.Exec(r.Context(), `UPDATE slot_reservations SET active=false WHERE quote_id=$1 AND active`, id)
		_ = tx.Commit(r.Context())
		problem(w, 409, "SELLER_UNAVAILABLE", "This link is not available now. Choose another time later.")
		return
	}
	if offerID != nil {
		// The hold (checked above) was created inside the agreement window, so an
		// agreement that lapsed while the hold is still valid may still convert.
		var offerState string
		if err = tx.QueryRow(r.Context(), `SELECT state FROM offers WHERE id=$1 FOR UPDATE`, *offerID).Scan(&offerState); err != nil || (offerState != "agreed" && offerState != "expired") {
			problem(w, 409, "OFFER_EXPIRED", "This agreed offer is no longer available for checkout.")
			return
		}
	}
	bookingID, err := randomUUID()
	if err != nil {
		problem(w, 500, "BOOKING_ERROR", "The booking could not be confirmed.")
		return
	}
	var email string
	err = tx.QueryRow(r.Context(), `SELECT normalized_identifier FROM user_identities WHERE user_id=$1 AND type='email' ORDER BY verified_at DESC NULLS LAST LIMIT 1`, u.ID).Scan(&email)
	if err != nil {
		problem(w, 503, "IDENTITY_ERROR", "The verified email could not be loaded.")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO bookings(id,quote_id,seller_id,buyer_user_id,buyer_name,guest_email,duration_minutes,starts_at,gross_minor,currency,state,payment_state,quote_snapshot,meeting_deadline) SELECT $1,q.id,q.seller_id,q.buyer_user_id,q.buyer_name,$2,q.duration_minutes,q.starts_at,q.gross_minor,q.currency,'confirmed','simulated',jsonb_build_object('quote_id',q.id,'offer_id',q.offer_id,'price_minor',q.gross_minor,'currency',q.currency,'duration_minutes',q.duration_minutes,'starts_at',q.starts_at,'payment_mode','local_simulator'),q.starts_at-interval '30 minutes' FROM quotes q WHERE q.id=$3`, bookingID, email, id)
	if err != nil {
		problem(w, 503, "BOOKING_ERROR", "The booking could not be confirmed.")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO payment_attempts(id,booking_id,provider,environment,merchant_reference,expected_minor,currency,canonical_state) VALUES(gen_random_uuid(),$1,'local_simulator','local',$2,$3,'NGN','simulated')`, bookingID, "sim_"+id, amount)
	if err != nil {
		problem(w, 503, "BOOKING_ERROR", "The local payment record could not be saved.")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE quotes SET state='converted' WHERE id=$1`, id)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The booking could not be confirmed.")
		return
	}
	if offerID != nil {
		command, updateErr := tx.Exec(r.Context(), `UPDATE offers SET state='converted',converted_booking_id=$2 WHERE id=$1 AND state IN ('agreed','expired')`, *offerID, bookingID)
		if updateErr != nil || command.RowsAffected() != 1 {
			problem(w, 409, "OFFER_CONVERSION_CONFLICT", "This offer could not be converted. Contact support.")
			return
		}
	}
	_, err = tx.Exec(r.Context(), `UPDATE slot_reservations SET reservation_kind='booking',expires_at=NULL,booking_id=$2 WHERE quote_id=$1 AND active`, id, bookingID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The booking hold could not be confirmed.")
		return
	}
	if err = enqueueBookingNotifications(r.Context(), tx, bookingID); err != nil {
		problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The booking confirmation could not be queued.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The booking could not be confirmed.")
		return
	}
	jsonOut(w, 201, map[string]any{"booking_id": bookingID, "simulated": true, "notice": "Development-only payment simulation. No money was collected."})
}
