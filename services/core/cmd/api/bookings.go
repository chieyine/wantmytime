package main

import (
	"context"
	"errors"
	"fmt"
	_ "image/jpeg"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

func (a *API) listBookings(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	var rows pgx.Rows
	var e error
	if guest {
		rows, e = a.db.Query(r.Context(), `SELECT b.id::text,sp.handle,b.buyer_name,b.duration_minutes,b.starts_at,b.gross_minor,b.state,b.payment_state,b.currency::text FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE b.buyer_user_id=$1 AND (b.id::text=ANY($2::text[]) OR b.quote_id::text=ANY($3::text[])) AND ($4::timestamptz IS NULL OR (b.starts_at,b.id)<($4,$5::uuid)) ORDER BY b.starts_at DESC,b.id DESC LIMIT 100`, u.ID, scope.BookingIDs, scope.QuoteIDs, cursorAt, cursorID)
	} else {
		rows, e = a.db.Query(r.Context(), `SELECT b.id::text,sp.handle,b.buyer_name,b.duration_minutes,b.starts_at,b.gross_minor,b.state,b.payment_state,b.currency::text FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE (sp.user_id=$1 OR b.buyer_user_id=$1) AND ($2::timestamptz IS NULL OR (b.starts_at,b.id)<($2,$3::uuid)) ORDER BY b.starts_at DESC,b.id DESC LIMIT 100`, u.ID, cursorAt, cursorID)
	}
	if e != nil {
		problem(w, 503, "DATABASE_ERROR", "Bookings could not be loaded.")
		return
	}
	defer rows.Close()
	type item struct {
		ID       string    `json:"id"`
		Seller   string    `json:"seller"`
		Buyer    string    `json:"buyer"`
		Duration int       `json:"duration_minutes"`
		Starts   time.Time `json:"starts_at"`
		Amount   int64     `json:"amount_minor"`
		State    string    `json:"state"`
		Payment  string    `json:"payment_state"`
		Currency string    `json:"currency"`
	}
	out := []item{}
	for rows.Next() {
		var x item
		if rows.Scan(&x.ID, &x.Seller, &x.Buyer, &x.Duration, &x.Starts, &x.Amount, &x.State, &x.Payment, &x.Currency) != nil {
			problem(w, 503, "DATABASE_ERROR", "Bookings could not be loaded.")
			return
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Bookings could not be loaded.")
		return
	}
	var nextCursor string
	if len(out) == operationsPageSize {
		nextCursor = encodeListCursor(out[len(out)-1].Starts, out[len(out)-1].ID)
	}
	jsonOut(w, 200, map[string]any{"bookings": out, "next_cursor": nextCursor})
}

func (a *API) getBooking(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if guest && !a.guestCanBooking(r.Context(), scope, id) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	if len(id) != 36 {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	var out struct {
		ID               string    `json:"id"`
		Seller           string    `json:"seller"`
		Timezone         string    `json:"timezone"`
		SellerName       string    `json:"seller_name"`
		Buyer            string    `json:"buyer"`
		Duration         int       `json:"duration_minutes"`
		Starts           time.Time `json:"starts_at"`
		Amount           string    `json:"amount_minor"`
		State            string    `json:"state"`
		Payment          string    `json:"payment_state"`
		MeetingURL       string    `json:"meeting_url,omitempty"`
		MeetingDeadline  time.Time `json:"meeting_deadline"`
		Role             string    `json:"role"`
		Issue            *string   `json:"issue_reason,omitempty"`
		BuyerDone        bool      `json:"buyer_completed"`
		SellerDone       bool      `json:"seller_completed"`
		CancellationOpen bool      `json:"cancellation_open"`
		Currency         string    `json:"currency"`
		// BuyerFee is the payment fee the buyer paid on top of the price.
		BuyerFee      int64   `json:"buyer_fee_minor"`
		MeetingSource *string `json:"meeting_source,omitempty"`
		// CancellationRequest is the buyer's open request to cancel outside
		// the policy, for the seller to answer.
		CancellationRequest *string `json:"cancellation_request_reason,omitempty"`
		CancellationByMe    bool    `json:"cancellation_request_mine"`
	}
	var encryptedMeeting []byte
	err := a.db.QueryRow(r.Context(), `SELECT b.id::text,sp.handle,sp.timezone,owner.display_name,b.buyer_name,b.duration_minutes,b.starts_at,b.gross_minor::text,b.state,b.payment_state,b.meeting_url,b.meeting_deadline,CASE WHEN sp.user_id=$2 THEN 'seller' ELSE 'buyer' END,b.issue_reason,b.buyer_completed_at IS NOT NULL,b.seller_completed_at IS NOT NULL,EXISTS(SELECT 1 FROM booking_cancellation_requests cr WHERE cr.booking_id=b.id AND cr.state='open'),b.currency::text,COALESCE((SELECT buyer_fee_minor FROM payment_attempts pa WHERE pa.booking_id=b.id AND pa.canonical_state='success' ORDER BY pa.created_at DESC LIMIT 1),0),b.meeting_source,(SELECT cr.reason FROM booking_cancellation_requests cr WHERE cr.booking_id=b.id AND cr.state='open' LIMIT 1),EXISTS(SELECT 1 FROM booking_cancellation_requests cr WHERE cr.booking_id=b.id AND cr.state='open' AND cr.requester_user_id=$2) FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id JOIN users owner ON owner.id=sp.user_id WHERE b.id=$1 AND (b.buyer_user_id=$2 OR sp.user_id=$2)`, id, u.ID).Scan(&out.ID, &out.Seller, &out.Timezone, &out.SellerName, &out.Buyer, &out.Duration, &out.Starts, &out.Amount, &out.State, &out.Payment, &encryptedMeeting, &out.MeetingDeadline, &out.Role, &out.Issue, &out.BuyerDone, &out.SellerDone, &out.CancellationOpen, &out.Currency, &out.BuyerFee, &out.MeetingSource, &out.CancellationRequest, &out.CancellationByMe)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The booking could not be loaded.")
		return
	}
	if len(encryptedMeeting) > 0 {
		decrypted, decryptErr := a.decryptMeetingLink(encryptedMeeting)
		if decryptErr != nil {
			problem(w, 503, "MEETING_LINK_UNAVAILABLE", "The private meeting link could not be loaded.")
			return
		}
		out.MeetingURL = string(decrypted)
	}
	extra, err := a.bookingLifecycle(r.Context(), out.ID, u.ID, out.Role)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The booking could not be loaded.")
		return
	}
	extra["booking"] = out
	extra["local_simulator"] = out.Payment == "simulated"
	jsonOut(w, 200, extra)
}

// bookingLifecycle adds cancellation, refund, no-show and review details to a
// booking for one participant.
func (a *API) bookingLifecycle(ctx context.Context, bookingID, userID, role string) (map[string]any, error) {
	var cancelledBy string
	var cancelledAt *time.Time
	var starts time.Time
	var duration int
	var state string
	if err := a.db.QueryRow(ctx, `SELECT COALESCE(cancelled_by_role,''),cancelled_at,starts_at,duration_minutes,state FROM bookings WHERE id=$1`, bookingID).Scan(&cancelledBy, &cancelledAt, &starts, &duration, &state); err != nil {
		return nil, err
	}
	out := map[string]any{"cancellation_policy": cancellationRule, "cancelled_at": cancelledAt, "cancelled_by_role": cancelledBy}
	var refundAmount int64
	var refundState string
	switch err := a.db.QueryRow(ctx, `SELECT amount_minor,state FROM refunds WHERE booking_id=$1 ORDER BY (state='failed'),created_at DESC LIMIT 1`, bookingID).Scan(&refundAmount, &refundState); {
	case err == nil:
		out["refund"] = map[string]any{"amount_minor": refundAmount, "state": refundState}
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}
	var absent, nsState, reporter string
	var resolves time.Time
	switch err := a.db.QueryRow(ctx, `SELECT absent_role,state,resolves_at,reporter_user_id::text FROM no_show_reports WHERE booking_id=$1`, bookingID).Scan(&absent, &nsState, &resolves, &reporter); {
	case err == nil:
		out["no_show"] = map[string]any{"absent_role": absent, "state": nsState, "resolves_at": resolves, "reported_by_me": reporter == userID, "about_me": absent == role}
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}
	var reviewID, body string
	var rating int16
	var reply *string
	switch err := a.db.QueryRow(ctx, `SELECT id::text,rating,body,seller_reply FROM reviews WHERE booking_id=$1`, bookingID).Scan(&reviewID, &rating, &body, &reply); {
	case err == nil:
		out["review"] = map[string]any{"id": reviewID, "rating": rating, "body": body, "seller_reply": reply}
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}
	ends := starts.Add(time.Duration(duration) * time.Minute)
	_, reviewed := out["review"]
	out["can_review"] = role == "buyer" && !reviewed && (state == "confirmed" || state == "completed") && ends.Before(time.Now()) && time.Since(ends) <= reviewWindow
	out["can_report_no_show"] = state == "confirmed" && out["no_show"] == nil && time.Now().After(starts.Add(noShowEarliest)) && time.Now().Before(ends.Add(noShowLatest()))
	problemDeadline := ends.Add(disputeWindow())
	out["problem_deadline"] = problemDeadline
	var issueOpen bool
	var issueBy, resolution string
	var issueReason, sellerResponse string
	var respondBy *time.Time
	if err := a.db.QueryRow(ctx, `SELECT issue_reason IS NOT NULL AND issue_resolved_at IS NULL,COALESCE(issue_reported_by,''),COALESCE(issue_resolution,''),COALESCE(issue_reason,''),COALESCE(issue_seller_response,''),issue_respond_by FROM bookings WHERE id=$1`, bookingID).Scan(&issueOpen, &issueBy, &resolution, &issueReason, &sellerResponse, &respondBy); err != nil {
		return nil, err
	}
	problemOut := map[string]any{"open": issueOpen, "reported_by": issueBy, "resolution": resolution, "seller_response": sellerResponse, "respond_by": respondBy}
	if issueOpen && issueBy == "buyer" && role == "seller" {
		problemOut["reason"] = issueReason
	}
	out["problem"] = problemOut
	out["can_answer_problem"] = issueOpen && issueBy == "buyer" && role == "seller" && sellerResponse == ""
	out["can_report_problem"] = !issueOpen && (state == "confirmed" || state == "completed") && (role == "seller" || time.Now().Before(problemDeadline))
	if role == "seller" {
		rows, err := a.queryPayouts(ctx, `po.booking_id=$2`, bookingID)
		if err != nil {
			return nil, err
		}
		if len(rows) == 1 {
			out["payout"] = rows[0].json(false)
		}
	}
	return out, nil
}

func (a *API) getBookingReceipt(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && !a.guestCanBooking(r.Context(), scope, r.PathValue("id")) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	var id, buyer, seller, bookingState, paymentState, currency, reference, role, sellerHandle string
	var hasSellerProfile bool
	var starts, created, paidAt time.Time
	var duration int
	var gross, deduction, entitlement, buyerFee, refunded int64
	var channel string
	err := a.db.QueryRow(r.Context(), `SELECT b.id::text,b.buyer_name,owner.display_name,b.starts_at,b.duration_minutes,b.gross_minor,b.currency,b.created_at,b.state,b.payment_state,COALESCE(pa.deduction_minor,0),COALESCE(pa.seller_entitlement_minor,0),COALESCE(p.merchant_reference,''),CASE WHEN b.buyer_user_id=$2 THEN 'buyer' ELSE 'seller' END,sp.handle,EXISTS(SELECT 1 FROM seller_profiles mine WHERE mine.user_id=$2),COALESCE(p.last_verified_at,b.created_at),COALESCE(p.buyer_fee_minor,0),COALESCE(p.channel,''),COALESCE((SELECT sum(amount_minor) FROM refunds rf WHERE rf.booking_id=b.id AND rf.state='processed'),0)::bigint FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id JOIN users owner ON owner.id=sp.user_id LEFT JOIN payment_allocations pa ON pa.booking_id=b.id LEFT JOIN LATERAL (SELECT merchant_reference,last_verified_at,buyer_fee_minor,channel FROM payment_attempts WHERE booking_id=b.id AND canonical_state='success' ORDER BY created_at DESC LIMIT 1) p ON true WHERE b.id=$1 AND (b.buyer_user_id=$2 OR sp.user_id=$2)`, r.PathValue("id"), u.ID).Scan(&id, &buyer, &seller, &starts, &duration, &gross, &currency, &created, &bookingState, &paymentState, &deduction, &entitlement, &reference, &role, &sellerHandle, &hasSellerProfile, &paidAt, &buyerFee, &channel, &refunded)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "RECEIPT_UNAVAILABLE", "The receipt could not be loaded.")
		return
	}
	if (paymentState != "paid" && paymentState != "refunded" && paymentState != "partially_refunded") || entitlement == 0 {
		problem(w, 404, "RECEIPT_NOT_AVAILABLE", "No verified payment receipt is available for this booking.")
		return
	}
	out := map[string]any{"id": id, "buyer_name": buyer, "seller_name": seller, "starts_at": starts, "duration_minutes": duration, "currency": strings.TrimSpace(currency), "gross_minor": gross, "payment_state": paymentState, "booking_state": bookingState, "paid_at": paidAt, "booked_at": created, "provider_reference": reference, "viewer_role": role, "seller_handle": sellerHandle, "has_seller_profile": hasSellerProfile, "refunded_minor": refunded, "payment_method": channel}
	if role == "buyer" {
		// The buyer sees what they paid: the price and the payment fee on top.
		out["fee_minor"], out["total_minor"] = buyerFee, gross+buyerFee
	} else {
		// The seller sees their share; the buyer's payment fee is not theirs.
		out["deduction_minor"], out["seller_entitlement_minor"] = deduction, entitlement
	}
	jsonOut(w, 200, out)
}

func (a *API) listReschedules(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	bookingID := r.PathValue("id")
	if guest && !a.guestCanBooking(r.Context(), scope, bookingID) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	var participant bool
	err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE b.id=$1 AND (b.buyer_user_id=$2 OR sp.user_id=$2))`, bookingID, u.ID).Scan(&participant)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Reschedule history could not be loaded.")
		return
	}
	if !participant {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id::text,requester_user_id=$2,proposed_starts_at,state,expires_at,created_at FROM reschedule_requests WHERE booking_id=$1 ORDER BY created_at DESC LIMIT 20`, bookingID, u.ID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Reschedule history could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, state string
		var requestedByMe bool
		var proposed, expires, created time.Time
		if err := rows.Scan(&id, &requestedByMe, &proposed, &state, &expires, &created); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Reschedule history could not be read.")
			return
		}
		items = append(items, map[string]any{"id": id, "requested_by_me": requestedByMe, "proposed_starts_at": proposed, "state": state, "expires_at": expires, "created_at": created})
	}
	if rows.Err() != nil {
		problem(w, 503, "DATABASE_ERROR", "Reschedule history could not be loaded.")
		return
	}
	jsonOut(w, 200, map[string]any{"requests": items})
}

func (a *API) createReschedule(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && !a.guestCanBooking(r.Context(), scope, r.PathValue("id")) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	var in struct {
		Proposed time.Time `json:"proposed_starts_at"`
	}
	if decode(r, &in) != nil || in.Proposed.IsZero() {
		problem(w, 422, "INVALID_RESCHEDULE", "Choose a valid replacement time.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "A reschedule request could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	var bookingID, buyerID, ownerID, sellerID, zone, state string
	var oldStart time.Time
	var duration, notice, horizon, buffer int
	err = tx.QueryRow(r.Context(), `SELECT b.id::text,b.buyer_user_id::text,sp.user_id::text,sp.id::text,b.starts_at,b.duration_minutes,b.state,sp.timezone,sp.minimum_notice_minutes,sp.booking_horizon_days,sp.buffer_minutes FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE b.id=$1 AND (b.buyer_user_id=$2 OR sp.user_id=$2) FOR UPDATE OF b,sp`, r.PathValue("id"), u.ID).Scan(&bookingID, &buyerID, &ownerID, &sellerID, &oldStart, &duration, &state, &zone, &notice, &horizon, &buffer)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "A reschedule request could not be saved.")
		return
	}
	if state != "confirmed" || !oldStart.After(time.Now()) {
		problem(w, 409, "RESCHEDULE_UNAVAILABLE", "Only upcoming confirmed bookings can be rescheduled.")
		return
	}
	if in.Proposed.Equal(oldStart) || in.Proposed.Before(time.Now().Add(time.Duration(notice)*time.Minute)) || in.Proposed.After(time.Now().AddDate(0, 0, horizon)) {
		problem(w, 409, "SLOT_UNAVAILABLE", "That time is outside the current booking window.")
		return
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		problem(w, 503, "INVALID_SELLER_TIMEZONE", "Availability is not configured correctly.")
		return
	}
	proposed := in.Proposed.UTC()
	if !validScheduledTime(r.Context(), tx, sellerID, zone, proposed.In(location), proposed, duration, buffer, &bookingID) {
		problem(w, 409, "SLOT_UNAVAILABLE", "That time is not within the seller’s current availability.")
		return
	}
	if _, err = tx.Exec(r.Context(), `WITH expired AS (UPDATE reschedule_requests SET state='expired',version=version+1 WHERE booking_id=$1 AND state='pending' AND expires_at<=now() RETURNING id,proposed_starts_at) INSERT INTO reschedule_events(id,request_id,actor_user_id,action,previous_starts_at,proposed_starts_at) SELECT gen_random_uuid(),id,$2,'expired',$3,proposed_starts_at FROM expired`, bookingID, u.ID, oldStart); err != nil {
		problem(w, 503, "DATABASE_ERROR", "A reschedule request could not be saved.")
		return
	}
	var pending bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM reschedule_requests WHERE booking_id=$1 AND state='pending')`, bookingID).Scan(&pending); err != nil {
		problem(w, 503, "DATABASE_ERROR", "A reschedule request could not be saved.")
		return
	}
	if pending {
		problem(w, 409, "RESCHEDULE_PENDING", "A reschedule request is already waiting for a response.")
		return
	}
	id, err := randomUUID()
	if err != nil {
		problem(w, 500, "RESCHEDULE_ERROR", "A reschedule request could not be created.")
		return
	}
	expires := time.Now().Add(48 * time.Hour)
	_, err = tx.Exec(r.Context(), `INSERT INTO reschedule_requests(id,booking_id,requester_user_id,proposed_starts_at,expires_at) VALUES($1,$2,$3,$4,$5)`, id, bookingID, u.ID, proposed, expires)
	if pgErrCode(err) == "23505" {
		problem(w, 409, "RESCHEDULE_PENDING", "A reschedule request is already waiting for a response.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "A reschedule request could not be saved.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO reschedule_events(id,request_id,actor_user_id,action,previous_starts_at,proposed_starts_at) VALUES(gen_random_uuid(),$1,$2,'proposed',$3,$4)`, id, u.ID, oldStart, proposed); err != nil {
		problem(w, 503, "DATABASE_ERROR", "A reschedule request could not be saved.")
		return
	}
	counterpart, audience := buyerID, "buyer"
	if u.ID == buyerID {
		counterpart, audience = ownerID, "seller"
	}
	if err = enqueueBookingEvent(r.Context(), tx, bookingID, counterpart, "reschedule_requested_"+audience, id+":reschedule-requested", &id); err != nil {
		problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "A reschedule request could not be saved.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "A reschedule request could not be saved.")
		return
	}
	jsonOut(w, 201, map[string]any{"id": id, "state": "pending", "proposed_starts_at": proposed, "expires_at": expires, "message": "The other participant must accept before the booking time changes."})
}

func (a *API) respondReschedule(accept bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok, guest, scope := a.buyerActor(w, r)
		if !ok {
			return
		}
		if guest && !a.guestCanReschedule(r.Context(), scope, r.PathValue("id")) {
			problem(w, 404, "NOT_FOUND", "This reschedule request is not available.")
			return
		}
		tx, err := a.db.Begin(r.Context())
		if err != nil {
			problem(w, 503, "DATABASE_ERROR", "The reschedule request could not be updated.")
			return
		}
		defer tx.Rollback(r.Context())
		var requestID, requester, bookingID, buyerID, ownerID, sellerID, zone, bookingState, requestState string
		var proposed, expires, oldStart time.Time
		var duration, notice, horizon, buffer int
		err = tx.QueryRow(r.Context(), `SELECT rr.id::text,rr.requester_user_id::text,rr.proposed_starts_at,rr.expires_at,rr.state,b.id::text,b.buyer_user_id::text,sp.user_id::text,sp.id::text,b.starts_at,b.duration_minutes,b.state,sp.timezone,sp.minimum_notice_minutes,sp.booking_horizon_days,sp.buffer_minutes FROM reschedule_requests rr JOIN bookings b ON b.id=rr.booking_id JOIN seller_profiles sp ON sp.id=b.seller_id JOIN slot_reservations sr ON sr.booking_id=b.id AND sr.active WHERE rr.id=$1 FOR UPDATE OF rr,b,sr`, r.PathValue("id")).Scan(&requestID, &requester, &proposed, &expires, &requestState, &bookingID, &buyerID, &ownerID, &sellerID, &oldStart, &duration, &bookingState, &zone, &notice, &horizon, &buffer)
		if errors.Is(err, pgx.ErrNoRows) {
			problem(w, 404, "NOT_FOUND", "This reschedule request is not available.")
			return
		}
		if err != nil {
			problem(w, 503, "DATABASE_ERROR", "The reschedule request could not be loaded.")
			return
		}
		if (u.ID != buyerID && u.ID != ownerID) || u.ID == requester {
			problem(w, 404, "NOT_FOUND", "This reschedule request is not available to you.")
			return
		}
		if requestState != "pending" || bookingState != "confirmed" {
			problem(w, 409, "RESCHEDULE_UNAVAILABLE", "This reschedule request is no longer active.")
			return
		}
		if !expires.After(time.Now()) {
			_, _ = tx.Exec(r.Context(), `UPDATE reschedule_requests SET state='expired',version=version+1 WHERE id=$1`, requestID)
			_, _ = tx.Exec(r.Context(), `INSERT INTO reschedule_events(id,request_id,actor_user_id,action,previous_starts_at,proposed_starts_at) VALUES(gen_random_uuid(),$1,$2,'expired',$3,$4)`, requestID, u.ID, oldStart, proposed)
			_ = tx.Commit(r.Context())
			problem(w, 409, "RESCHEDULE_EXPIRED", "This request expired. The original booking time remains.")
			return
		}
		newState := "declined"
		if accept {
			if !oldStart.After(time.Now()) {
				_, _ = tx.Exec(r.Context(), `UPDATE reschedule_requests SET state='expired',version=version+1 WHERE id=$1`, requestID)
				_, _ = tx.Exec(r.Context(), `INSERT INTO reschedule_events(id,request_id,actor_user_id,action,previous_starts_at,proposed_starts_at) VALUES(gen_random_uuid(),$1,$2,'expired',$3,$4)`, requestID, u.ID, oldStart, proposed)
				_ = tx.Commit(r.Context())
				problem(w, 409, "RESCHEDULE_EXPIRED", "The original appointment has passed. The booking time was not changed.")
				return
			}
			location, loadErr := time.LoadLocation(zone)
			if loadErr != nil {
				problem(w, 503, "INVALID_SELLER_TIMEZONE", "Availability is not configured correctly.")
				return
			}
			if proposed.Before(time.Now().Add(time.Duration(notice)*time.Minute)) || proposed.After(time.Now().AddDate(0, 0, horizon)) || !validScheduledTime(r.Context(), tx, sellerID, zone, proposed.In(location), proposed, duration, buffer, &bookingID) {
				problem(w, 409, "SLOT_UNAVAILABLE", "The proposed time is no longer available. The original booking remains.")
				return
			}
			newState = "accepted"
			newEnd := proposed.Add(time.Duration(duration+buffer) * time.Minute)
			if err = releaseExpiredHolds(r.Context(), tx, sellerID, proposed, newEnd); err != nil {
				problem(w, 503, "DATABASE_ERROR", "The new time could not be reserved.")
				return
			}
			_, err = tx.Exec(r.Context(), `UPDATE slot_reservations SET occupied_from=$2,occupied_to=$3 WHERE booking_id=$1 AND active`, bookingID, proposed, proposed.Add(time.Duration(duration+buffer)*time.Minute))
			if pgErrCode(err) == "23P01" {
				problem(w, 409, "SLOT_UNAVAILABLE", "That time was just taken. The original booking remains.")
				return
			}
			if err != nil {
				problem(w, 503, "DATABASE_ERROR", "The new time could not be reserved.")
				return
			}
			_, err = tx.Exec(r.Context(), `UPDATE bookings SET starts_at=$2::timestamptz,meeting_deadline=$2::timestamptz-interval '30 minutes',calendar_sequence=calendar_sequence+1 WHERE id=$1`, bookingID, proposed)
			if err != nil {
				problem(w, 503, "DATABASE_ERROR", "The new time could not be saved.")
				return
			}
			if err = store.New(tx).CancelPendingBookingReminders(r.Context(), store.CancelPendingBookingRemindersParams{ReasonCode: "RESCHEDULED", BookingID: bookingID}); err != nil {
				problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The new time could not be saved.")
				return
			}
			if err = enqueueBookingReminders(r.Context(), tx, bookingID, buyerID, ownerID, proposed); err != nil {
				problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The new time could not be saved.")
				return
			}
			if err = a.enqueueRescheduleEmails(r.Context(), tx, bookingID, requestID); err != nil {
				problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The new time could not be saved.")
				return
			}
			if err = enqueueCalendarSync(r.Context(), tx, bookingID); err != nil {
				problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The new time could not be saved.")
				return
			}
		}
		if !accept {
			audience := "buyer"
			if requester == ownerID {
				audience = "seller"
			}
			if err = enqueueBookingEvent(r.Context(), tx, bookingID, requester, "reschedule_declined_"+audience, requestID+":reschedule-declined", &requestID); err != nil {
				problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The reschedule response could not be saved.")
				return
			}
		}
		_, err = tx.Exec(r.Context(), `UPDATE reschedule_requests SET state=$2,responded_by=$3,responded_at=now(),version=version+1 WHERE id=$1`, requestID, newState, u.ID)
		if err != nil {
			problem(w, 503, "DATABASE_ERROR", "The reschedule response could not be saved.")
			return
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO reschedule_events(id,request_id,actor_user_id,action,previous_starts_at,proposed_starts_at) VALUES(gen_random_uuid(),$1,$2,$3,$4,$5)`, requestID, u.ID, newState, oldStart, proposed); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The reschedule response could not be saved.")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			problem(w, 503, "DATABASE_ERROR", "The reschedule response could not be saved.")
			return
		}
		jsonOut(w, 200, map[string]any{"state": newState, "starts_at": func() time.Time {
			if accept {
				return proposed
			}
			return oldStart
		}(), "message": func() string {
			if accept {
				return "The booking time has changed for both participants."
			}
			return "The original booking time remains unchanged."
		}()})
	}
}

func (a *API) updateMeetingLink(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var in struct {
		URL string `json:"meeting_url"`
	}
	if decode(r, &in) != nil {
		problem(w, 400, "INVALID_BODY", "Enter a secure meeting link.")
		return
	}
	parsed, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || len(in.URL) > 2048 {
		problem(w, 422, "INVALID_MEETING_URL", "Meeting links must use a valid HTTPS URL.")
		return
	}
	ciphertext, err := a.encryptMeetingLink([]byte(parsed.String()))
	if err != nil {
		problem(w, 503, "MEETING_LINK_ENCRYPTION_UNAVAILABLE", "Secure meeting-link storage is not configured.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The meeting link could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `UPDATE bookings b SET meeting_url=$1,meeting_ready_at=now(),meeting_source='seller' FROM seller_profiles sp WHERE b.id=$2 AND b.seller_id=sp.id AND sp.user_id=$3 AND b.state='confirmed' AND b.starts_at>now()`, ciphertext, r.PathValue("id"), u.ID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The meeting link could not be saved.")
		return
	}
	if tag.RowsAffected() != 1 {
		problem(w, 404, "NOT_FOUND", "This seller booking is not available to update.")
		return
	}
	if err = a.enqueueMeetingReady(r.Context(), tx, r.PathValue("id")); err != nil {
		problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The meeting link could not be safely saved.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The meeting link could not be saved.")
		return
	}
	jsonOut(w, 200, map[string]bool{"saved": true})
}

// reportBookingIssue records a problem with a booking. A buyer can report
// one until the dispute window after the session closes; that holds the
// seller's payout while the seller answers (refund, part refund, or
// disagree). No answer in time refunds the buyer; only a disagreement goes
// to WantMyTime.
func (a *API) reportBookingIssue(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && !a.guestCanBooking(r.Context(), scope, r.PathValue("id")) {
		problem(w, 404, "NOT_FOUND", "This booking is not available to report.")
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if decode(r, &in) != nil || len(strings.TrimSpace(in.Reason)) < 8 || len(in.Reason) > 1000 {
		problem(w, 422, "ISSUE_DETAILS_REQUIRED", "Describe the problem in at least a few words.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The problem could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	b, err := lockParticipantBooking(r.Context(), tx, r.PathValue("id"), u.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This booking is not available to report.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The problem could not be saved.")
		return
	}
	role := "buyer"
	if u.ID == b.SellerUserID {
		role = "seller"
	}
	ends := b.StartsAt.Add(time.Duration(b.DurationMinutes) * time.Minute)
	if b.State != "confirmed" && b.State != "completed" {
		problem(w, 409, "BOOKING_NOT_ACTIVE", "This booking can no longer be reported.")
		return
	}
	if role == "buyer" && time.Now().After(ends.Add(disputeWindow())) {
		problem(w, 409, "PROBLEM_WINDOW_CLOSED", "Problems must be reported within "+humanDuration(disputeWindow())+" of the end of the booking.")
		return
	}
	var respondBy *time.Time
	if role == "buyer" {
		due := time.Now().Add(problemResponseWindow())
		respondBy = &due
	}
	tag, err := tx.Exec(r.Context(), `UPDATE bookings SET issue_reason=$1,issue_created_at=now(),issue_resolved_at=NULL,issue_resolution=NULL,issue_reported_by=$3,issue_respond_by=$4,issue_seller_response=NULL,issue_seller_note=NULL,issue_disputed_at=NULL WHERE id=$2 AND (issue_reason IS NULL OR issue_resolved_at IS NOT NULL)`, strings.TrimSpace(in.Reason), b.BookingID, role, respondBy)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The problem could not be saved.")
		return
	}
	if tag.RowsAffected() != 1 {
		problem(w, 409, "PROBLEM_ALREADY_OPEN", "A problem is already being reviewed for this booking.")
		return
	}
	if role == "buyer" {
		if err = enqueuePush(r.Context(), tx, b.BookingID, b.SellerUserID, "problem_reported_seller", b.BookingID+":push:problem:"+time.Now().UTC().Format(time.RFC3339Nano), time.Now()); err != nil {
			problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The problem could not be saved.")
			return
		}
		if err = enqueueBookingEvent(r.Context(), tx, b.BookingID, b.SellerUserID, "problem_reported_seller", b.BookingID+":problem:"+time.Now().UTC().Format(time.RFC3339Nano), nil); err != nil {
			problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The problem could not be saved.")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The problem could not be saved.")
		return
	}
	jsonOut(w, 201, map[string]bool{"reported": true})
}

// humanDuration writes 2h as "2 hours" and 90m as "90 minutes".
func humanDuration(d time.Duration) string {
	if d%time.Hour == 0 {
		if d == time.Hour {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", int(d/time.Hour))
	}
	return fmt.Sprintf("%d minutes", int(d/time.Minute))
}

func (a *API) requestCancellation(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && !a.guestCanBooking(r.Context(), scope, r.PathValue("id")) {
		problem(w, 404, "NOT_FOUND", "This booking is not available to cancel.")
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if decode(r, &in) != nil || len(strings.TrimSpace(in.Reason)) < 8 || len(in.Reason) > 500 {
		problem(w, 422, "CANCELLATION_REASON_REQUIRED", "Describe why you are requesting cancellation.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The cancellation request could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	var id, buyerID, ownerID string
	err = tx.QueryRow(r.Context(), `INSERT INTO booking_cancellation_requests(booking_id,requester_user_id,reason) SELECT b.id,$2,$3 FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE b.id=$1 AND (b.buyer_user_id=$2 OR sp.user_id=$2) AND b.state='confirmed' AND b.starts_at>now() RETURNING id::text,(SELECT buyer_user_id::text FROM bookings WHERE id=$1),(SELECT sp2.user_id::text FROM bookings b2 JOIN seller_profiles sp2 ON sp2.id=b2.seller_id WHERE b2.id=$1)`, r.PathValue("id"), u.ID, strings.TrimSpace(in.Reason)).Scan(&id, &buyerID, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This upcoming booking is not available to cancel.")
		return
	}
	if err != nil {
		if pgErrCode(err) == "23505" {
			problem(w, 409, "CANCELLATION_ALREADY_OPEN", "A cancellation request is already open for this booking.")
			return
		}
		problem(w, 503, "DATABASE_ERROR", "The cancellation request could not be saved.")
		return
	}
	counterpart, audience := buyerID, "buyer"
	if u.ID == buyerID {
		counterpart, audience = ownerID, "seller"
	}
	if err = enqueueBookingEvent(r.Context(), tx, r.PathValue("id"), counterpart, "cancellation_requested_"+audience, id+":cancellation-requested", &id); err != nil {
		problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The cancellation request could not be saved.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The cancellation request could not be saved.")
		return
	}
	message := "Your request has gone to the seller. If they agree, they cancel the booking and you get a full refund. If not, the booking stays as it is."
	if audience == "buyer" {
		message = "Your request has gone to the buyer. The booking stays as it is unless one of you cancels it."
	}
	jsonOut(w, 201, map[string]any{"id": id, "state": "open", "message": message})
}

func (a *API) completeBooking(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && !a.guestCanBooking(r.Context(), scope, r.PathValue("id")) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Completion could not be recorded.")
		return
	}
	defer tx.Rollback(r.Context())
	var sellerUser string
	var buyer string
	var state string
	var ends time.Time
	err = tx.QueryRow(r.Context(), `SELECT sp.user_id::text,b.buyer_user_id::text,b.state,b.starts_at+(b.duration_minutes*interval '1 minute') FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE b.id=$1 AND (sp.user_id=$2 OR b.buyer_user_id=$2) FOR UPDATE OF b`, r.PathValue("id"), u.ID).Scan(&sellerUser, &buyer, &state, &ends)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	if err != nil || state != "confirmed" || ends.After(time.Now()) {
		problem(w, 409, "BOOKING_NOT_ACTIVE", "Only a confirmed booking can be completed.")
		return
	}
	column := "buyer_completed_at"
	if u.ID == sellerUser {
		column = "seller_completed_at"
	}
	if _, err = tx.Exec(r.Context(), `UPDATE bookings SET `+column+`=COALESCE(`+column+`,now()) WHERE id=$1`, r.PathValue("id")); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Completion could not be recorded.")
		return
	}
	var both bool
	if err = tx.QueryRow(r.Context(), `SELECT buyer_completed_at IS NOT NULL AND seller_completed_at IS NOT NULL FROM bookings WHERE id=$1`, r.PathValue("id")).Scan(&both); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Completion could not be loaded.")
		return
	}
	if both {
		_, err = tx.Exec(r.Context(), `UPDATE bookings SET state='completed' WHERE id=$1`, r.PathValue("id"))
		if err != nil {
			problem(w, 503, "DATABASE_ERROR", "Completion could not be finalized.")
			return
		}
	}
	if tx.Commit(r.Context()) != nil {
		problem(w, 503, "DATABASE_ERROR", "Completion could not be recorded.")
		return
	}
	jsonOut(w, 200, map[string]any{"completed": true, "both_participants_confirmed": both})
}

func (a *API) bookingCalendar(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && !a.guestCanBooking(r.Context(), scope, r.PathValue("id")) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	var seller, buyer string
	var starts time.Time
	var duration, sequence int
	var name string
	err := a.db.QueryRow(r.Context(), `SELECT sp.handle,b.buyer_name,b.starts_at,b.duration_minutes,b.calendar_sequence FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE b.id=$1 AND (b.buyer_user_id=$2 OR sp.user_id=$2)`, r.PathValue("id"), u.ID).Scan(&seller, &buyer, &starts, &duration, &sequence)
	if err != nil {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	name = fmt.Sprintf("Time with %s", seller)
	bookingURL := appOrigin() + "/booking/" + r.PathValue("id")
	body := calendarEvent{UID: r.PathValue("id"), Sequence: sequence, Start: starts, End: starts.Add(time.Duration(duration) * time.Minute), Summary: name, Description: "Booked by " + buyer + ". The private meeting link is on your booking page: " + bookingURL, URL: bookingURL}.ICS()
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=wantmytime-booking.ics")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}
