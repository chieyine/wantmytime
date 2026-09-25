package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// startGuestBooking lets a buyer go straight from picking a time to paying,
// without typing an email code first. It opens a booking-only session for the
// email given: that session can hold a time and pay for it, and see only what
// it booked. The email is not treated as confirmed; the booking emails go to
// it, and seeing the booking from another device still needs a code.
//
// Limits: per-address rate limits on the route, at most 10 such sessions per
// email per hour, and the usual cap of three unpaid holds per person.
func (a *API) startGuestBooking(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if decode(r, &in) != nil || !validEmail(in.Email) {
		problem(w, 422, "INVALID_EMAIL", "Enter a valid email address.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Booking could not be started.")
		return
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, email); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Booking could not be started.")
		return
	}
	var userID, status string
	err = tx.QueryRow(ctx, `SELECT u.id::text,u.status FROM user_identities i JOIN users u ON u.id=i.user_id WHERE i.type='email' AND i.normalized_identifier=$1`, email).Scan(&userID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.QueryRow(ctx, `INSERT INTO users(id,display_name) VALUES(gen_random_uuid(),split_part($1,'@',1)) RETURNING id::text`, email).Scan(&userID); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO user_identities(id,user_id,type,normalized_identifier,verified_at) VALUES(gen_random_uuid(),$1,'email',$2,NULL)`, userID, email)
		}
		status = "active"
	}
	if err != nil {
		problem(w, 503, "ACCOUNT_ERROR", "Booking could not be started.")
		return
	}
	if status != "active" {
		problem(w, 403, "ACCOUNT_RESTRICTED", "This email can’t be used to book. Contact support if you think that’s wrong.")
		return
	}
	// Someone already signed in as this person keeps their session.
	if current, currentErr := a.currentUser(r); currentErr == nil && current.ID == userID {
		if err = tx.Commit(ctx); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Booking could not be started.")
			return
		}
		jsonOut(w, 200, map[string]any{"started": true, "session": "existing"})
		return
	}
	var recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1 AND guest_scope->>'unverified'='true' AND created_at>now()-interval '1 hour'`, userID).Scan(&recent); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Booking could not be started.")
		return
	}
	if recent >= 10 {
		problem(w, 429, "RATE_LIMITED", "Too many bookings started with this email. Wait a little and try again.")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		problem(w, 500, "SESSION_ERROR", "Booking could not be started.")
		return
	}
	scope, _ := json.Marshal(map[string]any{"purpose": "guest_booking", "unverified": true, "quote_ids": []string{}, "offer_ids": []string{}, "booking_ids": []string{}})
	if _, err = tx.Exec(ctx, `INSERT INTO sessions(id,token_hash,user_id,guest_scope,expires_at) VALUES(gen_random_uuid(),$1,$2,$3,now()+interval '7 days')`, digest(token), userID, scope); err != nil {
		problem(w, 503, "SESSION_ERROR", "Booking could not be started.")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Booking could not be started.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "aside_session", Value: token, Path: "/", HttpOnly: true, Secure: a.env == "production", SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 60 * 60})
	jsonOut(w, 200, map[string]any{"started": true})
}
