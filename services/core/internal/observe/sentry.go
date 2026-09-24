package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Reporter sends events to Sentry's envelope endpoint. It is safe for
// concurrent use, never blocks the caller, drops events when its queue is full,
// and throttles identical messages to one per minute. A nil *Reporter is valid
// and does nothing, so callers need not check whether SENTRY_DSN is set.
//
// Events carry the route pattern, status, request ID and a stack trace. They
// never include request bodies, headers, cookies or query strings.
type Reporter struct {
	endpoint    string
	publicKey   string
	environment string
	release     string
	server      string
	client      *http.Client
	queue       chan []byte
	done        chan struct{}
	mu          sync.Mutex
	lastSent    map[string]time.Time
	wg          sync.WaitGroup
}

// NewReporter parses a Sentry DSN (https://KEY@HOST/PROJECT). An empty DSN
// returns a nil reporter.
func NewReporter(dsn, environment, release string) (*Reporter, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, nil
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.User == nil || parsed.User.Username() == "" || parsed.Host == "" {
		return nil, errors.New("SENTRY_DSN is not a valid DSN")
	}
	// Self-hosted Sentry may serve under a path prefix: https://KEY@HOST/PREFIX/PROJECT.
	path := strings.Trim(parsed.Path, "/")
	prefix, project := "", path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		prefix, project = "/"+path[:i], path[i+1:]
	}
	if project == "" {
		return nil, errors.New("SENTRY_DSN has no project ID")
	}
	host, _ := os.Hostname()
	r := &Reporter{
		endpoint:    fmt.Sprintf("%s://%s%s/api/%s/envelope/", parsed.Scheme, parsed.Host, prefix, project),
		publicKey:   parsed.User.Username(),
		environment: environment,
		release:     release,
		server:      host,
		client:      &http.Client{Timeout: 5 * time.Second},
		queue:       make(chan []byte, 100),
		done:        make(chan struct{}),
		lastSent:    map[string]time.Time{},
	}
	r.wg.Add(1)
	go r.run()
	return r, nil
}

func (r *Reporter) run() {
	defer r.wg.Done()
	for {
		select {
		case body := <-r.queue:
			r.send(body)
		case <-r.done:
			for {
				select {
				case body := <-r.queue:
					r.send(body)
				default:
					return
				}
			}
		}
	}
}

func (r *Reporter) send(body []byte) {
	req, err := http.NewRequest(http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_client=aside-go/1.0, sentry_key="+r.publicKey)
	resp, err := r.client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// Close flushes queued events, waiting at most timeout.
func (r *Reporter) Close(timeout time.Duration) {
	if r == nil {
		return
	}
	close(r.done)
	finished := make(chan struct{})
	go func() { r.wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(timeout):
	}
}

type sentryFrame struct {
	Function string `json:"function"`
	Module   string `json:"module,omitempty"`
	Filename string `json:"filename"`
	Lineno   int    `json:"lineno"`
	InApp    bool   `json:"in_app"`
}

// CaptureMessage records a message at the given level ("error", "warning", "info").
func (r *Reporter) CaptureMessage(ctx context.Context, level, message string, tags map[string]string) {
	r.capture(ctx, level, "", message, tags, nil)
}

// CapturePanic records a recovered panic with the stack of the panicking goroutine.
func (r *Reporter) CapturePanic(ctx context.Context, value any, tags map[string]string) {
	r.capture(ctx, "fatal", "panic", fmt.Sprint(value), tags, stackFrames(4))
}

func (r *Reporter) capture(ctx context.Context, level, exceptionType, message string, tags map[string]string, frames []sentryFrame) {
	if r == nil {
		return
	}
	fingerprint := level + "|" + exceptionType + "|" + message + "|" + tags["route"]
	now := time.Now()
	r.mu.Lock()
	if last, ok := r.lastSent[fingerprint]; ok && now.Sub(last) < time.Minute {
		r.mu.Unlock()
		return
	}
	r.lastSent[fingerprint] = now
	if len(r.lastSent) > 1000 {
		r.lastSent = map[string]time.Time{fingerprint: now}
	}
	r.mu.Unlock()

	allTags := map[string]string{}
	for k, v := range tags {
		allTags[k] = v
	}
	if id := RequestID(ctx); id != "" {
		allTags["request_id"] = id
	}
	eventID := NewRequestID()
	event := map[string]any{
		"event_id":    eventID,
		"timestamp":   now.UTC().Format(time.RFC3339Nano),
		"level":       level,
		"platform":    "go",
		"logger":      "aside-api",
		"environment": r.environment,
		"server_name": r.server,
		"tags":        allTags,
	}
	if r.release != "" {
		event["release"] = r.release
	}
	if exceptionType != "" {
		exception := map[string]any{"type": exceptionType, "value": message}
		if len(frames) > 0 {
			exception["stacktrace"] = map[string]any{"frames": frames}
		}
		event["exception"] = map[string]any{"values": []any{exception}}
	} else {
		event["message"] = map[string]any{"formatted": message}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	header, _ := json.Marshal(map[string]any{"event_id": eventID, "sent_at": now.UTC().Format(time.RFC3339Nano)})
	item, _ := json.Marshal(map[string]any{"type": "event", "length": len(payload)})
	var body bytes.Buffer
	body.Write(header)
	body.WriteByte('\n')
	body.Write(item)
	body.WriteByte('\n')
	body.Write(payload)
	body.WriteByte('\n')
	select {
	case r.queue <- body.Bytes():
	default: // queue full: drop rather than slow the request path
	}
}

func stackFrames(skip int) []sentryFrame {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(skip, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var out []sentryFrame
	for {
		f, more := frames.Next()
		module := ""
		function := f.Function
		if i := strings.LastIndex(function, "/"); i >= 0 {
			if j := strings.Index(function[i:], "."); j >= 0 {
				module, function = function[:i+j], function[i+j+1:]
			}
		} else if j := strings.Index(function, "."); j >= 0 {
			module, function = function[:j], function[j+1:]
		}
		out = append(out, sentryFrame{Function: function, Module: module, Filename: f.File, Lineno: f.Line, InApp: strings.HasPrefix(module, "aside/") || module == "main"})
		if !more {
			break
		}
	}
	// Sentry expects the oldest frame first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
