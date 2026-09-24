package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	_ "image/jpeg"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *API) createOffer(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, guestScope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && guestScope.Purpose != "guest_offer" {
		problem(w, 403, "GUEST_SCOPE_DENIED", "This guest session cannot send an offer.")
		return
	}
	var in struct {
		Seller   string `json:"seller"`
		Name     string `json:"name"`
		Duration int    `json:"duration_minutes"`
		Amount   int64  `json:"amount_minor"`
	}
	if decode(r, &in) != nil {
		problem(w, 400, "INVALID_BODY", "Check the offer details.")
		return
	}
	in.Seller = normalizeHandle(in.Seller)
	in.Name = strings.TrimSpace(in.Name)
	if !validHandle(in.Seller) || !validName(in.Name) || (in.Duration != 15 && in.Duration != 30 && in.Duration != 60) || in.Amount < 100 || in.Amount > 100000000 {
		problem(w, 422, "INVALID_OFFER", "Choose a valid link, name, duration and offer amount.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 16 || len(key) > 128 {
		problem(w, 422, "IDEMPOTENCY_KEY_REQUIRED", "Refresh the page and try again.")
		return
	}
	canonical, _ := json.Marshal(in)
	bodyDigest := digest(string(canonical))
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your offer could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT id FROM users WHERE id=$1 FOR UPDATE`, u.ID); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your offer could not be saved.")
		return
	}
	var existing, state string
	var existingDigest []byte
	err = tx.QueryRow(r.Context(), `SELECT id::text,state,request_digest FROM offers WHERE buyer_user_id=$1 AND idempotency_key=$2`, u.ID, key).Scan(&existing, &state, &existingDigest)
	if err == nil {
		if subtle.ConstantTimeCompare(existingDigest, bodyDigest) != 1 {
			problem(w, 409, "IDEMPOTENCY_CONFLICT", "This request key was already used for different offer details.")
			return
		}
		if guest && !guestHas(guestScope, "offer_ids", existing) {
			if err = addGuestResource(r.Context(), tx, requestSessionHash(r), "offer_ids", existing); err != nil {
				problem(w, 503, "GUEST_SCOPE_ERROR", "Restricted offer access could not be saved.")
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Your offer could not be saved.")
			return
		}
		jsonOut(w, 200, map[string]any{"id": existing, "state": state})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "Your offer could not be saved.")
		return
	}
	var sellerID, sellerUserID string
	var mode string
	var paused bool
	err = tx.QueryRow(r.Context(), `SELECT id::text,user_id::text,mode,paused FROM seller_profiles WHERE handle=$1 AND publication_state='published' FOR UPDATE`, in.Seller).Scan(&sellerID, &sellerUserID, &mode, &paused)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This link is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your offer could not be saved.")
		return
	}
	if mode != "offer" {
		problem(w, 409, "OFFER_MODE_DISABLED", "This link is not accepting offers.")
		return
	}
	if paused {
		problem(w, 409, "SELLER_UNAVAILABLE", "This link is not accepting offers right now.")
		return
	}
	id, err := randomUUID()
	if err != nil {
		problem(w, 500, "OFFER_ERROR", "Your offer could not be created.")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO offers(id,seller_id,buyer_user_id,buyer_name,buyer_email,duration_minutes,state,version,expires_at,idempotency_key,request_digest) VALUES($1,$2,$3,$4,$5,$6,'pending',1,now()+interval '72 hours',$7,$8)`, id, sellerID, u.ID, in.Name, u.Email, in.Duration, key, bodyDigest)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your offer could not be saved.")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO offer_versions(id,offer_id,version,amount_minor,actor) VALUES(gen_random_uuid(),$1,1,$2,'buyer')`, id, in.Amount)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your offer could not be saved.")
		return
	}
	if err = enqueueOfferEvent(r.Context(), tx, id, sellerUserID, "offer_received_seller", id+":offer:received"); err != nil {
		problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "Your offer could not be saved.")
		return
	}
	if guest {
		if err = addGuestResource(r.Context(), tx, requestSessionHash(r), "offer_ids", id); err != nil {
			problem(w, 503, "GUEST_SCOPE_ERROR", "Restricted offer access could not be saved.")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your offer could not be saved.")
		return
	}
	jsonOut(w, 201, map[string]any{"id": id, "state": "pending", "version": 1, "expires_in_seconds": 259200, "notice": "Saved for the seller to review. No payment was taken."})
}

func (a *API) listOffers(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, e := a.db.Query(r.Context(), `SELECT o.id::text,sp.handle,o.buyer_name,o.duration_minutes,o.state,o.version,o.expires_at,latest.amount_minor::text,CASE WHEN sp.user_id=$1 THEN 'seller' ELSE 'buyer' END,o.created_at FROM offers o JOIN seller_profiles sp ON sp.id=o.seller_id JOIN LATERAL (SELECT amount_minor FROM offer_versions WHERE offer_id=o.id ORDER BY version DESC LIMIT 1) latest ON true WHERE (sp.user_id=$1 OR o.buyer_user_id=$1) AND ($2::timestamptz IS NULL OR (o.created_at,o.id)<($2,$3::uuid)) ORDER BY o.created_at DESC,o.id DESC LIMIT 100`, u.ID, cursorAt, cursorID)
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Offers could not be loaded.")
		return
	}
	defer rows.Close()
	type item struct {
		ID       string    `json:"id"`
		Seller   string    `json:"seller"`
		Buyer    string    `json:"buyer_name"`
		Duration int       `json:"duration_minutes"`
		State    string    `json:"state"`
		Version  int       `json:"version"`
		Expires  time.Time `json:"expires_at"`
		Amount   string    `json:"amount_minor"`
		Role     string    `json:"role"`
		Created  time.Time `json:"-"`
	}
	out := []item{}
	for rows.Next() {
		var x item
		if rows.Scan(&x.ID, &x.Seller, &x.Buyer, &x.Duration, &x.State, &x.Version, &x.Expires, &x.Amount, &x.Role, &x.Created) != nil {
			problem(w, 503, "DATABASE_ERROR", "Offers could not be loaded.")
			return
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Offers could not be loaded.")
		return
	}
	var nextCursor string
	if len(out) == operationsPageSize {
		nextCursor = encodeListCursor(out[len(out)-1].Created, out[len(out)-1].ID)
	}
	jsonOut(w, 200, map[string]any{"offers": out, "next_cursor": nextCursor})
}

func (a *API) getOffer(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if guest && !guestHas(scope, "offer_ids", id) {
		problem(w, 404, "NOT_FOUND", "This offer is not available.")
		return
	}
	var out struct {
		ID               string     `json:"id"`
		Seller           string     `json:"seller"`
		Buyer            string     `json:"buyer_name"`
		Duration         int        `json:"duration_minutes"`
		State            string     `json:"state"`
		Version          int        `json:"version"`
		Amount           string     `json:"amount_minor"`
		Expires          time.Time  `json:"expires_at"`
		CheckoutExpires  *time.Time `json:"checkout_expires_at"`
		Role             string     `json:"role"`
		LocalSimulator   bool       `json:"local_simulator"`
		ProviderCheckout bool       `json:"provider_checkout_enabled"`
		Timezone         string     `json:"timezone"`
	}
	err := a.db.QueryRow(r.Context(), `SELECT o.id::text,sp.handle,o.buyer_name,o.duration_minutes,o.state,o.version,latest.amount_minor::text,o.expires_at,o.checkout_expires_at,CASE WHEN sp.user_id=$2 THEN 'seller' ELSE 'buyer' END,sp.timezone FROM offers o JOIN seller_profiles sp ON sp.id=o.seller_id JOIN LATERAL (SELECT amount_minor FROM offer_versions WHERE offer_id=o.id ORDER BY version DESC LIMIT 1) latest ON true WHERE o.id=$1 AND (sp.user_id=$2 OR o.buyer_user_id=$2)`, id, u.ID).Scan(&out.ID, &out.Seller, &out.Buyer, &out.Duration, &out.State, &out.Version, &out.Amount, &out.Expires, &out.CheckoutExpires, &out.Role, &out.Timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This offer is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The offer could not be loaded.")
		return
	}
	if out.State == "agreed" && out.CheckoutExpires != nil && !out.CheckoutExpires.After(time.Now()) {
		_, _ = a.db.Exec(r.Context(), `UPDATE offers SET state='expired' WHERE id=$1 AND state='agreed' AND checkout_expires_at<=now()`, id)
		out.State = "expired"
	}
	out.LocalSimulator = a.localPaymentSimulatorEnabled()
	out.ProviderCheckout = a.providerCheckoutConfigured()
	jsonOut(w, 200, out)
}

func (a *API) offerRespond(state string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok, guest, scope := a.buyerActor(w, r)
		if !ok {
			return
		}
		if guest && !guestHas(scope, "offer_ids", r.PathValue("id")) {
			problem(w, 404, "NOT_FOUND", "This offer is not available.")
			return
		}
		var in struct {
			Version int   `json:"version"`
			Amount  int64 `json:"amount_minor"`
		}
		if state == "counter" || state == "accept" || state == "decline" || state == "withdraw" {
			if decode(r, &in) != nil {
				problem(w, 400, "INVALID_BODY", "Refresh the offer and try again.")
				return
			}
		}
		tx, err := a.db.Begin(r.Context())
		if err != nil {
			problem(w, 503, "DATABASE_ERROR", "The offer could not be updated.")
			return
		}
		defer tx.Rollback(r.Context())
		var sellerUser, buyerUser, offerState string
		var version int
		var expires time.Time
		err = tx.QueryRow(r.Context(), `SELECT sp.user_id::text,o.buyer_user_id::text,o.state,o.version,o.expires_at FROM offers o JOIN seller_profiles sp ON sp.id=o.seller_id WHERE o.id=$1 AND (sp.user_id=$2 OR o.buyer_user_id=$2) FOR UPDATE OF o`, r.PathValue("id"), u.ID).Scan(&sellerUser, &buyerUser, &offerState, &version, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			problem(w, 404, "NOT_FOUND", "This offer is not available.")
			return
		}
		if err != nil {
			problem(w, 503, "DATABASE_ERROR", "The offer could not be updated.")
			return
		}
		if offerState == "pending" || offerState == "countered" {
			if !expires.After(time.Now()) {
				_, _ = tx.Exec(r.Context(), `UPDATE offers SET state='expired' WHERE id=$1`, r.PathValue("id"))
				_ = tx.Commit(r.Context())
				problem(w, 409, "OFFER_EXPIRED", "This offer has expired.")
				return
			}
		}
		if in.Version != version {
			problem(w, 409, "STALE_OFFER", "This offer changed. Refresh it before responding.")
			return
		}
		isSeller := sellerUser == u.ID
		newState := ""
		switch state {
		case "counter":
			if !isSeller || offerState != "pending" || version != 1 {
				problem(w, 409, "COUNTER_UNAVAILABLE", "Only one seller counteroffer is allowed.")
				return
			}
			if in.Amount < 100 || in.Amount > 100000000 {
				problem(w, 422, "INVALID_AMOUNT", "Enter a supported offer amount.")
				return
			}
			if _, err = tx.Exec(r.Context(), `INSERT INTO offer_versions(id,offer_id,version,amount_minor,actor) VALUES(gen_random_uuid(),$1,2,$2,'seller')`, r.PathValue("id"), in.Amount); err != nil {
				problem(w, 503, "DATABASE_ERROR", "The counteroffer could not be saved.")
				return
			}
			newState = "countered"
			version = 2
		case "accept":
			if (isSeller && offerState != "pending") || (!isSeller && offerState != "countered") {
				problem(w, 409, "OFFER_ACTION_UNAVAILABLE", "This offer cannot be accepted in its current state.")
				return
			}
			newState = "agreed"
		case "decline":
			if (isSeller && offerState != "pending") || (!isSeller && offerState != "countered") {
				problem(w, 409, "OFFER_ACTION_UNAVAILABLE", "This offer cannot be declined in its current state.")
				return
			}
			newState = "declined"
		case "withdraw":
			if (isSeller && offerState != "countered") || (!isSeller && (offerState != "pending" && offerState != "countered")) {
				problem(w, 409, "OFFER_ACTION_UNAVAILABLE", "This offer cannot be withdrawn in its current state.")
				return
			}
			newState = "withdrawn"
		default:
			problem(w, 404, "NOT_FOUND", "This offer action is not available.")
			return
		}
		if newState == "agreed" {
			_, err = tx.Exec(r.Context(), `UPDATE offers SET state='agreed',checkout_expires_at=now()+interval '24 hours' WHERE id=$1`, r.PathValue("id"))
		} else {
			_, err = tx.Exec(r.Context(), `UPDATE offers SET state=$2,version=$3 WHERE id=$1`, r.PathValue("id"), newState, version)
		}
		if err != nil {
			problem(w, 503, "DATABASE_ERROR", "The offer could not be updated.")
			return
		}
		// The other participant hears about every change by email.
		recipient, audience := buyerUser, "buyer"
		if !isSeller {
			recipient, audience = sellerUser, "seller"
		}
		event := map[string]string{"countered": "countered", "agreed": "accepted", "declined": "declined", "withdrawn": "withdrawn"}[newState]
		if err = enqueueOfferEvent(r.Context(), tx, r.PathValue("id"), recipient, "offer_"+event+"_"+audience, fmt.Sprintf("%s:offer:%s:%d", r.PathValue("id"), newState, version)); err != nil {
			problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The offer could not be updated.")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The offer could not be updated.")
			return
		}
		jsonOut(w, 200, map[string]any{"id": r.PathValue("id"), "state": newState, "version": version, "message": "Offer status saved. Agreement does not collect payment."})
	}
}
