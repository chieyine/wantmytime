package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Payments that cannot become a booking.
//
// A buyer can pay and still have no booking: a slow bank transfer lands after
// the time hold has lapsed and someone else has taken the time, the buyer pays
// twice (starts a transfer, then pays with bank), the seller stops taking
// bookings in between, or an agreed offer changes. None of that is the
// buyer's fault, so the money goes back automatically and the buyer is told
// by email. The exception stays on record in Operations.

// unbookedRefundKinds are the exceptions whose money is returned in full.
var unbookedRefundKinds = map[string]bool{"payment_without_slot": true, "duplicate_charge": true, "offer_conflict": true, "wrong_amount": true, "wrong_currency": true}

// reclaimLateSlot tries to give a late payment its time after all: the
// quote's hold is reactivated if the time is still in the future, still
// inside the seller's hours and not taken by anyone else. It runs inside the
// caller's transaction and leaves nothing behind when it fails.
func reclaimLateSlot(ctx context.Context, tx pgx.Tx, quoteID, sellerID string, starts time.Time, duration int) (bool, error) {
	if !starts.After(time.Now().Add(5 * time.Minute)) {
		return false, nil
	}
	var zone string
	var buffer int
	if err := tx.QueryRow(ctx, `SELECT timezone,buffer_minutes FROM seller_profiles WHERE id=$1`, sellerID).Scan(&zone, &buffer); err != nil {
		return false, err
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return false, nil
	}
	if !validScheduledTime(ctx, tx, sellerID, zone, starts.In(location), starts, duration, buffer, nil) {
		return false, nil
	}
	// A savepoint, so a time someone else holds (an exclusion violation)
	// does not abort the caller's transaction.
	sp, err := tx.Begin(ctx)
	if err != nil {
		return false, err
	}
	until := time.Now().Add(10 * time.Minute)
	tag, err := sp.Exec(ctx, `UPDATE slot_reservations SET active=true,expires_at=$2 WHERE quote_id=$1 AND reservation_kind='hold'`, quoteID, until)
	if err == nil && tag.RowsAffected() == 0 {
		_, err = sp.Exec(ctx, `INSERT INTO slot_reservations(id,seller_id,occupied_from,occupied_to,active,reservation_kind,quote_id,expires_at) VALUES(gen_random_uuid(),$1,$2,$3,true,'hold',$4,$5)`, sellerID, starts, starts.Add(time.Duration(duration+buffer)*time.Minute), quoteID, until)
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		if pgErrCode(err) == "23P01" {
			return false, nil
		}
		return false, err
	}
	if _, err = sp.Exec(ctx, `UPDATE quotes SET state='held',expires_at=$2 WHERE id=$1 AND state IN ('held','expired')`, quoteID, until); err != nil {
		_ = sp.Rollback(ctx)
		return false, err
	}
	if err = sp.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// returnUnbookedPayment starts the refund of a payment that became an
// exception, and queues the email that tells the buyer. With automatic
// refunds off, the refund waits in Operations for approval.
func (a *API) returnUnbookedPayment(ctx context.Context, tx pgx.Tx, attemptID, quoteID, kind string, amount int64, currency string) error {
	if !unbookedRefundKinds[kind] || amount <= 0 {
		return nil
	}
	var exceptionID, buyerID string
	err := tx.QueryRow(ctx, `SELECT pe.id::text,q.buyer_user_id::text FROM payment_exceptions pe JOIN payment_attempts pa ON pa.id=pe.payment_attempt_id JOIN quotes q ON q.id=pa.quote_id WHERE pe.payment_attempt_id=$1 AND pe.kind=$2`, attemptID, kind).Scan(&exceptionID, &buyerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	state := "pending_approval"
	if a.refundsEnabled() {
		state = "queued"
	}
	note := "Automatic refund of a payment that could not become a booking."
	var refundID string
	err = tx.QueryRow(ctx, `INSERT INTO refunds(booking_id,payment_exception_id,payment_attempt_id,amount_minor,currency,platform_share_minor,seller_share_minor,reason,state,note,seller_liability)
		VALUES(NULL,$1,$2,$3,$4,$3,0,'unbooked_payment',$5,$6,'payable')
		ON CONFLICT (payment_exception_id) WHERE payment_exception_id IS NOT NULL AND state <> 'failed' DO NOTHING RETURNING id::text`,
		exceptionID, attemptID, amount, strings.TrimSpace(currency), state, note).Scan(&refundID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already being refunded
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE payment_exceptions SET state='awaiting_provider',resolution=$2 WHERE id=$1 AND state='open'`, exceptionID, "Refund "+refundID+" started automatically."); err != nil {
		return err
	}
	return enqueueExceptionEmail(ctx, tx, exceptionID, buyerID, "payment_returning_buyer")
}

func enqueueExceptionEmail(ctx context.Context, tx pgx.Tx, exceptionID, recipientID, kind string) error {
	_, err := tx.Exec(ctx, `INSERT INTO notification_outbox(id,event_key,payment_exception_id,recipient_user_id,kind,due_at) VALUES(gen_random_uuid(),$1,$2,$3,$4,now()) ON CONFLICT (event_key) DO NOTHING`,
		exceptionID+":"+kind, exceptionID, recipientID, kind)
	return err
}

// finalizeExceptionRefund records a completed refund of an unbooked payment:
// the exception is resolved and the buyer is emailed. The original charge
// never reached the booking ledger, so there is nothing to reverse there.
func (a *API) finalizeExceptionRefund(ctx context.Context, refundID string, operator, note *string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var state, exceptionID, buyerID string
	if err = tx.QueryRow(ctx, `SELECT r.state,r.payment_exception_id::text,q.buyer_user_id::text FROM refunds r JOIN payment_attempts pa ON pa.id=r.payment_attempt_id JOIN quotes q ON q.id=pa.quote_id WHERE r.id=$1 FOR UPDATE OF r`, refundID).Scan(&state, &exceptionID, &buyerID); err != nil {
		return err
	}
	if state == "processed" {
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE refunds SET state='processed',processed_at=now(),note=COALESCE($2,note),approved_by=COALESCE($3::uuid,approved_by),updated_at=now() WHERE id=$1`, refundID, note, operator); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE payment_exceptions SET state='resolved',resolution=$2,resolved_at=now() WHERE id=$1 AND state<>'resolved'`, exceptionID, "Refunded to the buyer in full (refund "+refundID+")."); err != nil {
		return err
	}
	if err = enqueueExceptionEmail(ctx, tx, exceptionID, buyerID, "payment_returned_buyer"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// exceptionEmail builds the email about a payment that did not become a booking.
func (a *API) exceptionEmail(ctx context.Context, jobID string) (emailContent, string, string) {
	var kind, exceptionKind, currency, email, recipientZone, sellerName, handle string
	var amount int64
	var starts time.Time
	var duration int
	err := a.db.QueryRow(ctx, `SELECT n.kind,pe.kind,COALESCE(pe.amount_minor,0),COALESCE(pe.currency,pa.currency),identity.normalized_identifier,recipient.timezone,owner.display_name,sp.handle,q.starts_at,q.duration_minutes
		FROM notification_outbox n
		JOIN payment_exceptions pe ON pe.id=n.payment_exception_id
		JOIN payment_attempts pa ON pa.id=pe.payment_attempt_id
		JOIN quotes q ON q.id=pa.quote_id
		JOIN seller_profiles sp ON sp.id=q.seller_id
		JOIN users owner ON owner.id=sp.user_id
		JOIN users recipient ON recipient.id=n.recipient_user_id AND recipient.status='active'
		JOIN user_identities identity ON identity.user_id=recipient.id AND identity.type='email'
		WHERE n.id=$1 AND n.state='processing' LIMIT 1`, jobID).Scan(&kind, &exceptionKind, &amount, &currency, &email, &recipientZone, &sellerName, &handle, &starts, &duration)
	if errors.Is(err, pgx.ErrNoRows) {
		return emailContent{}, "", "RECIPIENT_UNAVAILABLE"
	}
	if err != nil {
		return emailContent{}, "", "DATABASE_ERROR"
	}
	own := zoneOr(recipientZone, time.UTC)
	money := formatMoney(currency, amount)
	why := map[string]string{
		"payment_without_slot": "Your payment arrived after the time you picked had been taken, so we couldn’t book it.",
		"duplicate_charge":     "You paid twice for the same booking. Your booking stands; this second payment isn’t needed.",
		"offer_conflict":       "The offer this payment was for had already closed, so we couldn’t book it.",
		"wrong_amount":         "The amount that arrived didn’t match the booking, so we couldn’t book it with this payment.",
		"wrong_currency":       "This payment arrived in a different currency from the booking, so we couldn’t book it with it.",
	}[exceptionKind]
	if why == "" {
		why = "We couldn’t turn this payment into a booking."
	}
	c := emailContent{Facts: []emailFact{{"Amount", money}, {"Booking", fmt.Sprintf("%d minutes with %s, %s", duration, sellerName, formatShort(starts, own))}}}
	switch kind {
	case "payment_returning_buyer":
		c.Subject = "We’re returning your payment of " + money
		c.Heading = "We’re sending your money back."
		c.Paragraphs = []string{why, "The full amount is on its way back to the account you paid from. We’ll email you again when it has been sent."}
		if exceptionKind != "duplicate_charge" {
			c.Action = &emailLink{"Pick another time", appOrigin() + "/" + handle}
		}
	case "payment_returned_buyer":
		c.Subject = "Your refund of " + money + " has been sent"
		c.Heading = "Your refund has been sent."
		c.Paragraphs = []string{"We’ve sent " + money + " back to the account you paid from. Banks can take up to 10 working days to show it."}
	default:
		return c, "", "UNKNOWN_NOTIFICATION"
	}
	return c, email, ""
}
