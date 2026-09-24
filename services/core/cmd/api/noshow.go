package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

const (
	noShowEarliest     = 10 * time.Minute // after the start
	noShowDisputeTime  = 24 * time.Hour
	noShowWorkerPeriod = time.Minute
)

// noShowLatest is how long after the end a no-show can be reported: the same
// window as any other problem, so the seller can be paid once it closes.
func noShowLatest() time.Duration { return disputeWindow() }

// reportNoShow lets a participant say the other person never joined. The
// other person can dispute it for 24 hours; after that it stands. A report
// that the seller didn't join holds their payout until it is settled.
func (a *API) reportNoShow(w http.ResponseWriter, r *http.Request) {
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
		problem(w, 503, "DATABASE_ERROR", "The report could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	b, err := lockParticipantBooking(r.Context(), tx, r.PathValue("id"), u.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The report could not be saved.")
		return
	}
	now := time.Now()
	ends := b.StartsAt.Add(time.Duration(b.DurationMinutes) * time.Minute)
	if b.State != "confirmed" {
		problem(w, 409, "NO_SHOW_UNAVAILABLE", "This booking can't be reported as a no-show.")
		return
	}
	if now.Before(b.StartsAt.Add(noShowEarliest)) {
		problem(w, 409, "NO_SHOW_TOO_EARLY", "You can report a no-show from 10 minutes after the start time.")
		return
	}
	if now.After(ends.Add(noShowLatest())) {
		problem(w, 409, "NO_SHOW_TOO_LATE", "No-shows must be reported within "+humanDuration(noShowLatest())+" of the end of the booking.")
		return
	}
	var alreadyCompleted bool
	column := "buyer_completed_at"
	absent, absentUser := "seller", b.SellerUserID
	if u.ID == b.SellerUserID {
		column, absent, absentUser = "seller_completed_at", "buyer", b.BuyerUserID
	}
	if err = tx.QueryRow(r.Context(), `SELECT `+column+` IS NOT NULL FROM bookings WHERE id=$1`, b.BookingID).Scan(&alreadyCompleted); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The report could not be saved.")
		return
	}
	if alreadyCompleted {
		problem(w, 409, "ALREADY_COMPLETED", "You already confirmed this conversation took place.")
		return
	}
	var reportID string
	err = tx.QueryRow(r.Context(), `INSERT INTO no_show_reports(booking_id,reporter_user_id,absent_role,resolves_at) VALUES($1,$2,$3,$4) RETURNING id::text`, b.BookingID, u.ID, absent, now.Add(noShowDisputeTime)).Scan(&reportID)
	if pgErrCode(err) == "23505" {
		problem(w, 409, "NO_SHOW_ALREADY_REPORTED", "A no-show has already been reported for this booking.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "The report could not be saved.")
		return
	}
	if err = enqueueBookingEvent(r.Context(), tx, b.BookingID, absentUser, "no_show_reported_"+absent, reportID+":no-show-reported", &reportID); err != nil {
		problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The report could not be saved.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The report could not be saved.")
		return
	}
	jsonOut(w, 201, map[string]any{"id": reportID, "state": "open", "absent_role": absent, "resolves_at": now.Add(noShowDisputeTime)})
}

// disputeNoShow is the reported person's answer: "I was there".
func (a *API) disputeNoShow(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, scope := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest && !a.guestCanBooking(r.Context(), scope, r.PathValue("id")) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if decode(r, &in) != nil || len(strings.TrimSpace(in.Reason)) < 8 || len(in.Reason) > 1000 {
		problem(w, 422, "REASON_REQUIRED", "Tell us what happened, in a sentence or two.")
		return
	}
	tag, err := a.db.Exec(r.Context(), `UPDATE no_show_reports ns SET state='disputed',dispute_reason=$3
		FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id
		WHERE ns.booking_id=$1 AND b.id=ns.booking_id AND ns.state='open' AND ns.resolves_at>now()
		  AND ((ns.absent_role='buyer' AND b.buyer_user_id=$2) OR (ns.absent_role='seller' AND sp.user_id=$2))`, r.PathValue("id"), u.ID, strings.TrimSpace(in.Reason))
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your response could not be saved.")
		return
	}
	if tag.RowsAffected() != 1 {
		problem(w, 409, "NO_SHOW_NOT_DISPUTABLE", "There is no open no-show report about you on this booking.")
		return
	}
	jsonOut(w, 200, map[string]string{"state": "disputed", "message": "Thanks. WantMyTime will review both sides and email you the outcome."})
}

// resolveNoShows accepts undisputed reports once their window has passed.
func (a *API) resolveNoShows(ctx context.Context) error {
	rows, err := a.db.Query(ctx, `SELECT id::text FROM no_show_reports WHERE state='open' AND resolves_at<=now() ORDER BY resolves_at LIMIT 20`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err = a.settleNoShow(ctx, id, "", "accepted", nil, "Not disputed within 24 hours."); err != nil {
			return err
		}
	}
	return nil
}

// settleNoShow applies a no-show outcome. absent is "buyer", "seller" or ""
// (use the report's own claim); state is accepted or rejected.
func (a *API) settleNoShow(ctx context.Context, reportID, absent, state string, operator *string, resolution string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var bookingID, claimed, current string
	if err = tx.QueryRow(ctx, `SELECT booking_id::text,absent_role,state FROM no_show_reports WHERE id=$1 FOR UPDATE`, reportID).Scan(&bookingID, &claimed, &current); err != nil {
		return err
	}
	if current == "accepted" || current == "rejected" {
		return tx.Commit(ctx)
	}
	if absent == "" {
		absent = claimed
	}
	if _, err = tx.Exec(ctx, `UPDATE no_show_reports SET state=$2,resolution=$3,resolved_by=$4,resolved_at=now() WHERE id=$1`, reportID, state, resolution, operator); err != nil {
		return err
	}
	if operator != nil {
		if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'no_show.resolved',$2,$3,jsonb_build_object('outcome',$4::text,'absent',$5::text))`, *operator, reportID, resolution, state, absent); err != nil {
			return err
		}
	}
	b, err := store.New(tx).LockBookingForCancellation(ctx, bookingID)
	if err != nil {
		return err
	}
	if state == "accepted" && b.State == "confirmed" {
		if _, err = tx.Exec(ctx, `UPDATE bookings SET state=$2 WHERE id=$1`, bookingID, "no_show_"+absent); err != nil {
			return err
		}
		if err = store.New(tx).CancelUpcomingBookingMail(ctx, store.CancelUpcomingBookingMailParams{ReasonCode: "NO_SHOW", BookingID: bookingID}); err != nil {
			return err
		}
		if absent == "seller" {
			// The buyer paid for time they didn't get: full refund.
			if _, err = a.createRefund(ctx, tx, b, b.GrossMinor, "seller_no_show", operator); err != nil && !strings.Contains(err.Error(), "no verified payment") {
				return err
			}
		}
	}
	for _, recipient := range []struct{ user, kind string }{{b.BuyerUserID, "no_show_resolved_buyer"}, {b.SellerUserID, "no_show_resolved_seller"}} {
		if err = enqueueBookingEvent(ctx, tx, bookingID, recipient.user, recipient.kind, reportID+":no-show-resolved:"+recipient.kind, &reportID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (a *API) opsNoShows(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT ns.id::text,ns.booking_id::text,sp.handle,b.buyer_name,ns.absent_role,ns.state,COALESCE(ns.dispute_reason,''),ns.resolves_at,ns.created_at,b.starts_at FROM no_show_reports ns JOIN bookings b ON b.id=ns.booking_id JOIN seller_profiles sp ON sp.id=b.seller_id ORDER BY (ns.state='disputed') DESC, ns.created_at DESC LIMIT 200`)
	if err != nil {
		problem(w, 503, "NO_SHOWS_UNAVAILABLE", "No-show reports could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, booking, handle, buyer, absent, state, dispute string
		var resolves, created, starts time.Time
		if err = rows.Scan(&id, &booking, &handle, &buyer, &absent, &state, &dispute, &resolves, &created, &starts); err != nil {
			problem(w, 503, "NO_SHOWS_UNAVAILABLE", "No-show reports could not be read.")
			return
		}
		items = append(items, map[string]any{"id": id, "booking_id": booking, "seller": handle, "buyer_name": buyer, "absent_role": absent, "state": state, "dispute_reason": dispute, "resolves_at": resolves, "created_at": created, "starts_at": starts})
	}
	jsonOut(w, 200, map[string]any{"reports": items})
}

// opsResolveNoShow decides a disputed report after reviewing both sides.
func (a *API) opsResolveNoShow(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:booking:resolve")
	if !ok {
		return
	}
	var in struct {
		Outcome string `json:"outcome"` // buyer_absent, seller_absent, both_attended
		Reason  string `json:"reason"`
	}
	if decode(r, &in) != nil || len(strings.TrimSpace(in.Reason)) < 8 || len(in.Reason) > 500 {
		problem(w, 422, "REASON_REQUIRED", "Choose an outcome and record the evidence.")
		return
	}
	absent, state := "", ""
	switch in.Outcome {
	case "buyer_absent":
		absent, state = "buyer", "accepted"
	case "seller_absent":
		absent, state = "seller", "accepted"
	case "both_attended":
		state = "rejected"
	default:
		problem(w, 422, "INVALID_OUTCOME", "Choose buyer absent, seller absent or both attended.")
		return
	}
	var current string
	if err := a.db.QueryRow(r.Context(), `SELECT state FROM no_show_reports WHERE id=$1`, r.PathValue("id")).Scan(&current); err != nil {
		problem(w, 404, "NOT_FOUND", "This report was not found.")
		return
	}
	if current != "open" && current != "disputed" {
		problem(w, 409, "ALREADY_RESOLVED", "This report has already been resolved.")
		return
	}
	resolution := "Reviewed by WantMyTime: " + strings.TrimSpace(in.Reason)
	if err := a.settleNoShow(r.Context(), r.PathValue("id"), absent, state, &actor.ID, resolution); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The report could not be resolved.")
		return
	}
	jsonOut(w, 200, map[string]string{"state": state})
}
