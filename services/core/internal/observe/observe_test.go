package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAcceptRequestID(t *testing.T) {
	for _, good := range []string{"abcdef12", "3f2a9c1d-77aa-4b0e-9a1b-0c0d0e0f1011", "req.id_01"} {
		if got := acceptRequestID(good); got != good {
			t.Errorf("acceptRequestID(%q) = %q, want it kept", good, got)
		}
	}
	for _, bad := range []string{"", "short", "has space in it", "inject\nnewline0", strings.Repeat("a", 65), "<script>alert</script>"} {
		got := acceptRequestID(bad)
		if got == bad || len(got) != 32 {
			t.Errorf("acceptRequestID(%q) = %q, want a fresh 32-char ID", bad, got)
		}
	}
}

func TestNewReporterDSN(t *testing.T) {
	cases := map[string]string{
		"https://key@o1.ingest.sentry.io/42":         "https://o1.ingest.sentry.io/api/42/envelope/",
		"https://key@sentry.example.com/prefix/7":    "https://sentry.example.com/prefix/api/7/envelope/",
		"http://key@127.0.0.1:9000/deep/prefix/1234": "http://127.0.0.1:9000/deep/prefix/api/1234/envelope/",
	}
	for dsn, want := range cases {
		r, err := NewReporter(dsn, "test", "")
		if err != nil {
			t.Fatalf("%s: %v", dsn, err)
		}
		if r.endpoint != want || r.publicKey != "key" {
			t.Errorf("%s: endpoint %q key %q", dsn, r.endpoint, r.publicKey)
		}
		r.Close(time.Second)
	}
	if r, err := NewReporter("", "test", ""); r != nil || err != nil {
		t.Fatal("empty DSN must disable reporting without error")
	}
	for _, bad := range []string{"not a url", "https://o1.ingest.sentry.io/42", "https://key@host/"} {
		if _, err := NewReporter(bad, "test", ""); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
	var nilReporter *Reporter // nil receivers must be safe everywhere
	nilReporter.CaptureMessage(context.Background(), "error", "x", nil)
	nilReporter.CapturePanic(context.Background(), "x", nil)
	nilReporter.Close(time.Millisecond)
}

// fakeSentry records envelopes posted to it.
type fakeSentry struct {
	mu     sync.Mutex
	events []map[string]any
	auth   []string
}

func (f *fakeSentry) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	lines := bytes.Split(bytes.TrimSpace(body), []byte("\n"))
	var event map[string]any
	if len(lines) == 3 {
		_ = json.Unmarshal(lines[2], &event)
	}
	f.mu.Lock()
	f.events = append(f.events, event)
	f.auth = append(f.auth, r.Header.Get("X-Sentry-Auth"))
	f.mu.Unlock()
}

func (f *fakeSentry) snapshot() ([]map[string]any, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.events...), append([]string(nil), f.auth...)
}

func newTestReporter(t *testing.T) (*Reporter, *fakeSentry) {
	fake := &fakeSentry{}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(server.Close)
	r, err := NewReporter("http://pubkey@"+strings.TrimPrefix(server.URL, "http://")+"/5", "test", "v1")
	if err != nil {
		t.Fatal(err)
	}
	return r, fake
}

func TestReporterSendsAndThrottles(t *testing.T) {
	r, fake := newTestReporter(t)
	ctx := WithRequestID(context.Background(), "request-id-0001")
	r.CaptureMessage(ctx, "error", "database down", map[string]string{"route": "GET /x"})
	r.CaptureMessage(ctx, "error", "database down", map[string]string{"route": "GET /x"}) // throttled
	r.CaptureMessage(ctx, "error", "database down", map[string]string{"route": "GET /y"}) // different route
	r.Close(2 * time.Second)
	events, auth := fake.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (one throttled)", len(events))
	}
	if !strings.Contains(auth[0], "sentry_key=pubkey") {
		t.Errorf("auth header %q", auth[0])
	}
	tags, _ := events[0]["tags"].(map[string]any)
	if tags["request_id"] != "request-id-0001" || events[0]["release"] != "v1" || events[0]["environment"] != "test" {
		t.Errorf("event missing context: %v", events[0])
	}
}

func TestMiddlewareRequestIDPanicAndMetrics(t *testing.T) {
	r, fake := newTestReporter(t)
	var logs bytes.Buffer
	logger := NewLogger(&logs, r)
	metrics := NewMetrics()
	metrics.AddGauges(func(context.Context) ([]Gauge, error) {
		return []Gauge{{Name: "aside_test_gauge", Help: "Test.", Labels: map[string]string{"k": `a"b`}, Value: 3}}, nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ok/{id}", func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) == "" {
			t.Error("handler context has no request ID")
		}
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("kaboom") })
	mux.HandleFunc("GET /fail", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) })
	mux.Handle("GET /metrics", metrics.Handler("s3cret"))
	server := httptest.NewServer(Middleware(mux, logger, r, metrics))
	defer server.Close()

	get := func(path string, headers ...string) *http.Response {
		req, _ := http.NewRequest("GET", server.URL+path, nil)
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}

	res := get("/ok/1", "X-Request-ID", "upstream-id-123")
	if res.Header.Get("X-Request-ID") != "upstream-id-123" {
		t.Errorf("upstream request ID not kept: %q", res.Header.Get("X-Request-ID"))
	}
	res = get("/ok/2", "X-Request-ID", "bad id")
	if id := res.Header.Get("X-Request-ID"); len(id) != 32 {
		t.Errorf("malformed upstream ID should be replaced, got %q", id)
	}

	res = get("/boom")
	if res.StatusCode != 500 {
		t.Fatalf("panic status %d", res.StatusCode)
	}
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	id := res.Header.Get("X-Request-ID")
	if body.Error.Code != "INTERNAL_ERROR" || !strings.Contains(body.Error.Message, id) || strings.Contains(body.Error.Message, "kaboom") {
		t.Errorf("panic body %+v (id %s)", body.Error, id)
	}
	get("/fail")

	if res = get("/metrics"); res.StatusCode != 401 {
		t.Errorf("metrics without token: %d", res.StatusCode)
	}
	if res = get("/metrics", "Authorization", "Bearer wrong"); res.StatusCode != 401 {
		t.Errorf("metrics with wrong token: %d", res.StatusCode)
	}
	res = get("/metrics", "Authorization", "Bearer s3cret")
	text, _ := io.ReadAll(res.Body)
	for _, want := range []string{
		`aside_http_requests_total{method="GET",route="GET /ok/{id}",status="2xx"} 2`,
		`aside_http_requests_total{method="GET",route="GET /boom",status="5xx"} 1`,
		`aside_http_request_duration_seconds_count{route="GET /ok/{id}"} 2`,
		`aside_test_gauge{k="a\"b"} 3`,
	} {
		if !strings.Contains(string(text), want) {
			t.Errorf("metrics missing %s\n%s", want, text)
		}
	}
	if strings.Contains(string(text), "/ok/1") {
		t.Error("metrics must use route patterns, not raw paths")
	}

	r.Close(2 * time.Second)
	events, _ := fake.snapshot()
	panics, warnings := 0, 0
	for _, e := range events {
		if _, ok := e["exception"]; ok {
			panics++
		} else if e["level"] == "warning" {
			warnings++
		}
	}
	if panics != 1 || warnings != 1 || len(events) != 2 {
		t.Errorf("want exactly one panic event and one 503 warning, got %d events: %v", len(events), events)
	}

	var sawPanicLog bool
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %s", line)
		}
		if entry["msg"] == "panic recovered" {
			sawPanicLog = entry["request_id"] == id
		}
	}
	if !sawPanicLog {
		t.Error("panic log line missing or without the request ID")
	}
}

func TestMetricsDisabledWithoutToken(t *testing.T) {
	rec := httptest.NewRecorder()
	NewMetrics().Handler("")(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 404 {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestLoggerLevels(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, nil)
	logger.Debug("hidden")
	logger.InfoContext(WithRequestID(context.Background(), "abcdefgh1"), "shown")
	out := buf.String()
	if strings.Contains(out, "hidden") || !strings.Contains(out, `"request_id":"abcdefgh1"`) {
		t.Fatalf("unexpected log output: %s", out)
	}
	t.Setenv("LOG_LEVEL", "debug")
	buf.Reset()
	NewLogger(&buf, nil).Debug("now shown")
	if !strings.Contains(buf.String(), "now shown") {
		t.Fatal("LOG_LEVEL=debug did not enable debug logs")
	}
}
