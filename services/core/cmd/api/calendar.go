package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"aside/core/internal/store"
)

const (
	busySyncMaxAge     = 5 * time.Minute // background refresh interval per seller
	busyHoldMaxAge     = 2 * time.Minute // fresher data is fetched before holding a time
	calendarJobRetries = 10
)

// --- seller-facing endpoints ------------------------------------------------

func (a *API) calendarStatus(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	out := map[string]any{"configured": a.googleCalendarConfigured(), "connected": false}
	c, err := store.New(a.db).CalendarConnectionForUser(r.Context(), u.ID)
	if err == nil {
		out["connected"] = true
		out["account_email"] = c.AccountEmail
		out["check_busy"] = c.CheckBusy
		out["add_events"] = c.AddEvents
		out["create_meet_links"] = c.CreateMeetLinks
		out["status"] = c.Status
		out["last_error"] = c.LastError
		out["last_synced_at"] = c.LastSyncedAt
		out["connected_at"] = c.ConnectedAt
	} else if !errors.Is(err, pgx.ErrNoRows) {
		problem(w, 503, "DATABASE_ERROR", "Calendar status could not be loaded.")
		return
	}
	jsonOut(w, 200, out)
}

// calendarConnectStart begins the Google consent flow and returns the URL to open.
func (a *API) calendarConnectStart(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	client, err := a.newGoogleClient()
	if err != nil {
		problem(w, 503, "CALENDAR_NOT_CONFIGURED", "Google Calendar connections are not available yet.")
		return
	}
	q := store.New(a.db)
	if _, err = q.SellerIDForUser(r.Context(), u.ID); err != nil {
		problem(w, 409, "SELLER_REQUIRED", "Claim your link before connecting a calendar.")
		return
	}
	state, err1 := randomToken(32)
	verifier, err2 := randomToken(48)
	if err1 != nil || err2 != nil {
		problem(w, 500, "CALENDAR_ERROR", "The connection could not be started.")
		return
	}
	_ = q.PruneOAuthStates(r.Context())
	if err = q.CreateOAuthState(r.Context(), store.CreateOAuthStateParams{StateHash: digest("google-oauth:" + state), UserID: u.ID, CodeVerifier: verifier}); err != nil {
		problem(w, 503, "DATABASE_ERROR", "The connection could not be started.")
		return
	}
	jsonOut(w, 200, map[string]string{"authorization_url": client.authURL(state, verifier)})
}

// calendarCallback receives Google's redirect. It always ends with a redirect
// back to the connections page carrying a short outcome code.
func (a *API) calendarCallback(w http.ResponseWriter, r *http.Request) {
	done := func(outcome string) {
		http.Redirect(w, r, appOrigin()+"/app/settings/connections?google="+outcome, http.StatusSeeOther)
	}
	query := r.URL.Query()
	if query.Get("error") != "" {
		done("cancelled")
		return
	}
	state, code := query.Get("state"), query.Get("code")
	if state == "" || code == "" || len(state) > 256 || len(code) > 2048 {
		done("failed")
		return
	}
	q := store.New(a.db)
	pending, err := q.ConsumeOAuthState(r.Context(), digest("google-oauth:"+state))
	if err != nil {
		done("expired")
		return
	}
	// The state is bound to the account that started the flow; a link opened
	// in someone else's browser session does nothing.
	u, err := a.currentUser(r)
	if err != nil || u.ID != pending.UserID {
		done("expired")
		return
	}
	client, err := a.newGoogleClient()
	if err != nil {
		done("failed")
		return
	}
	tokens, err := client.exchange(r.Context(), code, pending.CodeVerifier)
	if err != nil {
		a.log().WarnContext(r.Context(), "google token exchange failed", "error", err.Error())
		done("failed")
		return
	}
	granted := grantedScopes(tokens.Scope)
	if !granted[googleScopeFreeBusy] || !granted[googleScopeEventsOwn] {
		client.revoke(r.Context(), tokens.AccessToken)
		done("permissions")
		return
	}
	email := idTokenEmail(tokens.IDToken)
	if tokens.RefreshToken == "" || email == "" {
		client.revoke(r.Context(), tokens.AccessToken)
		done("failed")
		return
	}
	sellerID, err := q.SellerIDForUser(r.Context(), u.ID)
	if err != nil {
		done("failed")
		return
	}
	sealed, err := a.sealCalendarToken(sellerID, []byte(tokens.RefreshToken))
	if err != nil {
		done("failed")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		done("failed")
		return
	}
	defer tx.Rollback(r.Context())
	tq := store.New(tx)
	if err = tq.SaveCalendarConnection(r.Context(), store.SaveCalendarConnectionParams{SellerID: sellerID, AccountEmail: email, EncryptedRefreshToken: sealed, Scopes: tokens.Scope}); err != nil {
		done("failed")
		return
	}
	if err = tq.DeleteBusyBlocks(r.Context(), sellerID); err != nil {
		done("failed")
		return
	}
	if err = tq.EnqueueCalendarSyncForSeller(r.Context(), sellerID); err != nil {
		done("failed")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'calendar.connected',$2,'Seller connected Google Calendar','{}')`, u.ID, sellerID); err != nil {
		done("failed")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		done("failed")
		return
	}
	a.calTokens.put(sellerID, tokens.AccessToken, tokenTTL(tokens.ExpiresIn))
	// Pull busy times right away so the seller's page is accurate at once.
	syncCtx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	_ = a.syncSellerBusy(syncCtx, client, sellerID)
	done("connected")
}

func (a *API) calendarUpdate(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var in struct {
		CheckBusy       *bool `json:"check_busy"`
		AddEvents       *bool `json:"add_events"`
		CreateMeetLinks *bool `json:"create_meet_links"`
	}
	if decode(r, &in) != nil || in.CheckBusy == nil || in.AddEvents == nil || in.CreateMeetLinks == nil {
		problem(w, 422, "INVALID_CALENDAR_SETTINGS", "Choose each calendar setting.")
		return
	}
	q := store.New(a.db)
	current, err := q.CalendarConnectionForUser(r.Context(), u.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		problem(w, 404, "NOT_CONNECTED", "Connect Google Calendar first.")
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Calendar settings could not be saved.")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Calendar settings could not be saved.")
		return
	}
	defer tx.Rollback(r.Context())
	tq := store.New(tx)
	if err = tq.UpdateCalendarPreferences(r.Context(), store.UpdateCalendarPreferencesParams{CheckBusy: *in.CheckBusy, AddEvents: *in.AddEvents, CreateMeetLinks: *in.CreateMeetLinks, SellerID: current.SellerID}); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Calendar settings could not be saved.")
		return
	}
	if !*in.CheckBusy {
		if err = tq.DeleteBusyBlocks(r.Context(), current.SellerID); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Calendar settings could not be saved.")
			return
		}
	}
	if *in.AddEvents || *in.CreateMeetLinks {
		if err = tq.EnqueueCalendarSyncForSeller(r.Context(), current.SellerID); err != nil {
			problem(w, 503, "DATABASE_ERROR", "Calendar settings could not be saved.")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		problem(w, 503, "DATABASE_ERROR", "Calendar settings could not be saved.")
		return
	}
	if *in.CheckBusy && !current.CheckBusy {
		if client, clientErr := a.newGoogleClient(); clientErr == nil {
			syncCtx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
			_ = a.syncSellerBusy(syncCtx, client, current.SellerID)
			cancel()
		}
	}
	a.calendarStatus(w, r)
}

func (a *API) calendarDisconnect(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	q := store.New(a.db)
	current, err := q.CalendarConnectionForUser(r.Context(), u.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		jsonOut(w, 200, map[string]any{"connected": false})
		return
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Google Calendar could not be disconnected.")
		return
	}
	// Tell Google first so the grant is gone even if the seller never visits
	// their Google account settings. A failure here still disconnects locally.
	if client, clientErr := a.newGoogleClient(); clientErr == nil {
		if refresh, openErr := a.openCalendarToken(current.SellerID, current.EncryptedRefreshToken); openErr == nil {
			revokeCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			client.revoke(revokeCtx, string(refresh))
			cancel()
		}
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Google Calendar could not be disconnected.")
		return
	}
	defer tx.Rollback(r.Context())
	tq := store.New(tx)
	if err = tq.DeleteBusyBlocks(r.Context(), current.SellerID); err == nil {
		err = tq.DeleteCalendarConnection(r.Context(), current.SellerID)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_id,action,target_id,reason,safe_summary) VALUES(gen_random_uuid(),$1,'calendar.disconnected',$2,'Seller disconnected Google Calendar','{}')`, u.ID, current.SellerID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		problem(w, 503, "DATABASE_ERROR", "Google Calendar could not be disconnected.")
		return
	}
	a.calTokens.drop(current.SellerID)
	jsonOut(w, 200, map[string]any{"connected": false})
}

// --- tokens and busy time ----------------------------------------------------

func tokenTTL(expiresIn int) time.Duration {
	if expiresIn <= 120 {
		return time.Minute
	}
	return time.Duration(expiresIn-60) * time.Second
}

// calendarAccess returns a current access token for a seller, refreshing it
// with the stored refresh token when needed.
func (a *API) calendarAccess(ctx context.Context, client *googleClient, sellerID string, sealed []byte) (string, error) {
	if token := a.calTokens.get(sellerID); token != "" {
		return token, nil
	}
	refresh, err := a.openCalendarToken(sellerID, sealed)
	if err != nil {
		return "", fmt.Errorf("stored calendar token unreadable: %w", err)
	}
	tokens, err := client.refresh(ctx, string(refresh))
	if err != nil {
		return "", err
	}
	a.calTokens.put(sellerID, tokens.AccessToken, tokenTTL(tokens.ExpiresIn))
	return tokens.AccessToken, nil
}

// recordCalendarProblem shows the seller why the connection stopped working.
func (a *API) recordCalendarProblem(ctx context.Context, sellerID string, err error) {
	status, message := "error", "Google Calendar could not be reached. WantMyTime will keep retrying."
	if errors.Is(err, errCalendarRevoked) {
		status, message = "revoked", "Google Calendar access was removed. Reconnect to keep your calendar in sync."
		a.calTokens.drop(sellerID)
	}
	_ = store.New(a.db).MarkCalendarConnectionProblem(ctx, store.MarkCalendarConnectionProblemParams{Status: status, LastError: message, SellerID: sellerID})
	a.log().WarnContext(ctx, "google calendar problem", "seller_id", sellerID, "status", status, "error", err.Error())
}

// syncSellerBusy replaces a seller's cached busy blocks with Google's view of
// the booking horizon. On failure the previous blocks stay, erring on the side
// of showing the seller as busy.
func (a *API) syncSellerBusy(ctx context.Context, client *googleClient, sellerID string) error {
	q := store.New(a.db)
	conn, err := q.CalendarConnection(ctx, sellerID)
	if err != nil {
		return err
	}
	if conn.Status == "revoked" || !conn.CheckBusy {
		return nil
	}
	var horizon int32
	if err = a.db.QueryRow(ctx, `SELECT booking_horizon_days FROM seller_profiles WHERE id=$1`, sellerID).Scan(&horizon); err != nil {
		return err
	}
	access, err := a.calendarAccess(ctx, client, sellerID, conn.EncryptedRefreshToken)
	if err != nil {
		a.recordCalendarProblem(ctx, sellerID, err)
		return err
	}
	now := time.Now()
	busy, err := client.freeBusy(ctx, access, now.Add(-time.Hour), now.AddDate(0, 0, int(horizon)+1))
	if err != nil {
		a.recordCalendarProblem(ctx, sellerID, err)
		return err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('calendar-busy:' || $1))`, sellerID); err != nil {
		return err
	}
	tq := store.New(tx)
	if err = tq.DeleteBusyBlocks(ctx, sellerID); err != nil {
		return err
	}
	for _, b := range busy {
		if !b.end.After(b.start) {
			continue
		}
		if err = tq.InsertBusyBlock(ctx, store.InsertBusyBlockParams{SellerID: sellerID, StartsAt: b.start, EndsAt: b.end}); err != nil {
			return err
		}
	}
	if err = tq.MarkCalendarSynced(ctx, sellerID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// refreshBusyIfStale fetches fresh busy times before a time is held, when the
// cached copy is older than busyHoldMaxAge. It never blocks a booking for
// long: on timeout or error the cached blocks decide.
func (a *API) refreshBusyIfStale(ctx context.Context, sellerID string) {
	if !a.googleCalendarConfigured() || sellerID == "" {
		return
	}
	row, err := store.New(a.db).SellerForBusyRefresh(ctx, store.SellerForBusyRefreshParams{MaxAgeSeconds: int32(busyHoldMaxAge.Seconds()), SellerID: sellerID})
	if err != nil || !row.Stale {
		return
	}
	client, err := a.newGoogleClient()
	if err != nil {
		return
	}
	syncCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	_ = a.syncSellerBusy(syncCtx, client, sellerID)
}

// calendarConflict reports whether the seller's Google calendar is busy during
// [start, end). ignoreBookingID skips the moving booking's own event.
func calendarConflict(ctx context.Context, tx pgx.Tx, sellerID string, start, end time.Time, ignoreBookingID *string) (bool, error) {
	return store.New(tx).CalendarBusyConflict(ctx, store.CalendarBusyConflictParams{SellerID: sellerID, FromAt: start, ToAt: end, IgnoreBookingID: ignoreBookingID})
}

// --- background worker -------------------------------------------------------

func (a *API) runCalendarWorker(ctx context.Context) {
	for {
		err := a.calendarCycle(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			a.log().ErrorContext(ctx, "calendar worker cycle failed", "error", err.Error())
		}
		a.beat(ctx, "calendar", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (a *API) calendarCycle(ctx context.Context) error {
	client, err := a.newGoogleClient()
	if err != nil {
		return err
	}
	q := store.New(a.db)
	jobs, err := q.ClaimCalendarJobs(ctx, 10)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		a.processCalendarJob(ctx, client, job)
	}
	due, err := q.ConnectionsDueForBusySync(ctx, store.ConnectionsDueForBusySyncParams{MaxAgeSeconds: int32(busySyncMaxAge.Seconds()), BatchSize: 5})
	if err != nil {
		return err
	}
	for _, c := range due {
		_ = a.syncSellerBusy(ctx, client, c.SellerID) // problems are recorded on the connection
	}
	return q.PruneOAuthStates(ctx)
}

func (a *API) finishCalendarJob(ctx context.Context, job store.ClaimCalendarJobsRow, state string, reason string, retry time.Duration) {
	if state == "queued" && int(job.Attempts) >= calendarJobRetries {
		state = "failed"
	}
	var lastError *string
	if reason != "" {
		lastError = &reason
	}
	_ = store.New(a.db).FinishCalendarJob(ctx, store.FinishCalendarJobParams{State: state, LastError: lastError, RetrySeconds: int32(retry.Seconds()), ID: job.ID})
}

func calendarBackoff(attempts int16) time.Duration {
	d := 30 * time.Second << min(int(attempts), 7)
	if d > time.Hour {
		d = time.Hour
	}
	return d
}

// processCalendarJob makes the seller's calendar match the booking's current
// state: create or move its event, remove it when the booking is no longer
// confirmed, and collect a Google Meet link when the booking has none.
func (a *API) processCalendarJob(ctx context.Context, client *googleClient, job store.ClaimCalendarJobsRow) {
	q := store.New(a.db)
	b, err := q.CalendarBookingContext(ctx, job.BookingID)
	if err != nil {
		a.finishCalendarJob(ctx, job, "failed", "booking unavailable", 0)
		return
	}
	conn, err := q.CalendarConnection(ctx, b.SellerID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (conn.Status == "revoked" || !(conn.AddEvents || conn.CreateMeetLinks))) {
		a.finishCalendarJob(ctx, job, "done", "calendar not connected", 0)
		return
	}
	if err != nil {
		a.finishCalendarJob(ctx, job, "queued", "database error", calendarBackoff(job.Attempts))
		return
	}
	access, err := a.calendarAccess(ctx, client, b.SellerID, conn.EncryptedRefreshToken)
	if err != nil {
		a.recordCalendarProblem(ctx, b.SellerID, err)
		if errors.Is(err, errCalendarRevoked) {
			a.finishCalendarJob(ctx, job, "done", "calendar access revoked", 0)
		} else {
			a.finishCalendarJob(ctx, job, "queued", "token refresh failed", calendarBackoff(job.Attempts))
		}
		return
	}
	fail := func(err error) {
		if errors.Is(err, errCalendarRevoked) {
			a.recordCalendarProblem(ctx, b.SellerID, err)
			a.finishCalendarJob(ctx, job, "done", "calendar access revoked", 0)
			return
		}
		a.finishCalendarJob(ctx, job, "queued", err.Error(), calendarBackoff(job.Attempts))
	}
	if b.State != "confirmed" {
		if b.CalendarEventID != "" {
			if err = client.deleteEvent(ctx, access, b.CalendarEventID); err != nil {
				fail(err)
				return
			}
			_ = q.SetBookingCalendarEvent(ctx, store.SetBookingCalendarEventParams{EventID: nil, BookingID: b.BookingID})
		}
		a.finishCalendarJob(ctx, job, "done", "", 0)
		return
	}
	if !b.StartsAt.After(time.Now()) {
		a.finishCalendarJob(ctx, job, "done", "", 0)
		return
	}
	needMeet := conn.CreateMeetLinks && !b.HasMeetingLink
	bookingURL := appOrigin() + "/booking/" + b.BookingID
	body := map[string]any{
		"summary":            fmt.Sprintf("%s · %d min (WantMyTime)", b.BuyerName, b.DurationMinutes),
		"description":        "Booking on WantMyTime with " + b.BuyerName + ".\nBooking page: " + bookingURL,
		"start":              map[string]string{"dateTime": b.StartsAt.UTC().Format(time.RFC3339)},
		"end":                map[string]string{"dateTime": b.StartsAt.Add(time.Duration(b.DurationMinutes) * time.Minute).UTC().Format(time.RFC3339)},
		"visibility":         "private",
		"transparency":       "opaque",
		"status":             "confirmed",
		"reminders":          map[string]bool{"useDefault": true},
		"extendedProperties": map[string]any{"private": map[string]string{"asideBookingId": b.BookingID}},
	}
	if needMeet {
		body["conferenceData"] = map[string]any{"createRequest": map[string]any{"requestId": "aside-" + b.BookingID, "conferenceSolutionKey": map[string]string{"type": "hangoutsMeet"}}}
	}
	eventID := b.CalendarEventID
	var ev googleEvent
	insert := func() (googleEvent, error) {
		withID := map[string]any{"id": calendarEventID(b.BookingID)}
		for k, v := range body {
			withID[k] = v
		}
		created, insertErr := client.insertEvent(ctx, access, withID)
		if errors.Is(insertErr, errGoogleConflict) {
			// A previous attempt created it (or the seller deleted it, which
			// Google keeps as cancelled): update it back into shape.
			return client.patchEvent(ctx, access, calendarEventID(b.BookingID), body)
		}
		return created, insertErr
	}
	if eventID == "" {
		ev, err = insert()
	} else {
		ev, err = client.patchEvent(ctx, access, eventID, body)
		if errors.Is(err, errGoogleNotFound) {
			ev, err = insert()
		}
	}
	if err != nil {
		fail(err)
		return
	}
	if ev.ID == "" {
		ev.ID = calendarEventID(b.BookingID)
	}
	if err = q.SetBookingCalendarEvent(ctx, store.SetBookingCalendarEventParams{EventID: &ev.ID, BookingID: b.BookingID}); err != nil {
		a.finishCalendarJob(ctx, job, "queued", "database error", calendarBackoff(job.Attempts))
		return
	}
	if needMeet {
		link, pending := ev.meetLink()
		if link == "" {
			if pending {
				a.finishCalendarJob(ctx, job, "queued", "waiting for Google Meet", 15*time.Second)
			} else {
				a.finishCalendarJob(ctx, job, "failed", "Google did not create a Meet link", 0)
			}
			return
		}
		if err = a.storeMeetLink(ctx, b.BookingID, link); err != nil {
			a.finishCalendarJob(ctx, job, "queued", "meeting link not saved", calendarBackoff(job.Attempts))
			return
		}
	}
	a.finishCalendarJob(ctx, job, "done", "", 0)
}

// storeMeetLink saves a Google Meet link as the booking's meeting link unless
// the seller already added one, and emails it to the buyer.
func (a *API) storeMeetLink(ctx context.Context, bookingID, link string) error {
	sealed, err := a.encryptMeetingLink([]byte(link))
	if err != nil {
		return err
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := store.New(tx).SetBookingMeetLink(ctx, store.SetBookingMeetLinkParams{MeetingUrl: sealed, BookingID: bookingID})
	if err != nil {
		return err
	}
	if rows == 1 {
		if err = a.enqueueMeetingReady(ctx, tx, bookingID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func enqueueCalendarSync(ctx context.Context, tx pgx.Tx, bookingID string) error {
	return store.New(tx).EnqueueCalendarSync(ctx, bookingID)
}
