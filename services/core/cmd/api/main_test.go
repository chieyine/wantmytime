package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidHandle(t *testing.T) {
	good := []string{"tomi", "ada-ola", "user2026"}
	for _, h := range good {
		if !validHandle(h) {
			t.Errorf("expected %q valid", h)
		}
	}
	bad := []string{"api", "a", "-name", "name-", "two--hyphens", "UPPER", "with space", "a_underscore", "book", "verify", "unsubscribe", "acceptable-use"}
	for _, h := range bad {
		if validHandle(h) {
			t.Errorf("expected %q invalid", h)
		}
	}
}

func TestProfileValidationNeverAcceptsClientReadiness(t *testing.T) {
	p := Person{Handle: "tomi", Name: "Tomi", Mode: "fixed", Base30: 1500000, Durations: []int{15, 30}, Ready: true, Timezone: "Africa/Lagos"}
	if err := validatePerson(&p); err != nil {
		t.Fatal(err)
	}
	if p.Ready {
		t.Fatal("client must not self-attest payment readiness")
	}
}

func TestProfileValidationModes(t *testing.T) {
	both := Person{Handle: "tomi", Name: "Tomi", Mode: "both", Base30: 1500000, Timezone: "Africa/Lagos"}
	if err := validatePerson(&both); err != nil {
		t.Fatalf("fixed price with offers: %v", err)
	}
	if both.Base30 != 1500000 {
		t.Fatal("a seller taking both keeps their fixed price")
	}
	noPrice := Person{Handle: "tomi", Name: "Tomi", Mode: "both", Timezone: "Africa/Lagos"}
	if validatePerson(&noPrice) == nil {
		t.Fatal("a seller taking both needs a fixed price")
	}
	unknown := Person{Handle: "tomi", Name: "Tomi", Mode: "auction", Base30: 1500000, Timezone: "Africa/Lagos"}
	if validatePerson(&unknown) == nil {
		t.Fatal("unknown mode was accepted")
	}
}

func TestChallengeIDIsPostgresUUID(t *testing.T) {
	id, err := randomUUID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' || id[14] != '4' {
		t.Fatalf("unexpected UUID format %q", id)
	}
}

func TestTOTPMatchesRFC6238SHA1Vector(t *testing.T) {
	secret := []byte("12345678901234567890")
	if !validTOTP(secret, "287082", time.Unix(59, 0)) {
		t.Fatal("expected RFC 6238 SHA1 6-digit vector to verify")
	}
	if validTOTP(secret, "287083", time.Unix(59, 0)) {
		t.Fatal("incorrect authenticator code was accepted")
	}
}

func TestLocalInstantsHandleDaylightSaving(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	spring := time.Date(2026, time.March, 8, 0, 0, 0, 0, loc)
	if got := localInstants(spring, 2*60+30, loc); len(got) != 0 {
		t.Fatalf("nonexistent spring-forward time yielded %v", got)
	}
	fall := time.Date(2026, time.November, 1, 0, 0, 0, 0, loc)
	if got := localInstants(fall, 1*60+30, loc); len(got) != 2 {
		t.Fatalf("repeated fall-back time returned %d instants, want 2", len(got))
	}
	normal := time.Date(2026, time.February, 1, 0, 0, 0, 0, loc)
	if got := localInstants(normal, 10*60, loc); len(got) != 1 {
		t.Fatalf("ordinary local time returned %d instants, want 1", len(got))
	}
}

func TestOriginPolicyRejectsUntrustedAndOriginlessMutations(t *testing.T) {
	a := &API{}
	called := false
	h := a.cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))
	for _, tc := range []struct {
		origin string
		want   int
	}{{"https://attacker.example", http.StatusForbidden}, {"", http.StatusForbidden}, {"http://127.0.0.1:5173", http.StatusNoContent}} {
		called = false
		r := httptest.NewRequest(http.MethodPost, "/api/v1/me/link", nil)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("origin %q: got %d want %d", tc.origin, w.Code, tc.want)
		}
		if called != (tc.want == http.StatusNoContent) {
			t.Errorf("origin %q: handler called=%v", tc.origin, called)
		}
	}
}

func TestKoraSignatureCoversDataObject(t *testing.T) {
	data := `{"reference":"aside-abc-t1","status":"success","amount":15000}`
	body := []byte(`{"event":"charge.success","data":` + data + `}`)
	secret := "sk_test_secret"
	m := hmac.New(sha256.New, []byte(secret))
	_, _ = m.Write([]byte(data))
	signature := hex.EncodeToString(m.Sum(nil))
	if !validKoraSignature(body, signature, secret) {
		t.Fatal("expected valid signature")
	}
	// Whitespace around the data object does not matter; its content does.
	spaced := []byte("{\"event\": \"charge.success\", \"data\": " + data + "}")
	if !validKoraSignature(spaced, signature, secret) {
		t.Fatal("signature is over the data object, not the envelope")
	}
	tampered := []byte(`{"event":"charge.success","data":{"reference":"aside-abc-t1","status":"success","amount":15001}}`)
	if validKoraSignature(tampered, signature, secret) {
		t.Fatal("a changed amount must fail")
	}
	if validKoraSignature(body, "bad", secret) || validKoraSignature(body, signature, "") {
		t.Fatal("malformed signature or missing secret must fail")
	}
}

func TestKoraAmountsConvertWithoutFloatingPoint(t *testing.T) {
	for in, want := range map[string]int64{`10000`: 1000000, `"100.00"`: 10000, `22.5`: 2250, `1.69`: 169, `"0.07"`: 7, `1.6875`: 169, `null`: 0, `"12"`: 1200} {
		var m koraMoney
		if err := json.Unmarshal([]byte(in), &m); err != nil || int64(m) != want {
			t.Errorf("%s -> %d (%v), want %d", in, m, err, want)
		}
	}
	if got := majorAmount(1000000); got != "10000" {
		t.Errorf("majorAmount whole = %s", got)
	}
	if got := majorAmount(1050); got != "10.50" {
		t.Errorf("majorAmount fraction = %s", got)
	}
}

func TestBankTransferIsOfferedFirst(t *testing.T) {
	t.Setenv("APPROVED_PAYMENT_CHANNELS", "pay_with_bank, bank_transfer")
	got, err := approvedChannels()
	if err != nil || len(got) != 2 || got[0] != "bank_transfer" || got[1] != "pay_with_bank" {
		t.Fatalf("channels %v %v", got, err)
	}
	t.Setenv("APPROVED_PAYMENT_CHANNELS", "bank_transfer,card")
	if _, err = approvedChannels(); err == nil {
		t.Fatal("unsupported channel must be refused")
	}
}

func TestCheckoutAndWebhookFailClosed(t *testing.T) {
	for _, key := range []string{"PAYMENTS_ENABLED", "FEE_POLICY_APPROVED", "PAYMENT_ROUTE", "FEE_BPS", "FEE_POLICY_MODE", "MIN_CHARGE_MINOR", "MAX_CHARGE_MINOR", "APPROVED_PAYMENT_CHANNELS", "PAYMENT_ENV", "LIVE_PAYMENTS_ENABLED", "PAYMENT_APPROVAL_ID", "KORA_SECRET_KEY", "LOCAL_PAYMENT_SIMULATOR"} {
		t.Setenv(key, "")
	}
	// The real handlers must refuse before touching the database when payment
	// gates are not configured (a nil database would panic if they did not).
	a := &API{env: "production"}
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"webhook", a.koraWebhook, "/api/v1/webhooks/kora"},
		{"quote", a.createQuote, "/api/v1/quotes"},
		{"legacy booking", a.createBooking, "/api/v1/bookings"},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		tc.handler(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status=%d, want 503", tc.name, w.Code)
		}
	}
}

func TestParseClockMinutesAcceptsDatabaseFormat(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
		ok   bool
	}{{"10:00", 600, true}, {"10:15:00", 615, true}, {"23:45", 1425, true}, {"10:00:30", 0, false}, {"24:00", 0, false}, {"", 0, false}} {
		got, ok := parseClockMinutes(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("parseClockMinutes(%q) = %d,%v want %d,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestValidNameRejectsControlCharacters(t *testing.T) {
	if !validName("Adaeze Okafor") || validName("Ada\nBcc: x") || validName("") || validName(strings.Repeat("a", 81)) {
		t.Fatal("display name validation is wrong")
	}
}

func TestProductionConfigChecks(t *testing.T) {
	key := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	good := map[string]string{
		"SESSION_SECRET": "k8Jq2mV9xR4tW7yB1nC5eH3gL6pS0dZa", "OTP_PEPPER": "Q2w9E4r7T1y5U8i3O6p0A2s4D6f8G1hJ",
		"OPS_MFA_ENCRYPTION_KEY": key(1), "MEETING_LINK_ENCRYPTION_KEY": key(2), "PAYOUT_ACCOUNT_ENCRYPTION_KEY": key(3),
		"PUBLIC_APP_ORIGIN": "https://wantmytime.com", "REDIS_URL": "rediss://:pw@cache.example.net:6380",
		"MEDIA_S3_ENDPOINT": "https://acct.r2.cloudflarestorage.com", "DATABASE_URL": "postgres://u:p@db.example.net/wmt?sslmode=verify-full", "SENTRY_DSN": "https://k@o1.ingest.sentry.io/1",
	}
	pub, priv, err := generateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	good["VAPID_PUBLIC_KEY"] = pub
	good["VAPID_PRIVATE_KEY"] = priv
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if problems, warnings := productionConfigProblems(get(good)); len(problems) != 0 || len(warnings) != 0 {
		t.Fatalf("good config rejected: %v %v", problems, warnings)
	}
	bad := map[string]string{}
	for k, v := range good {
		bad[k] = v
	}
	bad["SESSION_SECRET"] = "local-only-session-secret-change-before-use"
	bad["MEETING_LINK_ENCRYPTION_KEY"] = key(1)
	bad["PUBLIC_APP_ORIGIN"] = "http://wantmytime.com"
	bad["LOCAL_PAYMENT_SIMULATOR"] = "true"
	bad["KORA_API_BASE"] = "http://127.0.0.1:9912"
	bad["GOOGLE_CLIENT_ID"] = "x.apps.googleusercontent.com"
	problems, _ := productionConfigProblems(get(bad))
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"SESSION_SECRET still holds a placeholder", "MEETING_LINK_ENCRYPTION_KEY reuses the value of OPS_MFA_ENCRYPTION_KEY", "PUBLIC_APP_ORIGIN", "LOCAL_PAYMENT_SIMULATOR", "KORA_API_BASE", "CALENDAR_TOKEN_ENCRYPTION_KEY"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing problem %q in:\n%s", want, joined)
		}
	}
}

func TestSiteOriginsIncludeTheWwwTwin(t *testing.T) {
	cases := map[string][]string{
		"https://wantmytime.com":     {"https://wantmytime.com", "https://www.wantmytime.com"},
		"https://wantmytime.com/":    {"https://wantmytime.com", "https://www.wantmytime.com"},
		"https://www.wantmytime.com": {"https://www.wantmytime.com", "https://wantmytime.com"},
		"https://app.wantmytime.com": {"https://app.wantmytime.com"},
		"http://127.0.0.1:5173":      {"http://127.0.0.1:5173"},
		"":                           nil,
	}
	for in, want := range cases {
		got := siteOrigins(in)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("siteOrigins(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSignInAcceptsTheWwwSite(t *testing.T) {
	t.Setenv("PUBLIC_APP_ORIGIN", "https://wantmytime.com")
	a := &API{env: "production"}
	handler := a.cors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for origin, want := range map[string]int{"https://www.wantmytime.com": 204, "https://wantmytime.com": 204, "https://evil.example": 403} {
		req := httptest.NewRequest("POST", "/api/v1/auth/challenges", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("origin %s: %d, want %d", origin, rec.Code, want)
		}
	}
}
