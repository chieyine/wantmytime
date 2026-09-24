package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Google Calendar integration: busy times for availability, an event per
// booking and a Google Meet link. Scopes are the narrowest that do the job:
// free/busy only (no event details are read) and events on calendars the
// seller owns.
const (
	googleScopeFreeBusy   = "https://www.googleapis.com/auth/calendar.freebusy"
	googleScopeEventsOwn  = "https://www.googleapis.com/auth/calendar.events.owned"
	googleRequestedScopes = "openid email " + googleScopeFreeBusy + " " + googleScopeEventsOwn
)

var (
	errGoogleNotConfigured = errors.New("google calendar is not configured")
	// errCalendarRevoked means the seller removed access or the grant expired;
	// only reconnecting fixes it.
	errCalendarRevoked = errors.New("google calendar access was revoked")
	errGoogleNotFound  = errors.New("google calendar item not found")
	errGoogleConflict  = errors.New("google calendar item already exists")
)

type googleClient struct {
	clientID, clientSecret string
	authBase               string // consent screen
	oauthBase              string // token and revoke endpoints
	apiBase                string // Calendar API
	http                   *http.Client
}

// googleCalendarConfigured reports whether sellers can connect Google Calendar.
func (a *API) googleCalendarConfigured() bool {
	if os.Getenv("GOOGLE_CLIENT_ID") == "" || os.Getenv("GOOGLE_CLIENT_SECRET") == "" {
		return false
	}
	_, err := calendarTokenKey(a)
	return err == nil
}

func (a *API) newGoogleClient() (*googleClient, error) {
	if !a.googleCalendarConfigured() {
		return nil, errGoogleNotConfigured
	}
	c := &googleClient{
		clientID: os.Getenv("GOOGLE_CLIENT_ID"), clientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		authBase: "https://accounts.google.com", oauthBase: "https://oauth2.googleapis.com", apiBase: "https://www.googleapis.com",
		http: &http.Client{Timeout: 10 * time.Second},
	}
	// Test doubles only; production always talks to Google.
	if a.env != "production" {
		if v := os.Getenv("GOOGLE_AUTH_BASE"); v != "" {
			c.authBase = strings.TrimRight(v, "/")
		}
		if v := os.Getenv("GOOGLE_OAUTH_BASE"); v != "" {
			c.oauthBase = strings.TrimRight(v, "/")
		}
		if v := os.Getenv("GOOGLE_API_BASE"); v != "" {
			c.apiBase = strings.TrimRight(v, "/")
		}
	}
	return c, nil
}

func googleRedirectURI() string {
	return appOrigin() + "/api/v1/integrations/google/callback"
}

// authURL builds the consent URL with PKCE (S256) and offline access so a
// refresh token is issued.
func (c *googleClient) authURL(state, verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id":             {c.clientID},
		"redirect_uri":          {googleRedirectURI()},
		"response_type":         {"code"},
		"scope":                 {googleRequestedScopes},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	return c.authBase + "/o/oauth2/v2/auth?" + q.Encode()
}

type googleTokens struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token"`
	Error        string `json:"error"`
}

func (c *googleClient) token(ctx context.Context, form url.Values) (googleTokens, error) {
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.oauthBase+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return googleTokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return googleTokens{}, err
	}
	defer resp.Body.Close()
	var out googleTokens
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return googleTokens{}, fmt.Errorf("google token response unreadable (status %d)", resp.StatusCode)
	}
	if out.Error == "invalid_grant" {
		return out, errCalendarRevoked
	}
	if resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		return out, fmt.Errorf("google token request failed (status %d, %s)", resp.StatusCode, out.Error)
	}
	return out, nil
}

func (c *googleClient) exchange(ctx context.Context, code, verifier string) (googleTokens, error) {
	return c.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {googleRedirectURI()}})
}

func (c *googleClient) refresh(ctx context.Context, refreshToken string) (googleTokens, error) {
	return c.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
}

func (c *googleClient) revoke(ctx context.Context, token string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.oauthBase+"/revoke", strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if resp, err := c.http.Do(req); err == nil {
		_ = resp.Body.Close()
	}
}

// idTokenEmail reads the verified email from an ID token received directly
// from Google's token endpoint over TLS (OpenID Connect Core §3.1.3.7 allows
// skipping signature checks for tokens obtained this way).
func idTokenEmail(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if json.Unmarshal(payload, &claims) != nil || !claims.EmailVerified {
		return ""
	}
	return strings.ToLower(claims.Email)
}

func grantedScopes(scope string) map[string]bool {
	out := map[string]bool{}
	for _, s := range strings.Fields(scope) {
		out[s] = true
	}
	return out
}

// api calls the Calendar API with an access token and decodes JSON into out.
func (c *googleClient) api(ctx context.Context, access, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return errGoogleNotFound
	case resp.StatusCode == http.StatusConflict:
		return errGoogleConflict
	case resp.StatusCode == http.StatusUnauthorized:
		return errCalendarRevoked
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return fmt.Errorf("google calendar %s %s failed with status %d", method, strings.SplitN(path, "?", 2)[0], resp.StatusCode)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

type busyInterval struct{ start, end time.Time }

// freeBusy returns the busy intervals on the primary calendar, querying in
// 30-day chunks.
func (c *googleClient) freeBusy(ctx context.Context, access string, from, to time.Time) ([]busyInterval, error) {
	var out []busyInterval
	for chunk := from; chunk.Before(to); chunk = chunk.AddDate(0, 0, 30) {
		end := chunk.AddDate(0, 0, 30)
		if end.After(to) {
			end = to
		}
		var resp struct {
			Calendars map[string]struct {
				Busy []struct {
					Start time.Time `json:"start"`
					End   time.Time `json:"end"`
				} `json:"busy"`
				Errors []struct {
					Reason string `json:"reason"`
				} `json:"errors"`
			} `json:"calendars"`
		}
		body := map[string]any{"timeMin": chunk.UTC().Format(time.RFC3339), "timeMax": end.UTC().Format(time.RFC3339), "items": []map[string]string{{"id": "primary"}}}
		if err := c.api(ctx, access, http.MethodPost, "/calendar/v3/freeBusy", body, &resp); err != nil {
			return nil, err
		}
		cal, ok := resp.Calendars["primary"]
		if !ok {
			return nil, errors.New("google free/busy response has no primary calendar")
		}
		if len(cal.Errors) > 0 {
			return nil, fmt.Errorf("google free/busy error: %s", cal.Errors[0].Reason)
		}
		for _, b := range cal.Busy {
			out = append(out, busyInterval{b.Start, b.End})
		}
	}
	return out, nil
}

type googleEvent struct {
	ID             string `json:"id,omitempty"`
	Status         string `json:"status,omitempty"`
	HangoutLink    string `json:"hangoutLink,omitempty"`
	ConferenceData *struct {
		CreateRequest *struct {
			Status struct {
				StatusCode string `json:"statusCode"`
			} `json:"status"`
		} `json:"createRequest"`
		EntryPoints []struct {
			EntryPointType string `json:"entryPointType"`
			URI            string `json:"uri"`
		} `json:"entryPoints"`
	} `json:"conferenceData,omitempty"`
}

// meetLink returns the Meet URL once Google has created it, and whether the
// conference is still being set up.
func (e googleEvent) meetLink() (link string, pending bool) {
	if e.ConferenceData != nil {
		for _, ep := range e.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" && strings.HasPrefix(ep.URI, "https://") {
				return ep.URI, false
			}
		}
		if e.ConferenceData.CreateRequest != nil && e.ConferenceData.CreateRequest.Status.StatusCode == "pending" {
			return "", true
		}
	}
	if strings.HasPrefix(e.HangoutLink, "https://") {
		return e.HangoutLink, false
	}
	return "", false
}

const eventsPath = "/calendar/v3/calendars/primary/events"

func (c *googleClient) insertEvent(ctx context.Context, access string, body map[string]any) (googleEvent, error) {
	var ev googleEvent
	err := c.api(ctx, access, http.MethodPost, eventsPath+"?conferenceDataVersion=1&sendUpdates=none", body, &ev)
	return ev, err
}

func (c *googleClient) patchEvent(ctx context.Context, access, id string, body map[string]any) (googleEvent, error) {
	var ev googleEvent
	err := c.api(ctx, access, http.MethodPatch, eventsPath+"/"+url.PathEscape(id)+"?conferenceDataVersion=1&sendUpdates=none", body, &ev)
	return ev, err
}

func (c *googleClient) deleteEvent(ctx context.Context, access, id string) error {
	err := c.api(ctx, access, http.MethodDelete, eventsPath+"/"+url.PathEscape(id)+"?sendUpdates=none", nil, nil)
	if errors.Is(err, errGoogleNotFound) {
		return nil
	}
	return err
}

// calendarEventID derives a stable Google event ID from a booking ID, so a
// retried create finds the event instead of making a second one. Google IDs
// use base32hex characters (a-v, 0-9); hex digits are a subset.
func calendarEventID(bookingID string) string {
	return "aside" + strings.ReplaceAll(strings.ToLower(bookingID), "-", "")
}

// accessTokens caches short-lived access tokens per seller in memory.
type accessTokens struct {
	mu     sync.Mutex
	tokens map[string]cachedToken
}

type cachedToken struct {
	value   string
	expires time.Time
}

func (t *accessTokens) get(seller string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.tokens[seller]; ok && time.Now().Before(c.expires) {
		return c.value
	}
	return ""
}

func (t *accessTokens) put(seller, value string, ttl time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tokens == nil {
		t.tokens = map[string]cachedToken{}
	}
	t.tokens[seller] = cachedToken{value, time.Now().Add(ttl)}
}

func (t *accessTokens) drop(seller string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.tokens, seller)
}
