package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const reviewWindow = 60 * 24 * time.Hour // after the end of the session

// reviewerName shows only a first name publicly.
func reviewerName(full string) string {
	fields := strings.Fields(full)
	if len(fields) == 0 {
		return "A buyer"
	}
	name := fields[0]
	if utf8.RuneCountInString(name) > 30 {
		name = string([]rune(name)[:30])
	}
	return name
}

// createReview records the buyer's rating after a session that took place.
func (a *API) createReview(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && !a.guestCanBooking(r.Context(), scope, r.PathValue("id")) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	var in struct {
		Rating int    `json:"rating"`
		Body   string `json:"body"`
	}
	if decode(r, &in) != nil || in.Rating < 1 || in.Rating > 5 || utf8.RuneCountInString(strings.TrimSpace(in.Body)) > 1000 {
		problem(w, 422, "INVALID_REVIEW", "Choose 1 to 5 stars and keep the note under 1,000 characters.")
		return
	}
	body := strings.TrimSpace(in.Body)
	if strings.ContainsFunc(body, func(r rune) bool { return r < 0x20 && r != '\n' }) {
		problem(w, 422, "INVALID_REVIEW", "Remove unusual characters from the note.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your review could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	var bookingID, sellerID, sellerUser, buyerName, state string
	var ends time.Time
	err = tx.QueryRow(r.Context(), `SELECT b.id::text,b.seller_id::text,sp.user_id::text,b.buyer_name,b.state,b.starts_at+b.duration_minutes*interval '1 minute' FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE b.id=$1 AND b.buyer_user_id=$2 FOR UPDATE OF b`, r.PathValue("id"), u.ID).Scan(&bookingID, &sellerID, &sellerUser, &buyerName, &state, &ends)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "Only the buyer of this booking can review it.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your review could not be saved.")
		return
	}
	if (state != "confirmed" && state != "completed") || ends.After(time.Now()) {
		problem(w, 409, "REVIEW_UNAVAILABLE", "You can review a booking after it has taken place.")
		return
	}
	if time.Since(ends) > reviewWindow {
		problem(w, 409, "REVIEW_WINDOW_CLOSED", "Reviews can be left for 60 days after a booking.")
		return
	}
	var reviewID string
	err = tx.QueryRow(r.Context(), `INSERT INTO reviews(booking_id,seller_id,buyer_user_id,reviewer_name,rating,body) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, bookingID, sellerID, u.ID, reviewerName(buyerName), in.Rating, body).Scan(&reviewID)
	if pgErrCode(err) == "23505" {
		problem(w, 409, "ALREADY_REVIEWED", "You have already reviewed this booking.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your review could not be saved.")
		return
	}
	if err = enqueueBookingEvent(r.Context(), tx, bookingID, sellerUser, "review_received_seller", reviewID+":review-received", &reviewID); err != nil {
		problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "Your review could not be saved.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your review could not be saved.")
		return
	}
	jsonOut(w, 201, map[string]string{"id": reviewID})
}

// replyToReview lets the seller answer a review once, publicly.
func (a *API) replyToReview(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Body) == "" || utf8.RuneCountInString(strings.TrimSpace(in.Body)) > 1000 {
		problem(w, 422, "INVALID_REPLY", "Write a reply under 1,000 characters.")
		return
	}
	tag, err := a.db.Exec(r.Context(), `UPDATE reviews rv SET seller_reply=$3,replied_at=now() FROM seller_profiles sp WHERE rv.id=$1 AND sp.id=rv.seller_id AND sp.user_id=$2 AND rv.seller_reply IS NULL`, r.PathValue("id"), u.ID, strings.TrimSpace(in.Body))
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your reply could not be saved.")
		return
	}
	if tag.RowsAffected() != 1 {
		problem(w, 409, "REPLY_UNAVAILABLE", "You can reply once to reviews of your own bookings.")
		return
	}
	jsonOut(w, 200, map[string]bool{"replied": true})
}

type reviewSummary struct {
	Average float64 `json:"average"`
	Count   int64   `json:"count"`
}

func (a *API) sellerReviewSummary(ctx context.Context, sellerID string) (reviewSummary, error) {
	var s reviewSummary
	err := a.db.QueryRow(ctx, `SELECT COALESCE(round(avg(rating)::numeric,1),0)::float8,count(*) FROM reviews WHERE seller_id=$1 AND hidden_at IS NULL`, sellerID).Scan(&s.Average, &s.Count)
	return s, err
}

// publicReviews lists a seller's visible reviews, newest first.
func (a *API) publicReviews(w http.ResponseWriter, r *http.Request) {
	handle := normalizeHandle(r.PathValue("handle"))
	if !validHandle(handle) {
		problem(w, 404, "NOT_FOUND", "This link is not available.")
		return
	}
	var sellerID string
	if err := a.db.QueryRow(r.Context(), `SELECT id::text FROM seller_profiles WHERE handle=$1 AND publication_state='published'`, handle).Scan(&sellerID); err != nil {
		problem(w, 404, "NOT_FOUND", "This link is not available.")
		return
	}
	summary, err := a.sellerReviewSummary(r.Context(), sellerID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Reviews could not be loaded.")
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id::text,reviewer_name,rating,body,seller_reply,created_at,replied_at FROM reviews WHERE seller_id=$1 AND hidden_at IS NULL ORDER BY created_at DESC LIMIT 30`, sellerID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Reviews could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, body string
		var rating int16
		var reply *string
		var created time.Time
		var replied *time.Time
		if err = rows.Scan(&id, &name, &rating, &body, &reply, &created, &replied); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Reviews could not be read.")
			return
		}
		items = append(items, map[string]any{"id": id, "reviewer": name, "rating": rating, "body": body, "seller_reply": reply, "created_at": created, "replied_at": replied})
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	jsonOut(w, 200, map[string]any{"average": summary.Average, "count": summary.Count, "reviews": items})
}

func (a *API) opsReviews(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT rv.id::text,rv.booking_id::text,sp.handle,rv.reviewer_name,rv.rating,rv.body,COALESCE(rv.seller_reply,''),rv.created_at,rv.hidden_at,COALESCE(rv.hidden_reason,'') FROM reviews rv JOIN seller_profiles sp ON sp.id=rv.seller_id ORDER BY rv.created_at DESC LIMIT 200`)
	if err != nil {
		problem(w, 503, "REVIEWS_UNAVAILABLE", "Reviews could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, booking, handle, name, body, reply, hiddenReason string
		var rating int16
		var created time.Time
		var hidden *time.Time
		if err = rows.Scan(&id, &booking, &handle, &name, &rating, &body, &reply, &created, &hidden, &hiddenReason); err != nil {
			problem(w, 503, "REVIEWS_UNAVAILABLE", "Reviews could not be read.")
			return
		}
		items = append(items, map[string]any{"id": id, "booking_id": booking, "seller": handle, "reviewer": name, "rating": rating, "body": body, "seller_reply": reply, "created_at": created, "hidden_at": hidden, "hidden_reason": hiddenReason})
	}
	jsonOut(w, 200, map[string]any{"reviews": items})
}

// opsSetReviewVisibility hides or restores a review (abuse, personal data).
func (a *API) opsSetReviewVisibility(hide bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := a.requireOps(w, r, "ops:booking:resolve")
		if !ok {
			return
		}
		var in struct {
			Reason string `json:"reason"`
		}
		if decode(r, &in) != nil || len(strings.TrimSpace(in.Reason)) < 8 || len(in.Reason) > 500 {
			problem(w, 422, "REASON_REQUIRED", "Record why this review is being hidden or restored.")
			return
		}
		tx, err := a.db.Begin(r.Context())
		if err != nil {
			problem(w, 503, "DATABASE_ERROR", "The review could not be updated.")
			return
		}
		defer tx.Rollback(r.Context())
		sql := `UPDATE reviews SET hidden_at=now(),hidden_reason=$2 WHERE id=$1 AND hidden_at IS NULL`
		action := "review.hidden"
		if !hide {
			sql = `UPDATE reviews SET hidden_at=NULL,hidden_reason=NULL WHERE id=$1 AND hidden_at IS NOT NULL AND $2::text IS NOT NULL`
			action = "review.restored"
		}
		tag, err := tx.Exec(r.Context(), sql, r.PathValue("id"), strings.TrimSpace(in.Reason))
		if err != nil {
			problem(w, 503, "DATABASE_ERROR", "The review could not be updated.")
			return
		}
		if tag.RowsAffected() != 1 {
			problem(w, 409, "REVIEW_STATE_CHANGED", "This review is already in that state.")
			return
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,$2,$3,$4,'{}')`, actor.ID, action, r.PathValue("id"), strings.TrimSpace(in.Reason)); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The review could not be updated.")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The review could not be updated.")
			return
		}
		jsonOut(w, 200, map[string]bool{"hidden": hide})
	}
}

// --- lifecycle worker ------------------------------------------------------------

// runLifecycleWorker settles no-show reports, moves refunds along and pays
// sellers once their payouts are due.
func (a *API) runLifecycleWorker(ctx context.Context) {
	for {
		err := a.resolveNoShows(ctx)
		if refundErr := a.processRefunds(ctx); err == nil {
			err = refundErr
		}
		if payoutErr := a.processPayouts(ctx); err == nil {
			err = payoutErr
		}
		if retentionErr := a.maybeApplyRetention(ctx, time.Now()); err == nil {
			err = retentionErr
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			a.log().ErrorContext(ctx, "lifecycle worker cycle failed", "error", err.Error())
		}
		a.beat(ctx, "lifecycle", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(noShowWorkerPeriod):
		}
	}
}
