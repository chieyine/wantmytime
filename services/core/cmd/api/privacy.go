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

// Data rights under the Nigeria Data Protection Act 2023 (and GDPR-style
// rights for people elsewhere): a person can download everything WantMyTime
// holds about them, and can delete their account. Deletion erases identifying
// details but keeps the payment, payout, refund and ledger records the law
// requires (with the person's name removed), so books still balance.

const (
	exportsPerDay     = 3
	handleHoldPeriod  = 180 * 24 * time.Hour
	deletedPersonName = "Deleted user"
)

// exportSections are read inside one repeatable-read transaction, so the file
// is a consistent snapshot. Each query returns one JSON value; $1 is the user
// ID, $2 their seller profile ID (or NULL) and $3 their analytics subject hash.
var exportSections = []struct{ name, sql string }{
	{"account", `SELECT to_jsonb(t) FROM (SELECT u.id,i.normalized_identifier AS email,u.display_name,u.timezone,u.status,u.created_at FROM users u LEFT JOIN user_identities i ON i.user_id=u.id AND i.type='email' WHERE u.id=$1) t`},
	{"sessions", `SELECT COALESCE(jsonb_agg(t ORDER BY t.created_at DESC),'[]') FROM (SELECT created_at,expires_at,revoked_at,CASE WHEN guest_scope IS NULL THEN 'account' ELSE 'booking access' END AS kind FROM sessions WHERE user_id=$1) t`},
	{"seller_profile", `SELECT to_jsonb(t) FROM (SELECT handle,mode,publication_state,timezone,identity_url,minimum_notice_minutes,booking_horizon_days,buffer_minutes,paused,cancellation_policy,created_at,(avatar_mime IS NOT NULL) AS has_photo FROM seller_profiles WHERE id=$2) t`},
	{"prices", `SELECT COALESCE(jsonb_agg(t ORDER BY t.created_at),'[]') FROM (SELECT currency,base_30_minor,durations,fee_basis_points,created_at FROM pricing_versions WHERE seller_id=$2) t`},
	{"availability_weekly", `SELECT COALESCE(jsonb_agg(t ORDER BY t.weekday,t.local_start),'[]') FROM (SELECT weekday,local_start,local_end,timezone FROM availability_windows WHERE seller_id=$2) t`},
	{"availability_changes", `SELECT COALESCE(jsonb_agg(t ORDER BY t.local_date),'[]') FROM (SELECT local_date,closed,replacement_windows FROM availability_overrides WHERE seller_id=$2) t`},
	{"calendar_connection", `SELECT to_jsonb(t) FROM (SELECT provider,account_email,check_busy,add_events,create_meet_links,status,connected_at,last_synced_at FROM calendar_connections WHERE seller_id=$2) t`},
	{"payout_account", `SELECT to_jsonb(t) FROM (SELECT country,bank_name,account_name,account_last4,currency,verified_at,updated_at FROM seller_payout_accounts WHERE seller_id=$2) t`},
	{"notification_devices", `SELECT COALESCE(jsonb_agg(t ORDER BY t.created_at),'[]') FROM (SELECT created_at,last_success_at,split_part(endpoint,'/',3) AS push_service FROM push_subscriptions WHERE user_id=$1) t`},
	{"bookings_you_made", `SELECT COALESCE(jsonb_agg(t ORDER BY t.starts_at DESC),'[]') FROM (SELECT b.id,sp.handle AS with_handle,b.buyer_name AS your_name,b.guest_email AS your_email,b.starts_at,b.duration_minutes,b.gross_minor,b.currency,b.state,b.payment_state,b.cancelled_at,b.cancellation_reason,b.issue_reason,b.created_at FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE b.buyer_user_id=$1) t`},
	{"bookings_you_hosted", `SELECT COALESCE(jsonb_agg(t ORDER BY t.starts_at DESC),'[]') FROM (SELECT id,buyer_name,starts_at,duration_minutes,gross_minor,currency,state,payment_state,cancelled_at,cancellation_reason,issue_reason,created_at FROM bookings WHERE seller_id=$2) t`},
	{"offers", `SELECT COALESCE(jsonb_agg(t ORDER BY t.created_at DESC),'[]') FROM (SELECT o.id,CASE WHEN o.buyer_user_id=$1 THEN 'you asked' ELSE 'asked of you' END AS role,o.buyer_name,o.duration_minutes,o.state,o.created_at,(SELECT jsonb_agg(jsonb_build_object('amount_minor',v.amount_minor,'by',v.actor,'at',v.created_at) ORDER BY v.version) FROM offer_versions v WHERE v.offer_id=o.id) AS amounts FROM offers o WHERE o.buyer_user_id=$1 OR o.seller_id=$2) t`},
	{"payments", `SELECT COALESCE(jsonb_agg(t ORDER BY t.created_at DESC),'[]') FROM (SELECT pa.merchant_reference AS reference,pa.provider,pa.channel,pa.expected_minor,pa.paid_minor,pa.currency,pa.canonical_state AS state,pa.created_at FROM payment_attempts pa JOIN quotes q ON q.id=pa.quote_id WHERE q.buyer_user_id=$1) t`},
	{"refunds", `SELECT COALESCE(jsonb_agg(t ORDER BY t.created_at DESC),'[]') FROM (SELECT r.booking_id,r.amount_minor,r.currency,r.reason,r.state,r.created_at,r.processed_at FROM refunds r JOIN bookings b ON b.id=r.booking_id WHERE b.buyer_user_id=$1 OR b.seller_id=$2) t`},
	{"payouts", `SELECT COALESCE(jsonb_agg(t ORDER BY t.created_at DESC),'[]') FROM (SELECT booking_id,amount_minor,fee_minor,recovery_minor,currency,state,reference,bank_name,account_last4,paid_at,created_at FROM seller_payouts WHERE seller_id=$2) t`},
	{"reviews_you_wrote", `SELECT COALESCE(jsonb_agg(t ORDER BY t.created_at DESC),'[]') FROM (SELECT booking_id,reviewer_name,rating,body,seller_reply,hidden_at IS NOT NULL AS hidden,created_at FROM reviews WHERE buyer_user_id=$1) t`},
	{"reviews_you_received", `SELECT COALESCE(jsonb_agg(t ORDER BY t.created_at DESC),'[]') FROM (SELECT booking_id,reviewer_name,rating,body,seller_reply,replied_at,hidden_at IS NOT NULL AS hidden,created_at FROM reviews WHERE seller_id=$2) t`},
	{"no_show_reports", `SELECT COALESCE(jsonb_agg(t ORDER BY t.created_at DESC),'[]') FROM (SELECT n.booking_id,n.absent_role,n.state,n.dispute_reason,n.resolution,n.created_at,n.resolved_at FROM no_show_reports n JOIN bookings b ON b.id=n.booking_id WHERE b.buyer_user_id=$1 OR b.seller_id=$2) t`},
	{"emails_sent_to_you", `SELECT COALESCE(jsonb_agg(t ORDER BY t.created_at DESC),'[]') FROM (SELECT kind,state,created_at,sent_at FROM notification_outbox WHERE recipient_user_id=$1) t`},
	{"product_events", `SELECT COALESCE(jsonb_agg(t ORDER BY t.occurred_at DESC),'[]') FROM (SELECT event_name,occurred_at FROM product_events WHERE subject_hash=$3 OR seller_id=$2) t`},
	{"account_actions", `SELECT COALESCE(jsonb_agg(t ORDER BY t.created_at DESC),'[]') FROM (SELECT action,CASE WHEN actor_id=$1 THEN 'you' ELSE 'WantMyTime' END AS by,reason,created_at FROM audit_events WHERE actor_id=$1 OR target_id=$1) t`},
}

// Every section receives the same three parameters; declaring their types up
// front lets a section leave some of them unused.
const exportParams = `WITH _params AS (SELECT $1::uuid AS user_id, $2::uuid AS seller_id, $3::bytea AS subject) `

// dataExport serves a JSON file of everything held about the signed-in person.
func (a *API) dataExport(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var recent int
	if err := a.db.QueryRow(ctx, `SELECT count(*) FROM data_requests WHERE user_id=$1 AND kind='export' AND created_at>now()-interval '24 hours'`, u.ID).Scan(&recent); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your data could not be prepared.")
		return
	}
	if recent >= exportsPerDay {
		problem(w, 429, "EXPORT_LIMIT", fmt.Sprintf("You can download your data %d times a day. Try again tomorrow.", exportsPerDay))
		return
	}
	tx, err := a.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your data could not be prepared.")
		return
	}
	defer tx.Rollback(ctx)
	var sellerID *string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM seller_profiles WHERE user_id=$1`, u.ID).Scan(&sellerID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "Your data could not be prepared.")
		return
	}
	out := map[string]any{
		"about":        "Everything WantMyTime holds about you, as of generated_at. Amounts are in minor units (kobo for NGN). Full bank account numbers, sign-in codes and security keys are never included.",
		"generated_at": time.Now().UTC(),
		"format":       1,
	}
	for _, s := range exportSections {
		var raw json.RawMessage
		if err = tx.QueryRow(ctx, exportParams+s.sql, u.ID, sellerID, analyticsSubjectHash(u.ID)).Scan(&raw); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			a.log().ErrorContext(ctx, "data export section failed", "section", s.name, "error", err.Error())
			problem(w, 503, "DATABASE_ERROR", "Your data could not be prepared.")
			return
		}
		if raw == nil {
			raw = json.RawMessage("null")
		}
		out[s.name] = raw
	}
	_ = tx.Rollback(ctx)
	if _, err = a.db.Exec(ctx, `INSERT INTO data_requests(id,user_id,kind) VALUES(gen_random_uuid(),$1,'export')`, u.ID); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your data could not be prepared.")
		return
	}
	body, _ := json.MarshalIndent(out, "", "  ")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="wantmytime-data-%s.json"`, time.Now().UTC().Format("2006-01-02")))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}

// deletionBlockers lists what must finish before an account can be deleted:
// money still moving, or sessions someone else is relying on.
func deletionBlockers(ctx context.Context, q pgx.Tx, userID string, sellerID *string, window time.Duration) ([]string, error) {
	checks := []struct{ message, sql string }{
		{"You hold operations access. Ask another operator to remove it first.", `SELECT EXISTS(SELECT 1 FROM admin_grants WHERE user_id=$1 AND revoked_at IS NULL)`},
		{"You have a booked session that hasn’t finished, or whose time to report a problem hasn’t passed. Cancel it or wait until it’s over.", `SELECT EXISTS(SELECT 1 FROM bookings WHERE (buyer_user_id=$1 OR seller_id=$2) AND state='confirmed' AND starts_at+make_interval(mins=>duration_minutes)+make_interval(secs=>$3)>now())`},
		{"A payment to you is still on its way. Wait until it has been paid.", `SELECT EXISTS(SELECT 1 FROM seller_payouts WHERE seller_id=$2 AND state IN ('scheduled','processing','failed'))`},
		{"A refund is still being processed.", `SELECT EXISTS(SELECT 1 FROM refunds r JOIN bookings b ON b.id=r.booking_id WHERE (b.buyer_user_id=$1 OR b.seller_id=$2) AND r.state IN ('pending_approval','queued','submitted','failed'))`},
		{"You owe WantMyTime for a refund that hasn’t been recovered yet.", `SELECT EXISTS(SELECT 1 FROM seller_recoveries WHERE seller_id=$2 AND recovered_minor<amount_minor)`},
		{"A reported problem or no-show is still being reviewed.", `SELECT EXISTS(SELECT 1 FROM bookings b WHERE (b.buyer_user_id=$1 OR b.seller_id=$2) AND ((b.issue_created_at IS NOT NULL AND b.issue_resolved_at IS NULL) OR EXISTS(SELECT 1 FROM no_show_reports n WHERE n.booking_id=b.id AND n.state IN ('open','disputed')) OR EXISTS(SELECT 1 FROM booking_cancellation_requests c WHERE c.booking_id=b.id AND c.state='open') OR EXISTS(SELECT 1 FROM payment_exceptions e WHERE e.booking_id=b.id AND e.state<>'resolved')))`},
	}
	var blockers []string
	for _, c := range checks {
		var blocked bool
		if err := q.QueryRow(ctx, `WITH _params AS (SELECT $1::uuid, $2::uuid, $3::float8) `+c.sql, userID, sellerID, window.Seconds()).Scan(&blocked); err != nil {
			return nil, err
		}
		if blocked {
			blockers = append(blockers, c.message)
		}
	}
	return blockers, nil
}

// deletionCheck tells the settings page whether deletion is possible now.
func (a *API) deletionCheck(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	tx, err := a.db.BeginTx(r.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your account could not be checked.")
		return
	}
	defer tx.Rollback(r.Context())
	var sellerID *string
	if err = tx.QueryRow(r.Context(), `SELECT id::text FROM seller_profiles WHERE user_id=$1`, u.ID).Scan(&sellerID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "Your account could not be checked.")
		return
	}
	blockers, err := deletionBlockers(r.Context(), tx, u.ID, sellerID, disputeWindow())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your account could not be checked.")
		return
	}
	if blockers == nil {
		blockers = []string{}
	}
	jsonOut(w, 200, map[string]any{"can_delete": len(blockers) == 0, "blockers": blockers, "email": u.Email})
}

// deleteAccount erases a person's identifying details. Records that must be
// kept for accounting and anti-fraud law stay, without their name or email.
func (a *API) deleteAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var in struct {
		ConfirmEmail string `json:"confirm_email"`
	}
	if decode(r, &in) != nil || !strings.EqualFold(strings.TrimSpace(in.ConfirmEmail), u.Email) {
		problem(w, 422, "CONFIRMATION_REQUIRED", "Type your email address exactly to confirm.")
		return
	}
	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your account could not be deleted.")
		return
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, u.ID); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your account could not be deleted.")
		return
	}
	var sellerID *string
	var handle, avatarKey string
	if err = tx.QueryRow(ctx, `SELECT id::text,handle,COALESCE(avatar_key,'') FROM seller_profiles WHERE user_id=$1 FOR UPDATE`, u.ID).Scan(&sellerID, &handle, &avatarKey); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "Your account could not be deleted.")
		return
	}
	blockers, err := deletionBlockers(ctx, tx, u.ID, sellerID, disputeWindow())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your account could not be deleted.")
		return
	}
	if len(blockers) > 0 {
		jsonOut(w, 409, map[string]any{"error": map[string]string{"code": "ACCOUNT_HAS_OPEN_ITEMS", "message": blockers[0]}, "blockers": blockers})
		return
	}
	steps := []struct{ sql string }{
		// Anything not yet sent or agreed is withdrawn.
		{`UPDATE offers SET state='expired' WHERE (buyer_user_id=$1 OR seller_id=$2) AND state IN ('pending','countered','agreed')`},
		{`UPDATE notification_outbox SET state='cancelled' WHERE state IN ('queued','failed') AND recipient_user_id=$1`},
		{`UPDATE reschedule_requests SET state='withdrawn' WHERE state='pending' AND booking_id IN (SELECT id FROM bookings WHERE buyer_user_id=$1 OR seller_id=$2)`},
		// Names and emails on records other people or the books still need.
		{`UPDATE bookings SET buyer_name=$3,guest_email='',meeting_url=NULL WHERE buyer_user_id=$1`},
		{`UPDATE bookings SET meeting_url=NULL,calendar_event_id=NULL WHERE seller_id=$2`},
		{`UPDATE quotes SET buyer_name=$3 WHERE buyer_user_id=$1`},
		{`UPDATE offers SET buyer_name=$3,buyer_email='' WHERE buyer_user_id=$1`},
		{`UPDATE reviews SET reviewer_name='Former client',body='' WHERE buyer_user_id=$1`},
		{`UPDATE seller_payouts SET account_sealed=NULL WHERE seller_id=$2 AND state IN ('paid','cancelled')`},
		// Things that exist only for this person.
		{`DELETE FROM seller_payout_accounts WHERE seller_id=$2`},
		{`DELETE FROM calendar_busy_blocks WHERE seller_id=$2`},
		{`DELETE FROM calendar_connections WHERE seller_id=$2`},
		{`DELETE FROM calendar_jobs WHERE booking_id IN (SELECT id FROM bookings WHERE seller_id=$2) AND state IN ('queued','failed')`},
		{`DELETE FROM oauth_states WHERE user_id=$1`},
		{`DELETE FROM availability_windows WHERE seller_id=$2`},
		{`DELETE FROM availability_overrides WHERE seller_id=$2`},
		{`DELETE FROM product_events WHERE seller_id=$2 OR subject_hash=$4`},
		{`DELETE FROM email_challenges WHERE normalized_email=$5`},
		{`DELETE FROM sessions WHERE user_id=$1`},
		{`DELETE FROM push_subscriptions WHERE user_id=$1`},
		{`DELETE FROM push_outbox WHERE user_id=$1`},
		{`DELETE FROM user_identities WHERE user_id=$1`},
		{`UPDATE seller_profiles SET publication_state='deleted',paused=true,identity_url=NULL,avatar_mime=NULL,avatar_data=NULL,avatar_key=NULL,handle='deleted-'||replace(id::text,'-',''),public_version=public_version+1 WHERE id=$2`},
		{`UPDATE users SET display_name=$3,status='deleted',deleted_at=now() WHERE id=$1`},
	}
	for _, s := range steps {
		if _, err = tx.Exec(ctx, `WITH _params AS (SELECT $1::uuid, $2::uuid, $3::text, $4::bytea, $5::text) `+s.sql, u.ID, sellerID, deletedPersonName, analyticsSubjectHash(u.ID), u.Email); err != nil {
			a.log().ErrorContext(ctx, "account deletion step failed", "error", err.Error(), "step", s.sql[:min(60, len(s.sql))])
			problem(w, 503, "DATABASE_ERROR", "Your account could not be deleted.")
			return
		}
	}
	if handle != "" {
		if _, err = tx.Exec(ctx, `INSERT INTO handle_holds(handle,held_until) VALUES($1,now()+make_interval(secs=>$2::float8)) ON CONFLICT(handle) DO UPDATE SET held_until=EXCLUDED.held_until`, handle, handleHoldPeriod.Seconds()); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Your account could not be deleted.")
			return
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO data_requests(id,user_id,kind) VALUES(gen_random_uuid(),$1,'deletion')`, u.ID); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your account could not be deleted.")
		return
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(id,actor_id,action,target_id,safe_summary) VALUES(gen_random_uuid(),$1,'account.deleted',$1,jsonb_build_object('had_link',$2::boolean))`, u.ID, sellerID != nil); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your account could not be deleted.")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Your account could not be deleted.")
		return
	}
	a.removeMedia(avatarKey)
	http.SetCookie(w, &http.Cookie{Name: "aside_session", Value: "", Path: "/", HttpOnly: true, Secure: a.env == "production", SameSite: http.SameSiteLaxMode, MaxAge: -1})
	jsonOut(w, 200, map[string]bool{"deleted": true})
}

// retentionRules delete or strip data once it has served its purpose. Each
// rule is safe to run repeatedly and touches at most a batch of rows.
var retentionRules = []struct{ name, sql string }{
	{"sign-in codes", `DELETE FROM email_challenges WHERE id IN (SELECT id FROM email_challenges WHERE expires_at<now()-interval '1 day' LIMIT 5000)`},
	{"ended sessions", `DELETE FROM sessions WHERE id IN (SELECT id FROM sessions WHERE (expires_at<now()-interval '30 days' OR revoked_at<now()-interval '30 days') LIMIT 5000)`},
	{"calendar sign-in states", `DELETE FROM oauth_states WHERE expires_at<now()-interval '1 day'`},
	{"past calendar busy times", `DELETE FROM calendar_busy_blocks WHERE upper(busy)<now()-interval '1 day'`},
	{"idempotency replies", `DELETE FROM idempotency_records WHERE id IN (SELECT id FROM idempotency_records WHERE created_at<now()-interval '30 days' LIMIT 5000)`},
	{"product analytics", `DELETE FROM product_events WHERE id IN (SELECT id FROM product_events WHERE received_at<now()-interval '400 days' LIMIT 5000)`},
	{"unpaid holds", `UPDATE quotes SET buyer_name='' WHERE id IN (SELECT id FROM quotes WHERE state='expired' AND buyer_name<>'' AND expires_at<now()-interval '90 days' LIMIT 5000)`},
	{"closed offers", `UPDATE offers SET buyer_email='' WHERE id IN (SELECT id FROM offers WHERE state IN ('expired','declined','withdrawn') AND buyer_email<>'' AND created_at<now()-interval '180 days' LIMIT 5000)`},
	{"meeting links", `UPDATE bookings SET meeting_url=NULL WHERE id IN (SELECT id FROM bookings WHERE meeting_url IS NOT NULL AND state<>'confirmed' AND starts_at<now()-interval '30 days' LIMIT 5000)`},
	{"data request log", `DELETE FROM data_requests WHERE created_at<now()-interval '2 years'`},
	{"link holds", `DELETE FROM handle_holds WHERE held_until<now()`},
}

// applyRetention runs every rule once and reports how many rows each changed.
func (a *API) applyRetention(ctx context.Context) (map[string]int64, error) {
	changed := map[string]int64{}
	for _, rule := range retentionRules {
		tag, err := a.db.Exec(ctx, rule.sql)
		if err != nil {
			return changed, fmt.Errorf("retention %s: %w", rule.name, err)
		}
		if n := tag.RowsAffected(); n > 0 {
			changed[rule.name] = n
		}
	}
	return changed, nil
}

// maybeApplyRetention runs the sweep at most once an hour per API instance.
func (a *API) maybeApplyRetention(ctx context.Context, now time.Time) error {
	a.retentionMu.Lock()
	due := now.Sub(a.retentionRan) >= time.Hour
	if due {
		a.retentionRan = now
	}
	a.retentionMu.Unlock()
	if !due {
		return nil
	}
	changed, err := a.applyRetention(ctx)
	if len(changed) > 0 {
		a.log().InfoContext(ctx, "retention sweep", "changed", changed)
	}
	return err
}
