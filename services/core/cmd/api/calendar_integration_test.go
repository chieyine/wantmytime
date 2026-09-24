//go:build integration

package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGoogle stands in for accounts.google.com, oauth2.googleapis.com and the
// Calendar API.
type fakeGoogle struct {
	mu          sync.Mutex
	server      *httptest.Server
	challenge   string // PKCE challenge seen on the consent URL
	scope       string // scopes granted on exchange
	email       string
	revokedRefs int
	grantBroken bool           // refresh_token grant returns invalid_grant
	busy        []busyInterval // extra busy time
	events      map[string]fakeEvent
	meetPending int // number of event writes that report the Meet link as pending
	calls       []string
}

type fakeEvent struct {
	start, end time.Time
	summary    string
	hasMeet    bool
	attendees  int
	status     string
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	f := &fakeGoogle{scope: "openid https://www.googleapis.com/auth/userinfo.email " + googleScopeFreeBusy + " " + googleScopeEventsOwn, email: "seller@gmail.test", events: map[string]fakeEvent{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	for k, v := range map[string]string{"GOOGLE_CLIENT_ID": "client-id", "GOOGLE_CLIENT_SECRET": "client-secret", "GOOGLE_AUTH_BASE": f.server.URL, "GOOGLE_OAUTH_BASE": f.server.URL, "GOOGLE_API_BASE": f.server.URL} {
		t.Setenv(k, v)
	}
	return f
}

func (f *fakeGoogle) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	idToken := func() string {
		enc := base64.RawURLEncoding.EncodeToString
		claims, _ := json.Marshal(map[string]any{"email": f.email, "email_verified": true})
		return enc([]byte(`{"alg":"RS256"}`)) + "." + enc(claims) + ".sig"
	}
	switch {
	case r.URL.Path == "/token":
		_ = r.ParseForm()
		if r.Form.Get("client_secret") != "client-secret" {
			w.WriteHeader(401)
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.Form.Get("code") != "good-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-1", "expires_in": 3599, "refresh_token": "refresh-1", "scope": f.scope, "id_token": idToken()})
		case "refresh_token":
			if f.grantBroken || r.Form.Get("refresh_token") != "refresh-1" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-2", "expires_in": 3599})
		}
	case r.URL.Path == "/revoke":
		f.revokedRefs++
	case r.URL.Path == "/calendar/v3/freeBusy":
		var in struct{ TimeMin, TimeMax time.Time }
		_ = json.NewDecoder(r.Body).Decode(&in)
		busy := []map[string]string{}
		add := func(s, e time.Time) {
			if s.Before(in.TimeMax) && e.After(in.TimeMin) {
				busy = append(busy, map[string]string{"start": s.UTC().Format(time.RFC3339), "end": e.UTC().Format(time.RFC3339)})
			}
		}
		for _, b := range f.busy {
			add(b.start, b.end)
		}
		for _, ev := range f.events { // WantMyTime's own events are busy time too
			if ev.status != "cancelled" {
				add(ev.start, ev.end)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"calendars": map[string]any{"primary": map[string]any{"busy": busy}}})
	case strings.HasPrefix(r.URL.Path, "/calendar/v3/calendars/primary/events"):
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(401)
			return
		}
		id := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/calendar/v3/calendars/primary/events"), "/")
		var body struct {
			ID      string `json:"id"`
			Summary string `json:"summary"`
			Status  string `json:"status"`
			Start   struct {
				DateTime time.Time `json:"dateTime"`
			} `json:"start"`
			End struct {
				DateTime time.Time `json:"dateTime"`
			} `json:"end"`
			Attendees      []any           `json:"attendees"`
			ConferenceData json.RawMessage `json:"conferenceData"`
		}
		if r.Method != http.MethodDelete {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		respond := func(id string, ev fakeEvent) {
			out := map[string]any{"id": id, "status": ev.status}
			if ev.hasMeet {
				if f.meetPending > 0 {
					f.meetPending--
					out["conferenceData"] = map[string]any{"createRequest": map[string]any{"status": map[string]string{"statusCode": "pending"}}}
				} else {
					out["hangoutLink"] = "https://meet.google.com/abc-defg-hij"
					out["conferenceData"] = map[string]any{"entryPoints": []map[string]string{{"entryPointType": "video", "uri": "https://meet.google.com/abc-defg-hij"}}}
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		}
		switch r.Method {
		case http.MethodPost:
			if _, exists := f.events[body.ID]; exists {
				w.WriteHeader(409)
				return
			}
			ev := fakeEvent{start: body.Start.DateTime, end: body.End.DateTime, summary: body.Summary, hasMeet: len(body.ConferenceData) > 0, attendees: len(body.Attendees), status: "confirmed"}
			f.events[body.ID] = ev
			respond(body.ID, ev)
		case http.MethodPatch:
			ev, ok := f.events[id]
			if !ok {
				w.WriteHeader(404)
				return
			}
			ev.start, ev.end, ev.summary, ev.status = body.Start.DateTime, body.End.DateTime, body.Summary, "confirmed"
			ev.hasMeet = ev.hasMeet || len(body.ConferenceData) > 0
			f.events[id] = ev
			respond(id, ev)
		case http.MethodDelete:
			if _, ok := f.events[id]; !ok {
				w.WriteHeader(410)
				return
			}
			delete(f.events, id)
			w.WriteHeader(204)
		}
	default:
		w.WriteHeader(404)
	}
}

func (f *fakeGoogle) event(id string) (fakeEvent, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ev, ok := f.events[id]
	return ev, ok
}

// connectCalendar runs the consent flow for a seller and returns the outcome
// code from the final redirect.
func connectCalendar(t *testing.T, h *harness, f *fakeGoogle, c *client, code string) string {
	t.Helper()
	start := c.expect(200, "POST", "/api/v1/me/calendar/google", "{}")
	consent, err := url.Parse(start["authorization_url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	q := consent.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" || !strings.Contains(q.Get("scope"), googleScopeFreeBusy) || q.Get("redirect_uri") != testOrigin+"/api/v1/integrations/google/callback" {
		t.Fatalf("consent URL: %s", consent)
	}
	f.mu.Lock()
	f.challenge = q.Get("code_challenge")
	f.mu.Unlock()
	req, _ := http.NewRequest("GET", h.server.URL+"/api/v1/integrations/google/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(q.Get("state")), nil)
	noFollow := &http.Client{Jar: c.http.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("callback status %d", res.StatusCode)
	}
	loc, _ := url.Parse(res.Header.Get("Location"))
	if loc.Path != "/app/settings/connections" {
		t.Fatalf("callback redirected to %s", loc)
	}
	return loc.Query().Get("google")
}

func runCalendarWorkerOnce(t *testing.T, h *harness) {
	t.Helper()
	if err := h.api.calendarCycle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleCalendarConnectBlocksBusyTimes(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	f := newFakeGoogle(t)
	s := h.newSeller("fixed")
	lagos, _ := time.LoadLocation("Africa/Lagos")
	tomorrow := time.Now().In(lagos).AddDate(0, 0, 1)
	busyStart := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 10, 0, 0, 0, lagos)
	f.busy = []busyInterval{{busyStart, busyStart.Add(time.Hour)}}

	status := s.client.expect(200, "GET", "/api/v1/me/calendar", nil)
	if status["configured"] != true || status["connected"] != false {
		t.Fatalf("status before connecting: %v", status)
	}
	if got := connectCalendar(t, h, f, s.client, "good-code"); got != "connected" {
		t.Fatalf("outcome %q", got)
	}
	status = s.client.expect(200, "GET", "/api/v1/me/calendar", nil)
	if status["connected"] != true || status["account_email"] != "seller@gmail.test" || status["last_synced_at"] == nil {
		t.Fatalf("status after connecting: %v", status)
	}
	stored := scalar[[]byte](t, `SELECT encrypted_refresh_token FROM calendar_connections c JOIN seller_profiles sp ON sp.id=c.seller_id WHERE sp.handle=$1`, s.handle)
	if strings.Contains(string(stored), "refresh-1") {
		t.Fatal("refresh token must be encrypted at rest")
	}

	// The busy hour is not offered and cannot be held.
	date := tomorrow.Format("2006-01-02")
	slots := h.client("").expect(200, "GET", "/api/v1/people/"+s.handle+"/slots?date="+date+"&duration=30", nil)["slots"].([]any)
	for _, raw := range slots {
		at, _ := time.Parse(time.RFC3339, raw.(map[string]any)["starts_at"].(string))
		if at.Before(busyStart.Add(time.Hour)) && busyStart.Before(at.Add(30*time.Minute)) {
			t.Fatalf("slot %s overlaps the Google busy hour", at.In(lagos))
		}
	}
	buyer := h.guestBuyer()
	res := buyer.do("POST", "/api/v1/quotes", map[string]any{"seller": s.handle, "name": "Buyer", "duration_minutes": 30, "starts_at": busyStart.Add(15 * time.Minute).UTC().Format(time.RFC3339)}, "Idempotency-Key", idempotencyKey())
	if res.Status != 409 {
		t.Fatalf("holding a busy time: %d %s", res.Status, res.Body)
	}
	// Turning busy checks off frees the time again.
	s.client.expect(200, "PATCH", "/api/v1/me/calendar", map[string]bool{"check_busy": false, "add_events": true, "create_meet_links": true})
	if n := scalar[int64](t, `SELECT count(*) FROM calendar_busy_blocks bb JOIN seller_profiles sp ON sp.id=bb.seller_id WHERE sp.handle=$1`, s.handle); n != 0 {
		t.Fatalf("busy blocks should be cleared, found %d", n)
	}
}

func TestGoogleCalendarConnectRejectsBadCallbacks(t *testing.T) {
	h := newHarness(t)
	f := newFakeGoogle(t)
	s := h.newSeller("fixed")
	// Missing a scope: the grant is revoked and the seller is told why.
	f.scope = "openid email " + googleScopeFreeBusy
	if got := connectCalendar(t, h, f, s.client, "good-code"); got != "permissions" {
		t.Fatalf("outcome %q", got)
	}
	if f.revokedRefs != 1 {
		t.Fatalf("partial grant should be revoked, revokes=%d", f.revokedRefs)
	}
	f.scope = "openid email " + googleScopeFreeBusy + " " + googleScopeEventsOwn
	// A bad code fails.
	if got := connectCalendar(t, h, f, s.client, "bad-code"); got != "failed" {
		t.Fatalf("outcome %q", got)
	}
	// Another account cannot complete someone else's flow.
	start := s.client.expect(200, "POST", "/api/v1/me/calendar/google", "{}")
	consent, _ := url.Parse(start["authorization_url"].(string))
	other := h.newSeller("fixed")
	req, _ := http.NewRequest("GET", h.server.URL+"/api/v1/integrations/google/callback?code=good-code&state="+url.QueryEscape(consent.Query().Get("state")), nil)
	noFollow := &http.Client{Jar: other.client.http.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if !strings.HasSuffix(res.Header.Get("Location"), "google=expired") {
		t.Fatalf("cross-account callback: %s", res.Header.Get("Location"))
	}
	if n := scalar[int64](t, `SELECT count(*) FROM calendar_connections c JOIN seller_profiles sp ON sp.id=c.seller_id WHERE sp.handle IN ($1,$2)`, s.handle, other.handle); n != 0 {
		t.Fatalf("no connection expected, got %d", n)
	}
	// Buyers without a link cannot start the flow.
	buyer := h.guestBuyer()
	if got := buyer.do("POST", "/api/v1/me/calendar/google", "{}").Status; got != 401 {
		t.Fatalf("guest start: %d", got)
	}
}

func TestBookingGetsCalendarEventAndMeetLink(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	f := newFakeGoogle(t)
	s := h.newSeller("fixed")
	if got := connectCalendar(t, h, f, s.client, "good-code"); got != "connected" {
		t.Fatalf("outcome %q", got)
	}
	f.meetPending = 1 // Google is still creating the conference on the first write
	buyer := h.guestBuyer()
	start := h.slot(s.handle, 0)
	_, bookingID := h.holdAndSimulate(buyer, s.handle, start)
	eventID := calendarEventID(bookingID)

	runCalendarWorkerOnce(t, h)
	ev, ok := f.event(eventID)
	if !ok {
		t.Fatalf("no calendar event created; calls %v", f.calls)
	}
	at, _ := time.Parse(time.RFC3339, start)
	if !ev.start.Equal(at) || !ev.end.Equal(at.Add(30*time.Minute)) || ev.attendees != 0 || !strings.Contains(ev.summary, "Buyer") {
		t.Fatalf("event %+v", ev)
	}
	if got := scalar[string](t, `SELECT state FROM calendar_jobs WHERE booking_id=$1 ORDER BY created_at DESC LIMIT 1`, bookingID); got != "queued" {
		t.Fatalf("job should wait for the Meet link, state %s", got)
	}
	if _, err := itPool.Exec(context.Background(), `UPDATE calendar_jobs SET due_at=now() WHERE booking_id=$1 AND state='queued'`, bookingID); err != nil {
		t.Fatal(err)
	}
	runCalendarWorkerOnce(t, h)
	if got := scalar[string](t, `SELECT COALESCE(meeting_source,'') FROM bookings WHERE id=$1`, bookingID); got != "google_meet" {
		t.Fatalf("meeting_source = %q", got)
	}
	booking := buyer.expect(200, "GET", "/api/v1/bookings/"+bookingID, nil)["booking"].(map[string]any)
	if booking["meeting_url"] != "https://meet.google.com/abc-defg-hij" {
		t.Fatalf("buyer should see the Meet link, got %v", booking["meeting_url"])
	}
	if n := scalar[int64](t, `SELECT count(*) FROM notification_outbox WHERE booking_id=$1 AND kind='meeting_link_ready_buyer'`, bookingID); n != 1 {
		t.Fatalf("meeting-link email not queued (%d)", n)
	}

	// Rescheduling moves the event; the Meet link stays.
	newStart := h.slot(s.handle, 8)
	req := buyer.expect(201, "POST", "/api/v1/bookings/"+bookingID+"/reschedules", map[string]string{"proposed_starts_at": newStart})
	s.client.expect(200, "POST", "/api/v1/reschedules/"+req["id"].(string)+"/accept", "{}")
	runCalendarWorkerOnce(t, h)
	moved, _ := f.event(eventID)
	want, _ := time.Parse(time.RFC3339, newStart)
	if !moved.start.Equal(want) || !moved.hasMeet {
		t.Fatalf("event after reschedule %+v, want start %s", moved, want)
	}
	if got := scalar[string](t, `SELECT COALESCE(calendar_event_id,'') FROM bookings WHERE id=$1`, bookingID); got != eventID {
		t.Fatalf("calendar_event_id = %q", got)
	}
}

func TestSellerMeetingLinkWinsOverMeet(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	f := newFakeGoogle(t)
	s := h.newSeller("fixed")
	connectCalendar(t, h, f, s.client, "good-code")
	buyer := h.guestBuyer()
	_, bookingID := h.holdAndSimulate(buyer, s.handle, h.slot(s.handle, 0))
	s.client.expect(200, "PATCH", "/api/v1/bookings/"+bookingID+"/meeting", map[string]string{"meeting_url": "https://zoom.example/j/123"})
	runCalendarWorkerOnce(t, h)
	ev, ok := f.event(calendarEventID(bookingID))
	if !ok || ev.hasMeet {
		t.Fatalf("event should exist without a Meet conference: %+v %v", ev, ok)
	}
	if got := scalar[string](t, `SELECT meeting_source FROM bookings WHERE id=$1`, bookingID); got != "seller" {
		t.Fatalf("meeting_source = %s", got)
	}
}

func TestRevokedGoogleAccessIsShownAndDisconnectCleansUp(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	f := newFakeGoogle(t)
	s := h.newSeller("fixed")
	connectCalendar(t, h, f, s.client, "good-code")
	sellerID := scalar[string](t, `SELECT id::text FROM seller_profiles WHERE handle=$1`, s.handle)
	h.api.calTokens.drop(sellerID)
	f.mu.Lock()
	f.grantBroken = true
	f.mu.Unlock()
	buyer := h.guestBuyer()
	_, bookingID := h.holdAndSimulate(buyer, s.handle, h.slot(s.handle, 0))
	runCalendarWorkerOnce(t, h)
	status := s.client.expect(200, "GET", "/api/v1/me/calendar", nil)
	if status["status"] != "revoked" || !strings.Contains(fmt.Sprint(status["last_error"]), "Reconnect") {
		t.Fatalf("status after revocation: %v", status)
	}
	if got := scalar[string](t, `SELECT state FROM calendar_jobs WHERE booking_id=$1`, bookingID); got != "done" {
		t.Fatalf("job state %s", got)
	}

	f.mu.Lock()
	f.grantBroken = false
	revokesBefore := f.revokedRefs
	f.mu.Unlock()
	s.client.expect(200, "DELETE", "/api/v1/me/calendar", nil)
	if f.revokedRefs != revokesBefore+1 {
		t.Fatal("disconnect should revoke the Google grant")
	}
	if n := scalar[int64](t, `SELECT count(*) FROM calendar_connections WHERE seller_id=$1`, sellerID); n != 0 {
		t.Fatal("connection should be deleted")
	}
	if n := scalar[int64](t, `SELECT count(*) FROM audit_events WHERE target_id=$1 AND action IN ('calendar.connected','calendar.disconnected')`, sellerID); n != 2 {
		t.Fatalf("expected connect and disconnect audit events, got %d", n)
	}
}

func TestMovedBookingIgnoresItsOwnCalendarEvent(t *testing.T) {
	h := newHarness(t)
	t.Setenv("LOCAL_PAYMENT_SIMULATOR", "true")
	s := h.newSeller("fixed")
	buyer := h.guestBuyer()
	start := h.slot(s.handle, 0)
	_, bookingID := h.holdAndSimulate(buyer, s.handle, start)
	sellerID := scalar[string](t, `SELECT id::text FROM seller_profiles WHERE handle=$1`, s.handle)
	at, _ := time.Parse(time.RFC3339, start)
	if _, err := itPool.Exec(context.Background(), `INSERT INTO calendar_busy_blocks(seller_id,busy) VALUES($1,tstzrange($2,$3,'[)'))`, sellerID, at, at.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	tx, err := itPool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	probe := at.Add(15 * time.Minute)
	if busy, _ := calendarConflict(context.Background(), tx, sellerID, probe, probe.Add(30*time.Minute), nil); !busy {
		t.Fatal("the busy block should conflict for other bookings")
	}
	if busy, _ := calendarConflict(context.Background(), tx, sellerID, probe, probe.Add(30*time.Minute), &bookingID); busy {
		t.Fatal("a booking's own event must not block moving it")
	}
}
