package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

// Operations run themselves. A buyer's problem report goes to the seller,
// who refunds or disagrees; silence refunds the buyer. Failed refunds,
// payment events, emails and payouts are retried on a schedule. Only a
// genuine disagreement (a disputed problem or no-show) waits for a person.

// problemResponseWindow is how long a seller has to answer a buyer's
// problem report before the buyer is refunded in full (default 24 hours).
func problemResponseWindow() time.Duration {
	hours, err := strconv.Atoi(os.Getenv("PROBLEM_RESPONSE_HOURS"))
	if err != nil || hours < 1 || hours > 24*7 {
		hours = 24
	}
	return time.Duration(hours) * time.Hour
}

// respondToProblem lets the seller answer a buyer's problem report: refund
// in full, refund part, or disagree (which sends it to WantMyTime).
func (a *API) respondToProblem(w http.ResponseWriter, r *http.Request) {
	u, ok, guest, _ := a.buyerActor(w, r)
	if !ok {
		return
	}
	if guest {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	var in struct {
		Action      string `json:"action"`
		AmountMinor int64  `json:"amount_minor"`
		Note        string `json:"note"`
	}
	if decode(r, &in) != nil || (in.Action != "refund_full" && in.Action != "refund_partial" && in.Action != "disagree") {
		problem(w, 422, "INVALID_RESPONSE", "Choose to refund in full, refund part, or disagree.")
		return
	}
	note := strings.TrimSpace(in.Note)
	if in.Action == "disagree" && (len(note) < 8 || len(note) > 1000) {
		problem(w, 422, "NOTE_REQUIRED", "Say what happened, in a sentence or two.")
		return
	}
	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your answer could not be saved.")
		return
	}
	defer tx.Rollback(ctx)
	b, err := store.New(tx).LockBookingForCancellation(ctx, r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && b.SellerUserID != u.ID) {
		problem(w, 404, "NOT_FOUND", "This booking is not available.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your answer could not be saved.")
		return
	}
	var open bool
	if err = tx.QueryRow(ctx, `SELECT issue_reason IS NOT NULL AND issue_resolved_at IS NULL AND issue_reported_by='buyer' AND issue_seller_response IS NULL FROM bookings WHERE id=$1`, b.BookingID).Scan(&open); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your answer could not be saved.")
		return
	}
	if !open {
		problem(w, 409, "NO_OPEN_PROBLEM", "There is no problem report waiting for your answer on this booking.")
		return
	}
	if in.Action == "disagree" {
		if _, err = tx.Exec(ctx, `UPDATE bookings SET issue_seller_response='disagree',issue_seller_note=$2,issue_disputed_at=now() WHERE id=$1`, b.BookingID, note); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Your answer could not be saved.")
			return
		}
		if err = enqueueBookingEvent(ctx, tx, b.BookingID, b.BuyerUserID, "problem_disputed_buyer", b.BookingID+":problem-disputed", nil); err != nil {
			problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "Your answer could not be saved.")
			return
		}
		if err = tx.Commit(ctx); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Your answer could not be saved.")
			return
		}
		jsonOut(w, 200, map[string]string{"state": "disputed", "message": "Thanks. WantMyTime will look at both sides and email you both the outcome. Your payout for this booking waits until then."})
		return
	}
	amount := b.GrossMinor
	if in.Action == "refund_partial" {
		if in.AmountMinor <= 0 || in.AmountMinor >= b.GrossMinor {
			problem(w, 422, "INVALID_AMOUNT", "A partial refund must be more than nothing and less than the full price.")
			return
		}
		amount = in.AmountMinor
	}
	resolution := "The seller agreed to refund " + formatMoney(b.Currency, amount) + "."
	if in.Action == "refund_full" {
		resolution = "The seller agreed to refund the price in full (" + formatMoney(b.Currency, amount) + ")."
	}
	if note != "" {
		resolution += " Their note: " + note
	}
	refundID, err := a.settleProblem(ctx, tx, b, in.Action, resolution, amount, &u.ID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your answer could not be saved.")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your answer could not be saved.")
		return
	}
	jsonOut(w, 200, map[string]any{"state": "resolved", "refund_id": refundID, "message": "Done. The buyer is refunded " + formatMoney(b.Currency, amount) + " and the rest of your payout is sent shortly."})
}

// settleProblem closes a buyer's problem report with a refund and tells both
// people. A booking without a verified payment (a local test) is closed
// without one.
func (a *API) settleProblem(ctx context.Context, tx pgx.Tx, b store.LockBookingForCancellationRow, response, resolution string, amount int64, actor *string) (*string, error) {
	tag, err := tx.Exec(ctx, `UPDATE bookings SET issue_seller_response=$2,issue_resolved_at=now(),issue_resolution=$3 WHERE id=$1 AND issue_reason IS NOT NULL AND issue_resolved_at IS NULL`, b.BookingID, response, resolution)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, errors.New("problem is no longer open")
	}
	var refundID *string
	var refundExists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM refunds WHERE booking_id=$1 AND state<>'failed')`, b.BookingID).Scan(&refundExists); err != nil {
		return nil, err
	}
	if amount > 0 && !refundExists {
		id, refundErr := a.createRefund(ctx, tx, b, amount, "problem_upheld", actor)
		switch {
		case refundErr != nil && strings.Contains(refundErr.Error(), "no verified payment"):
		case refundErr != nil:
			return nil, refundErr
		case id != "":
			refundID = &id
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'booking.issue_resolved',$2,$3,jsonb_build_object('refund_minor',$4::bigint,'response',$5::text))`, actor, b.BookingID, resolution, amount, response); err != nil {
		return nil, err
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	for _, recipient := range []struct{ user, kind string }{{b.BuyerUserID, "problem_resolved_buyer"}, {b.SellerUserID, "problem_resolved_seller"}} {
		if err = enqueueBookingEvent(ctx, tx, b.BookingID, recipient.user, recipient.kind, b.BookingID+":problem-resolved:"+stamp+":"+recipient.kind, refundID); err != nil {
			return nil, err
		}
	}
	return refundID, nil
}

// resolveUnansweredProblems refunds the buyer in full when the seller has
// not answered a problem report in time.
func (a *API) resolveUnansweredProblems(ctx context.Context) error {
	window := int(problemResponseWindow() / time.Minute)
	rows, err := a.db.Query(ctx, `SELECT id::text FROM bookings WHERE issue_reason IS NOT NULL AND issue_resolved_at IS NULL AND issue_reported_by='buyer' AND issue_seller_response IS NULL
		AND COALESCE(issue_respond_by, issue_created_at + make_interval(mins => $1)) < now() ORDER BY issue_created_at LIMIT 20`, window)
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
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		tx, txErr := a.db.Begin(ctx)
		if txErr != nil {
			return txErr
		}
		b, lockErr := store.New(tx).LockBookingForCancellation(ctx, id)
		if lockErr == nil {
			resolution := "The seller did not answer within " + humanDuration(problemResponseWindow()) + ", so the buyer has been refunded the price in full."
			_, lockErr = a.settleProblem(ctx, tx, b, "no_response", resolution, b.GrossMinor, nil)
		}
		if lockErr != nil {
			_ = tx.Rollback(ctx)
			a.log().ErrorContext(ctx, "unanswered problem could not be settled", "booking_id", id, "error", lockErr.Error())
			continue
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// autoRetryLadder is how long after a failure each automatic retry waits.
var autoRetryLadder = []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour}

// ladderSQL is the wait before retry number auto_retries+1, in seconds.
func ladderSQL(column string) string {
	return `(CASE ` + column + ` WHEN 0 THEN ` + strconv.Itoa(int(autoRetryLadder[0].Seconds())) + ` WHEN 1 THEN ` + strconv.Itoa(int(autoRetryLadder[1].Seconds())) + ` ELSE ` + strconv.Itoa(int(autoRetryLadder[2].Seconds())) + ` END)`
}

// autoRetryFailures puts failed refunds (never accepted by Kora), payment
// events and emails back in their queues, up to three times each. Only what
// still fails after that raises an alert.
func (a *API) autoRetryFailures(ctx context.Context) error {
	max := len(autoRetryLadder)
	statements := []string{
		`UPDATE refunds SET state='queued',attempts=0,last_error=NULL,next_attempt_at=now(),updated_at=now(),auto_retries=auto_retries+1
		 WHERE state='failed' AND provider_refund_id IS NULL AND payment_attempt_id IS NOT NULL AND auto_retries<$1 AND updated_at < now() - make_interval(secs => ` + ladderSQL("auto_retries") + `)`,
		`UPDATE provider_events SET state='queued',processing_attempts=0,last_error_code=NULL,next_attempt_at=now(),claimed_at=NULL,auto_retries=auto_retries+1
		 WHERE state='failed' AND auto_retries<$1 AND COALESCE(next_attempt_at,received_at) < now() - make_interval(secs => ` + ladderSQL("auto_retries") + `)`,
		`UPDATE notification_outbox SET state='queued',attempts=0,last_error_code=NULL,claimed_at=NULL,due_at=now(),auto_retries=auto_retries+1
		 WHERE state='failed' AND auto_retries<$1 AND due_at < now() - make_interval(secs => ` + ladderSQL("auto_retries") + `)`,
	}
	for _, sql := range statements {
		if _, err := a.db.Exec(ctx, sql, max); err != nil {
			return err
		}
	}
	return nil
}
