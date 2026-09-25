package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Four notifications, and only these: a seller's new booking, a buyer's
// problem report (the seller has a deadline), and "your call starts in 10
// minutes" for both people. Email stays the record of everything; a push
// is a nudge, so nothing depends on it arriving.

// pushConfig tells the web app whether push is on and which key to use.
func (a *API) pushConfig(w http.ResponseWriter, r *http.Request) {
	if a.vapid == nil {
		jsonOut(w, 200, map[string]any{"enabled": false})
		return
	}
	jsonOut(w, 200, map[string]any{"enabled": true, "public_key": b64.EncodeToString(a.vapid.public)})
}

type pushSubscriptionInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (in pushSubscriptionInput) valid() bool {
	if len(in.Endpoint) > 1000 || !allowedPushHost(in.Endpoint) {
		return false
	}
	key, err := b64.DecodeString(strings.TrimRight(in.Keys.P256dh, "="))
	if err != nil || len(key) != 65 || key[0] != 4 {
		return false
	}
	auth, err := b64.DecodeString(strings.TrimRight(in.Keys.Auth, "="))
	return err == nil && len(auth) == 16
}

// savePushSubscription stores this browser for the signed-in person (or the
// buyer on their booking link). Re-saving the same browser moves it to them.
func (a *API) savePushSubscription(w http.ResponseWriter, r *http.Request) {
	if a.vapid == nil {
		problem(w, 503, "PUSH_DISABLED", "Notifications are not switched on.")
		return
	}
	u, ok, _, _ := a.buyerActor(w, r)
	if !ok {
		return
	}
	var in pushSubscriptionInput
	if decode(r, &in) != nil || !in.valid() {
		problem(w, 422, "INVALID_SUBSCRIPTION", "This browser's notification details could not be read.")
		return
	}
	ctx := r.Context()
	if _, err := a.db.Exec(ctx, `INSERT INTO push_subscriptions(user_id,endpoint,p256dh,auth) VALUES($1,$2,$3,$4)
		ON CONFLICT (endpoint) DO UPDATE SET user_id=EXCLUDED.user_id,p256dh=EXCLUDED.p256dh,auth=EXCLUDED.auth,failures=0`, u.ID, in.Endpoint, in.Keys.P256dh, in.Keys.Auth); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Notifications could not be switched on.")
		return
	}
	// Keep the ten most recent browsers per person.
	_, _ = a.db.Exec(ctx, `DELETE FROM push_subscriptions WHERE user_id=$1 AND id NOT IN (SELECT id FROM push_subscriptions WHERE user_id=$1 ORDER BY created_at DESC LIMIT 10)`, u.ID)
	jsonOut(w, 200, map[string]bool{"enabled": true})
}

// deletePushSubscription switches notifications off for this browser.
func (a *API) deletePushSubscription(w http.ResponseWriter, r *http.Request) {
	u, ok, _, _ := a.buyerActor(w, r)
	if !ok {
		return
	}
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if decode(r, &in) != nil || in.Endpoint == "" {
		problem(w, 422, "INVALID_SUBSCRIPTION", "Which browser should stop getting notifications?")
		return
	}
	if _, err := a.db.Exec(r.Context(), `DELETE FROM push_subscriptions WHERE user_id=$1 AND endpoint=$2`, u.ID, in.Endpoint); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Notifications could not be switched off.")
		return
	}
	jsonOut(w, 200, map[string]bool{"enabled": false})
}

// enqueuePush queues one notification if the person has a browser to
// receive it.
func enqueuePush(ctx context.Context, tx pgx.Tx, bookingID, userID, kind, eventKey string, due time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO push_outbox(event_key,user_id,booking_id,kind,due_at)
		SELECT $1,$2,$3,$4,$5 WHERE EXISTS (SELECT 1 FROM push_subscriptions WHERE user_id=$2)
		ON CONFLICT (event_key) DO NOTHING`, eventKey, userID, bookingID, kind, due)
	return err
}

func (a *API) runPushWorker(ctx context.Context) {
	for {
		err := a.processPushes(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			a.log().ErrorContext(ctx, "push worker cycle failed", "error", err.Error())
		}
		a.beat(ctx, "push", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

type pushJob struct {
	id, userID, bookingID, kind string
	attempts                    int
}

func (a *API) processPushes(ctx context.Context) error {
	// Calls starting within the next 10 minutes (a few seconds of slack for
	// the worker's cycle). The key carries the start time, so a rescheduled
	// call gets a fresh reminder.
	if _, err := a.db.Exec(ctx, `INSERT INTO push_outbox(event_key,user_id,booking_id,kind,due_at)
		SELECT b.id::text||':'||extract(epoch FROM b.starts_at)::bigint||':'||r.kind, r.uid, b.id, r.kind, now()
		FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id
		CROSS JOIN LATERAL (VALUES (sp.user_id,'call_soon_seller'),(b.buyer_user_id,'call_soon_buyer')) AS r(uid,kind)
		WHERE b.state='confirmed' AND b.starts_at > now() AND b.starts_at <= now()+interval '10 minutes 30 seconds'
		  AND r.uid IS NOT NULL AND EXISTS (SELECT 1 FROM push_subscriptions ps WHERE ps.user_id=r.uid)
		ON CONFLICT (event_key) DO NOTHING`); err != nil {
		return err
	}
	rows, err := a.db.Query(ctx, `UPDATE push_outbox SET attempts=attempts+1,due_at=now()+interval '2 minutes'
		WHERE id IN (SELECT id FROM push_outbox WHERE state='queued' AND due_at<=now() ORDER BY due_at LIMIT 50 FOR UPDATE SKIP LOCKED)
		RETURNING id::text,user_id::text,booking_id::text,kind,attempts`)
	if err != nil {
		return err
	}
	var jobs []pushJob
	for rows.Next() {
		var j pushJob
		if err = rows.Scan(&j.id, &j.userID, &j.bookingID, &j.kind, &j.attempts); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	for _, j := range jobs {
		state := a.deliverPush(ctx, client, j)
		if state == "queued" && j.attempts >= 3 {
			state = "failed"
		}
		if state == "queued" {
			_, err = a.db.Exec(ctx, `UPDATE push_outbox SET due_at=now()+interval '1 minute' WHERE id=$1`, j.id)
		} else {
			_, err = a.db.Exec(ctx, `UPDATE push_outbox SET state=$2,sent_at=CASE WHEN $2='sent' THEN now() END WHERE id=$1`, j.id, state)
		}
		if err != nil {
			return err
		}
	}
	_, _ = a.db.Exec(ctx, `DELETE FROM push_outbox WHERE state<>'queued' AND created_at<now()-interval '30 days'`)
	_, _ = a.db.Exec(ctx, `DELETE FROM push_subscriptions WHERE failures>=20`)
	return nil
}

// pushMessage is what the service worker shows.
type pushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

// deliverPush sends one job to every browser of its recipient and returns
// the job's next state: sent, skipped (no longer relevant), or queued (try
// again).
func (a *API) deliverPush(ctx context.Context, client *http.Client, j pushJob) string {
	if a.vapid == nil {
		return "skipped"
	}
	var state, buyer, sellerName, zone string
	var starts time.Time
	var duration int
	var problemWaiting bool
	err := a.db.QueryRow(ctx, `SELECT b.state,b.buyer_name,owner.display_name,b.starts_at,b.duration_minutes,recipient.timezone,
		(b.issue_reason IS NOT NULL AND b.issue_resolved_at IS NULL AND b.issue_reported_by='buyer' AND b.issue_seller_response IS NULL)
		FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id JOIN users owner ON owner.id=sp.user_id JOIN users recipient ON recipient.id=$2
		WHERE b.id=$1`, j.bookingID, j.userID).Scan(&state, &buyer, &sellerName, &starts, &duration, &zone, &problemWaiting)
	if errors.Is(err, pgx.ErrNoRows) {
		return "skipped"
	}
	if err != nil {
		return "queued"
	}
	sellerURL, buyerURL := appOrigin()+"/app/bookings/"+j.bookingID, appOrigin()+"/booking/"+j.bookingID
	var msg pushMessage
	ttl := 24 * time.Hour
	switch j.kind {
	case "new_booking_seller":
		if state != "confirmed" || !starts.After(time.Now()) {
			return "skipped"
		}
		msg = pushMessage{Title: "New booking: " + buyer, Body: fmt.Sprintf("%d minutes, %s", duration, formatShort(starts, zoneOr(zone, time.UTC))), URL: sellerURL, Tag: "booking-" + j.bookingID}
	case "problem_reported_seller":
		if !problemWaiting {
			return "skipped"
		}
		msg = pushMessage{Title: buyer + " reported a problem", Body: "Answer within " + humanDuration(problemResponseWindow()) + ", or they're refunded in full.", URL: sellerURL, Tag: "problem-" + j.bookingID}
	case "call_soon_seller", "call_soon_buyer":
		left := time.Until(starts)
		if state != "confirmed" || left < -5*time.Minute {
			return "skipped"
		}
		other, url := buyer, sellerURL
		if j.kind == "call_soon_buyer" {
			other, url = sellerName, buyerURL
		}
		title := "Your call with " + other + " has started"
		if minutes := int((left + time.Minute - time.Second) / time.Minute); minutes >= 1 {
			title = fmt.Sprintf("Your call with %s starts in %d minute%s", other, minutes, map[bool]string{true: "", false: "s"}[minutes == 1])
		}
		msg = pushMessage{Title: title, Body: "Tap to open the booking and join.", URL: url, Tag: "call-" + j.bookingID}
		ttl = 15 * time.Minute
	default:
		return "skipped"
	}
	payload, _ := json.Marshal(msg)
	subs, err := a.db.Query(ctx, `SELECT id::text,endpoint,p256dh,auth FROM push_subscriptions WHERE user_id=$1`, j.userID)
	if err != nil {
		return "queued"
	}
	type sub struct{ id, endpoint, p256dh, auth string }
	var list []sub
	for subs.Next() {
		var s sub
		if subs.Scan(&s.id, &s.endpoint, &s.p256dh, &s.auth) == nil {
			list = append(list, s)
		}
	}
	subs.Close()
	if len(list) == 0 {
		return "skipped"
	}
	delivered, retry := false, false
	for _, s := range list {
		sendErr := a.vapid.sendPush(ctx, client, s.endpoint, s.p256dh, s.auth, payload, ttl)
		switch {
		case sendErr == nil:
			delivered = true
			_, _ = a.db.Exec(ctx, `UPDATE push_subscriptions SET last_success_at=now(),failures=0 WHERE id=$1`, s.id)
		case errors.Is(sendErr, errPushGone):
			_, _ = a.db.Exec(ctx, `DELETE FROM push_subscriptions WHERE id=$1`, s.id)
		default:
			retry = true
			_, _ = a.db.Exec(ctx, `UPDATE push_subscriptions SET failures=failures+1 WHERE id=$1`, s.id)
			a.log().WarnContext(ctx, "push delivery failed", "kind", j.kind, "error", sendErr.Error())
		}
	}
	switch {
	case delivered:
		return "sent"
	case retry:
		return "queued"
	default:
		return "skipped"
	}
}
