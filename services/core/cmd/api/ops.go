package main

import (
	"context"
	"encoding/json"
	"errors"
	_ "image/jpeg"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

func (a *API) requireOps(w http.ResponseWriter, r *http.Request, permission string) (user, bool) {
	u, err := a.currentUser(r)
	if err != nil {
		problem(w, 401, "OPS_AUTH_REQUIRED", "Sign in with an authorized operations account.")
		return user{}, false
	}
	var allowed bool
	err = a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM admin_grants WHERE user_id=$1 AND permission=$2 AND revoked_at IS NULL)`, u.ID, permission).Scan(&allowed)
	if err != nil || !allowed {
		problem(w, 403, "OPS_FORBIDDEN", "This account does not have that operations permission.")
		return user{}, false
	}
	var verified *time.Time
	err = a.db.QueryRow(r.Context(), `SELECT mfa_verified_at FROM sessions WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>now()`, digestFromRequest(r)).Scan(&verified)
	if err != nil || verified == nil || verified.Before(time.Now().Add(-10*time.Hour)) {
		problem(w, 401, "OPS_MFA_REQUIRED", "Verify your authenticator code to enter operations.")
		return user{}, false
	}
	return u, true
}

func (a *API) hasOpsPermission(ctx context.Context, userID, permission string) bool {
	var allowed bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_grants WHERE user_id=$1 AND permission=$2 AND revoked_at IS NULL)`, userID, permission).Scan(&allowed)
	return err == nil && allowed
}

func (a *API) opsSession(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if decode(r, &in) != nil || len(in.Code) != 6 {
		problem(w, 422, "INVALID_CODE", "Enter the six digit authenticator code.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Operations verification is unavailable.")
		return
	}
	defer tx.Rollback(r.Context())
	var encrypted []byte
	var failed int
	var lockedUntil *time.Time
	var lastStep *int64
	err = tx.QueryRow(r.Context(), `SELECT encrypted_secret,failed_attempts,locked_until,last_totp_step FROM admin_mfa WHERE user_id=$1 AND EXISTS(SELECT 1 FROM admin_grants WHERE user_id=$1 AND revoked_at IS NULL) FOR UPDATE`, u.ID).Scan(&encrypted, &failed, &lockedUntil, &lastStep)
	if err != nil {
		problem(w, 403, "OPS_NOT_CONFIGURED", "Operations access is not configured for this account.")
		return
	}
	if lockedUntil != nil && lockedUntil.After(time.Now()) {
		problem(w, 429, "OPS_MFA_RATE_LIMITED", "Too many incorrect codes. Try again later.")
		return
	}
	secret, err := decryptMFASecret(encrypted)
	if err != nil {
		problem(w, 503, "OPS_MFA_UNAVAILABLE", "The authenticator secret could not be loaded.")
		return
	}
	step, matched := matchTOTPStep(secret, in.Code, time.Now())
	if matched && lastStep != nil && step <= *lastStep {
		matched = false // a code (or an older one) was already used
	}
	if !matched {
		var lockErr error
		if failed >= 4 {
			_, lockErr = tx.Exec(r.Context(), `UPDATE admin_mfa SET failed_attempts=0,locked_until=now()+interval '15 minutes' WHERE user_id=$1`, u.ID)
		} else {
			_, lockErr = tx.Exec(r.Context(), `UPDATE admin_mfa SET failed_attempts=failed_attempts+1 WHERE user_id=$1`, u.ID)
		}
		if lockErr != nil || tx.Commit(r.Context()) != nil {
			problem(w, 503, "OPS_MFA_UNAVAILABLE", "The failed authenticator attempt could not be recorded.")
			return
		}
		problem(w, 401, "OPS_MFA_INVALID", "That authenticator code is not valid.")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE admin_mfa SET failed_attempts=0,locked_until=NULL,last_totp_step=$2 WHERE user_id=$1`, u.ID, step)
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE sessions SET mfa_verified_at=now() WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>now()`, digestFromRequest(r))
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Operations access could not be verified.")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Operations access could not be verified.")
		return
	}
	jsonOut(w, 200, map[string]bool{"verified": true})
}

func (a *API) opsOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	var people, bookings, offers int64
	if err := a.db.QueryRow(r.Context(), `SELECT count(*) FROM users`).Scan(&people); err != nil {
		problem(w, 503, "OVERVIEW_UNAVAILABLE", "The people count could not be loaded.")
		return
	}
	if err := a.db.QueryRow(r.Context(), `SELECT count(*) FROM bookings`).Scan(&bookings); err != nil {
		problem(w, 503, "OVERVIEW_UNAVAILABLE", "The booking count could not be loaded.")
		return
	}
	if err := a.db.QueryRow(r.Context(), `SELECT count(*) FROM offers WHERE state IN ('pending','countered','agreed')`).Scan(&offers); err != nil {
		problem(w, 503, "OVERVIEW_UNAVAILABLE", "The offer count could not be loaded.")
		return
	}
	var overdue int64
	if err := a.db.QueryRow(r.Context(), `SELECT count(*) FROM bookings WHERE state='confirmed' AND meeting_url IS NULL AND meeting_deadline<now() AND starts_at>now()`).Scan(&overdue); err != nil {
		problem(w, 503, "OVERVIEW_UNAVAILABLE", "The meeting queue count could not be loaded.")
		return
	}
	var requests int64
	if err := a.db.QueryRow(r.Context(), `SELECT count(*) FROM booking_cancellation_requests WHERE state='open'`).Scan(&requests); err != nil {
		problem(w, 503, "OVERVIEW_UNAVAILABLE", "The cancellation queue count could not be loaded.")
		return
	}
	var events []map[string]any
	rows, err := a.db.Query(r.Context(), `SELECT event_name,count(*) FROM product_events WHERE event_day>=((now() AT TIME ZONE 'UTC')::date-6) AND environment=$1 GROUP BY event_name ORDER BY event_name`, envOr("APP_ENV", "local"))
	if err != nil {
		problem(w, 503, "OVERVIEW_UNAVAILABLE", "Product event counts could not be loaded.")
		return
	}
	defer rows.Close()
	events = []map[string]any{}
	for rows.Next() {
		var name string
		var count int64
		if err := rows.Scan(&name, &count); err != nil {
			problem(w, 503, "OVERVIEW_UNAVAILABLE", "Product event counts could not be read.")
			return
		}
		events = append(events, map[string]any{"event": name, "count": count})
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "OVERVIEW_UNAVAILABLE", "Product event counts could not be read.")
		return
	}
	collection := "disabled"
	if a.providerCheckoutConfigured() {
		collection = "enabled_by_environment_gates"
	}
	jsonOut(w, 200, map[string]any{"people": people, "bookings": bookings, "open_offers": offers, "overdue_meeting_links": overdue, "open_cancellation_requests": requests, "product_events_7d": events, "payment_collection": collection, "settlement_reconciliation": "normalized_csv_import_available_provider_format_unverified"})
}

func (a *API) opsOverdueMeetings(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT b.id::text,sp.handle,owner.display_name,b.buyer_name,b.starts_at,b.meeting_deadline FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id JOIN users owner ON owner.id=sp.user_id WHERE b.state='confirmed' AND b.meeting_url IS NULL AND b.meeting_deadline<now() AND b.starts_at>now() AND ($1::timestamptz IS NULL OR (b.meeting_deadline,b.id)<($1,$2::uuid)) ORDER BY b.meeting_deadline DESC,b.id DESC LIMIT 100`, cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Meeting delivery queue could not be loaded.")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, handle, seller, buyer string
		var starts, deadline time.Time
		if err := rows.Scan(&id, &handle, &seller, &buyer, &starts, &deadline); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Meeting delivery queue could not be read.")
			return
		}
		out = append(out, map[string]any{"id": id, "seller_handle": handle, "seller_name": seller, "buyer_name": buyer, "starts_at": starts, "deadline": deadline})
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Meeting delivery queue could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"meetings": out, "next_cursor": nextListCursor(out, "deadline", "id")})
}

// opsResolveBookingIssue closes a reported problem. The operator can refund
// the buyer part or all of the payment; the rest of the seller's payout is
// then released.
func (a *API) opsResolveBookingIssue(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:booking:resolve")
	if !ok {
		return
	}
	var in struct {
		Resolution  string `json:"resolution"`
		RefundMinor int64  `json:"refund_minor"`
	}
	if decode(r, &in) != nil || len(strings.TrimSpace(in.Resolution)) < 8 || len(in.Resolution) > 500 || in.RefundMinor < 0 {
		problem(w, 422, "RESOLUTION_REQUIRED", "Record what was reviewed or communicated before resolving this problem.")
		return
	}
	if in.RefundMinor > 0 && !a.hasOpsPermission(r.Context(), actor.ID, "ops:refund:approve") {
		problem(w, 403, "REFUND_PERMISSION_REQUIRED", "Refunding needs the refund approval permission.")
		return
	}
	resolution := strings.TrimSpace(in.Resolution)
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Problem could not be resolved.")
		return
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	b, err := store.New(tx).LockBookingForCancellation(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "ISSUE_NOT_OPEN", "This booking has no open problem.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Problem could not be resolved.")
		return
	}
	tag, err := tx.Exec(r.Context(), `UPDATE bookings SET issue_resolved_at=now(),issue_resolution=$2 WHERE id=$1 AND issue_reason IS NOT NULL AND issue_resolved_at IS NULL`, id, resolution)
	if err != nil || tag.RowsAffected() != 1 {
		problem(w, 404, "ISSUE_NOT_OPEN", "This booking has no open problem.")
		return
	}
	var refundID *string
	if in.RefundMinor > 0 {
		if in.RefundMinor > b.GrossMinor {
			problem(w, 422, "REFUND_TOO_LARGE", "A refund cannot be more than the amount paid.")
			return
		}
		rid, refundErr := a.createRefund(r.Context(), tx, b, in.RefundMinor, "problem_upheld", &actor.ID)
		switch {
		case refundErr != nil && strings.Contains(refundErr.Error(), "no verified payment"):
			problem(w, 409, "NOT_REFUNDABLE", "This booking has no verified payment to refund.")
			return
		case pgErrCode(refundErr) == "23505":
			problem(w, 409, "REFUND_EXISTS", "This booking already has a refund.")
			return
		case refundErr != nil:
			problem(w, 503, "DATABASE_ERROR", "Problem could not be resolved.")
			return
		}
		refundID = &rid
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'booking.issue_resolved',$2,$3,jsonb_build_object('refund_minor',$4::bigint))`, actor.ID, id, resolution, in.RefundMinor); err != nil {
		problem(w, 503, "AUDIT_REQUIRED", "Problem resolution could not be audited.")
		return
	}
	for _, recipient := range []struct{ user, kind string }{{b.BuyerUserID, "problem_resolved_buyer"}, {b.SellerUserID, "problem_resolved_seller"}} {
		if err = enqueueBookingEvent(r.Context(), tx, id, recipient.user, recipient.kind, id+":problem-resolved:"+time.Now().UTC().Format(time.RFC3339Nano)+":"+recipient.kind, refundID); err != nil {
			problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "Problem could not be resolved.")
			return
		}
	}
	if tx.Commit(r.Context()) != nil {
		problem(w, 503, "DATABASE_ERROR", "Problem could not be resolved.")
		return
	}
	jsonOut(w, 200, map[string]any{"resolved": true, "refund_id": refundID})
}

func (a *API) opsSystem(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	var queued, processing, sent, failed, cancelled int64
	var oldest *time.Time
	err := a.db.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE state='queued'),count(*) FILTER(WHERE state='processing'),count(*) FILTER(WHERE state='sent'),count(*) FILTER(WHERE state='failed'),count(*) FILTER(WHERE state='cancelled'),min(due_at) FILTER(WHERE state='queued') FROM notification_outbox`).Scan(&queued, &processing, &sent, &failed, &cancelled, &oldest)
	if err != nil {
		problem(w, 503, "NOTIFICATION_STATUS_UNAVAILABLE", "Notification status could not be loaded.")
		return
	}
	var providerQueued, providerProcessing, providerFailed int64
	var providerOldest *time.Time
	if err = a.db.QueryRow(r.Context(), `SELECT count(*) FILTER(WHERE state IN ('queued','retry')),count(*) FILTER(WHERE state='processing'),count(*) FILTER(WHERE state='failed'),min(received_at) FILTER(WHERE state IN ('queued','retry','failed')) FROM provider_events`).Scan(&providerQueued, &providerProcessing, &providerFailed, &providerOldest); err != nil {
		problem(w, 503, "PAYMENT_STATUS_UNAVAILABLE", "Provider event status could not be loaded.")
		return
	}
	_, intlErr := store.New(a.db).ApprovedChannelFee(r.Context(), "card_international")
	jsonOut(w, 200, map[string]any{"email_transport_configured": a.emailConfigured(), "payments_enabled": a.providerCheckoutConfigured(), "checkouts_paused": os.Getenv("CHECKOUTS_PAUSED") == "true",
		"international_cards": map[string]bool{"enabled": internationalCardsEnabled(), "fee_schedule_approved": intlErr == nil}, "notifications": map[string]any{"queued": queued, "processing": processing, "sent": sent, "failed": failed, "cancelled": cancelled, "oldest_queued_at": oldest}, "provider_events": map[string]any{"queued": providerQueued, "processing": providerProcessing, "failed": providerFailed, "oldest_pending_at": providerOldest}})
}

func (a *API) opsPeople(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT u.id::text,u.display_name,u.status,COALESCE(i.normalized_identifier,''),u.created_at FROM users u LEFT JOIN LATERAL (SELECT normalized_identifier FROM user_identities WHERE user_id=u.id AND type='email' AND verified_at IS NOT NULL ORDER BY verified_at DESC,id DESC LIMIT 1) i ON true WHERE $1::timestamptz IS NULL OR (u.created_at,u.id)<($1,$2::uuid) ORDER BY u.created_at DESC,u.id DESC LIMIT 100`, cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "People could not be loaded.")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, status, email string
		var created time.Time
		if err := rows.Scan(&id, &name, &status, &email, &created); err != nil {
			problem(w, 503, "DATABASE_ERROR", "People could not be read.")
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "status": status, "email": email, "created_at": created})
	}
	if err = rows.Err(); err != nil {
		problem(w, 503, "DATABASE_ERROR", "People could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"people": out, "next_cursor": nextListCursor(out, "created_at", "id")})
}

func (a *API) opsPersonDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	var id, name, status string
	var email *string
	var created time.Time
	err := a.db.QueryRow(r.Context(), `SELECT u.id::text,u.display_name,u.status,i.normalized_identifier,u.created_at FROM users u LEFT JOIN user_identities i ON i.user_id=u.id AND i.type='email' AND i.verified_at IS NOT NULL WHERE u.id=$1`, r.PathValue("id")).Scan(&id, &name, &status, &email, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This account was not found.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Account could not be loaded.")
		return
	}
	var handle, publication, readiness string
	var paused, payoutAccount bool
	se := a.db.QueryRow(r.Context(), `SELECT handle,publication_state,readiness_state,paused,EXISTS(SELECT 1 FROM seller_payout_accounts a WHERE a.seller_id=seller_profiles.id) FROM seller_profiles WHERE user_id=$1`, id).Scan(&handle, &publication, &readiness, &paused, &payoutAccount)
	var sellerInfo any
	if se == nil {
		sellerInfo = map[string]any{"handle": handle, "publication_state": publication, "readiness_state": readiness, "paused": paused, "payout_account_added": payoutAccount}
	} else if !errors.Is(se, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "Account could not be loaded.")
		return
	}
	jsonOut(w, 200, map[string]any{"id": id, "name": name, "status": status, "email": email, "created_at": created, "seller": sellerInfo})
}

func (a *API) opsBookings(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT b.id::text,sp.handle,b.buyer_name,b.duration_minutes,b.starts_at,b.gross_minor::text,b.state,b.payment_state,b.issue_reason,b.issue_resolved_at IS NOT NULL,b.created_at FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE $1::timestamptz IS NULL OR (b.created_at,b.id)<($1,$2::uuid) ORDER BY b.created_at DESC,b.id DESC LIMIT 100`, cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Bookings could not be loaded.")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, handle, name, gross, state, payment string
		var issue *string
		var resolved bool
		var duration int
		var starts, created time.Time
		if err := rows.Scan(&id, &handle, &name, &duration, &starts, &gross, &state, &payment, &issue, &resolved, &created); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Bookings could not be read.")
			return
		}
		out = append(out, map[string]any{"id": id, "seller": handle, "buyer_name": name, "duration_minutes": duration, "starts_at": starts, "gross_minor": gross, "state": state, "payment_state": payment, "issue_reason": issue, "issue_resolved": resolved, "created_at": created})
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Bookings could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"bookings": out, "next_cursor": nextListCursor(out, "created_at", "id")})
}

func (a *API) opsBookingDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	var id, handle, buyer, guestEmail, state, payment, currency string
	var duration int
	var starts, created, deadline time.Time
	var gross int64
	var issue *string
	var resolved bool
	var meetingReady *time.Time
	err := a.db.QueryRow(r.Context(), `SELECT b.id::text,sp.handle,b.buyer_name,b.guest_email,b.duration_minutes,b.starts_at,b.gross_minor,b.currency,b.state,b.payment_state,b.created_at,b.meeting_deadline,b.meeting_ready_at,b.issue_reason,b.issue_resolved_at IS NOT NULL FROM bookings b JOIN seller_profiles sp ON sp.id=b.seller_id WHERE b.id=$1`, r.PathValue("id")).Scan(&id, &handle, &buyer, &guestEmail, &duration, &starts, &gross, &currency, &state, &payment, &created, &deadline, &meetingReady, &issue, &resolved)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_FOUND", "This booking was not found.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Booking could not be loaded.")
		return
	}
	out := map[string]any{"id": id, "seller": handle, "buyer_name": buyer, "guest_email": guestEmail, "duration_minutes": duration, "starts_at": starts, "gross_minor": gross, "currency": strings.TrimSpace(currency), "state": state, "payment_state": payment, "created_at": created, "meeting_deadline": deadline, "meeting_ready_at": meetingReady, "issue_reason": issue, "issue_resolved": resolved, "payout": nil}
	var reportedBy, resolution string
	if err = a.db.QueryRow(r.Context(), `SELECT COALESCE(issue_reported_by,''),COALESCE(issue_resolution,'') FROM bookings WHERE id=$1`, id).Scan(&reportedBy, &resolution); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Booking could not be loaded.")
		return
	}
	out["issue_reported_by"], out["issue_resolution"] = reportedBy, resolution
	payouts, err := a.queryPayouts(r.Context(), `po.booking_id=$2`, id)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Booking could not be loaded.")
		return
	}
	if len(payouts) == 1 {
		out["payout"] = payouts[0].json(true)
	}
	jsonOut(w, 200, out)
}

func (a *API) opsOffers(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT o.id::text,sp.handle,o.buyer_name,o.duration_minutes,o.state,o.expires_at,o.created_at FROM offers o JOIN seller_profiles sp ON sp.id=o.seller_id WHERE $1::timestamptz IS NULL OR (o.created_at,o.id)<($1,$2::uuid) ORDER BY o.created_at DESC,o.id DESC LIMIT 100`, cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Offers could not be loaded.")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, handle, name, state string
		var duration int
		var expires, created time.Time
		if err := rows.Scan(&id, &handle, &name, &duration, &state, &expires, &created); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Offers could not be read.")
			return
		}
		out = append(out, map[string]any{"id": id, "seller": handle, "buyer_name": name, "duration_minutes": duration, "state": state, "expires_at": expires, "created_at": created})
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Offers could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"offers": out, "next_cursor": nextListCursor(out, "created_at", "id")})
}

func (a *API) opsAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id::text,actor_id::text,action,target_id::text,reason,safe_summary,created_at FROM audit_events WHERE $1::timestamptz IS NULL OR (created_at,id)<($1,$2::uuid) ORDER BY created_at DESC,id DESC LIMIT 100`, cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Audit history could not be loaded.")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, action string
		var actor, target, reason *string
		var summary []byte
		var created time.Time
		if err := rows.Scan(&id, &actor, &action, &target, &reason, &summary, &created); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Audit history could not be read.")
			return
		}
		out = append(out, map[string]any{"id": id, "actor_id": actor, "action": action, "target_id": target, "reason": reason, "summary": json.RawMessage(summary), "created_at": created})
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Audit history could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"events": out, "next_cursor": nextListCursor(out, "created_at", "id")})
}

func (a *API) opsRestrict(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:account:restrict")
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if decode(r, &in) != nil || len(strings.TrimSpace(in.Reason)) < 8 || len(in.Reason) > 300 {
		problem(w, 422, "REASON_REQUIRED", "Provide a clear reason for restricting this account.")
		return
	}
	id := r.PathValue("id")
	if strings.EqualFold(id, actor.ID) {
		problem(w, 409, "SELF_RESTRICTION_DENIED", "Operators cannot restrict their own account.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Account could not be restricted.")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `UPDATE users SET status='restricted' WHERE id=$1`, id)
	if err != nil || tag.RowsAffected() != 1 {
		problem(w, 404, "NOT_FOUND", "This account was not found.")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE seller_profiles SET paused=true WHERE user_id=$1`, id); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The seller link could not be paused; the restriction was not committed.")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Sessions could not be revoked; the restriction was not committed.")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'account.restricted',$2,$3,'{}')`, actor.ID, id, in.Reason)
	if err != nil {
		problem(w, 503, "AUDIT_REQUIRED", "The restriction could not be audited; no change was committed.")
		return
	}
	if tx.Commit(r.Context()) != nil {
		problem(w, 503, "DATABASE_ERROR", "Account could not be restricted.")
		return
	}
	jsonOut(w, 200, map[string]bool{"restricted": true})
}

func (a *API) opsRevokeSessions(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:session:revoke")
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if decode(r, &in) != nil || len(strings.TrimSpace(in.Reason)) < 8 || len(in.Reason) > 300 {
		problem(w, 422, "REASON_REQUIRED", "Provide a clear reason for revoking sessions.")
		return
	}
	id := r.PathValue("id")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Sessions could not be revoked.")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Sessions could not be revoked.")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'sessions.revoked',$2,$3,jsonb_build_object('count',$4::bigint))`, actor.ID, id, in.Reason, tag.RowsAffected())
	if err != nil {
		problem(w, 503, "AUDIT_REQUIRED", "Session revocation could not be audited.")
		return
	}
	if tx.Commit(r.Context()) != nil {
		problem(w, 503, "DATABASE_ERROR", "Sessions could not be revoked.")
		return
	}
	jsonOut(w, 200, map[string]any{"revoked_sessions": tag.RowsAffected()})
}

func (a *API) opsCancellations(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOps(w, r, "ops:read"); !ok {
		return
	}
	cursorAt, cursorID, ok := cursorForRequest(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT cr.id::text,cr.booking_id::text,cr.reason,cr.created_at,b.starts_at,sp.handle,u.display_name FROM booking_cancellation_requests cr JOIN bookings b ON b.id=cr.booking_id JOIN seller_profiles sp ON sp.id=b.seller_id JOIN users u ON u.id=cr.requester_user_id WHERE cr.state='open' AND ($1::timestamptz IS NULL OR (cr.created_at,cr.id)<($1,$2::uuid)) ORDER BY cr.created_at DESC,cr.id DESC LIMIT 100`, cursorAt, cursorID)
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Cancellation requests could not be loaded.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, bid, reason, handle, name string
		var created, starts time.Time
		if err := rows.Scan(&id, &bid, &reason, &created, &starts, &handle, &name); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Cancellation requests could not be read.")
			return
		}
		items = append(items, map[string]any{"id": id, "booking_id": bid, "reason": reason, "created_at": created, "starts_at": starts, "seller": handle, "requester": name})
	}
	if err := rows.Err(); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Cancellation requests could not be read.")
		return
	}
	jsonOut(w, 200, map[string]any{"requests": items, "next_cursor": nextListCursor(items, "created_at", "id")})
}

func (a *API) opsResolveCancellation(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireOps(w, r, "ops:booking:resolve")
	if !ok {
		return
	}
	var in struct {
		Resolution string `json:"resolution"`
	}
	if decode(r, &in) != nil || len(strings.TrimSpace(in.Resolution)) < 8 || len(in.Resolution) > 500 {
		problem(w, 422, "RESOLUTION_REQUIRED", "Record the review outcome; a booking or payment is not changed automatically.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Cancellation request could not be resolved.")
		return
	}
	defer tx.Rollback(r.Context())
	id := r.PathValue("id")
	tag, err := tx.Exec(r.Context(), `UPDATE booking_cancellation_requests SET state='resolved',resolution=$2,resolved_at=now() WHERE id=$1 AND state='open'`, id, strings.TrimSpace(in.Resolution))
	if err != nil || tag.RowsAffected() != 1 {
		problem(w, 404, "CANCELLATION_NOT_OPEN", "This cancellation request is not open.")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'booking.cancellation_request_resolved',$2,$3,'{}')`, actor.ID, id, strings.TrimSpace(in.Resolution)); err != nil {
		problem(w, 503, "AUDIT_REQUIRED", "Resolution could not be audited.")
		return
	}
	if err = enqueueCancellationReview(r.Context(), tx, id); err != nil {
		problem(w, 503, "NOTIFICATION_QUEUE_ERROR", "The review was not saved because participant notifications could not be queued.")
		return
	}
	if tx.Commit(r.Context()) != nil {
		problem(w, 503, "DATABASE_ERROR", "Cancellation request could not be resolved.")
		return
	}
	jsonOut(w, 200, map[string]bool{"resolved": true})
}
