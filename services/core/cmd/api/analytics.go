package main

import (
	"encoding/hex"
	"encoding/json"
	_ "image/jpeg"
	"net/http"
	"strings"
)

func (a *API) recordProductEvent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Event     string `json:"event"`
		Handle    string `json:"handle"`
		BookingID string `json:"booking_id"`
	}
	if decode(r, &in) != nil {
		problem(w, 400, "INVALID_BODY", "The event could not be recorded.")
		return
	}
	allowed := map[string]bool{"public_link_viewed": true, "booking_started": true, "duration_selected": true, "slot_selected": true, "seller_cta_viewed": true, "seller_cta_clicked": true, "link_copy_clicked": true, "share_action_opened": true}
	if !allowed[in.Event] {
		problem(w, 422, "INVALID_EVENT", "This product event is not available.")
		return
	}
	var sellerID any
	if in.Handle != "" {
		_ = a.db.QueryRow(r.Context(), `SELECT id FROM seller_profiles WHERE handle=$1 AND publication_state='published'`, normalizeHandle(in.Handle)).Scan(&sellerID)
	}
	var subject []byte
	if u, err := a.currentUser(r); err == nil {
		subject = analyticsSubjectHash(u.ID)
	}
	if in.Event == "seller_cta_clicked" {
		// Buyers normally hold a guest booking session, so accept either kind.
		u, err := a.anySessionUser(r)
		compactBookingID := strings.ReplaceAll(in.BookingID, "-", "")
		if err != nil || sellerID == nil || len(in.BookingID) != 36 || len(compactBookingID) != 32 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if _, err = hex.DecodeString(compactBookingID); err != nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var eligible bool
		if err = a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM bookings b JOIN payment_allocations pa ON pa.booking_id=b.id WHERE b.id=$1 AND b.buyer_user_id=$2 AND b.seller_id=$3 AND b.payment_state='paid')`, in.BookingID, u.ID, sellerID).Scan(&eligible); err != nil {
			problem(w, 503, "EVENT_UNAVAILABLE", "This optional product event could not be recorded.")
			return
		}
		if !eligible {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		subject = analyticsSubjectHash(u.ID)
	}
	props := []byte(`{}`)
	if in.Event == "seller_cta_clicked" {
		props, _ = json.Marshal(map[string]string{"booking_id": in.BookingID})
	}
	_, err := a.db.Exec(r.Context(), `INSERT INTO product_events(id,event_name,subject_hash,environment,props,occurred_at,seller_id) VALUES(gen_random_uuid(),$1,$2,$3,$4::jsonb,now(),$5)`, in.Event, nullBytes(subject), envOr("APP_ENV", "local"), props, sellerID)
	if err != nil {
		problem(w, 503, "EVENT_UNAVAILABLE", "This optional product event could not be recorded.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
