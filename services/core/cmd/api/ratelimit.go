package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// rateLimiter is a fixed-window, per-client limiter. With REDIS_URL set, the
// count is shared by every API instance through Redis. Without Redis, or
// whenever Redis does not answer within its short timeout, the limiter falls
// back to a counter in this process, so a Redis outage never takes the API
// down and never switches limits off entirely.
type rateLimiter struct {
	name   string
	limit  int
	window time.Duration
	shared *redisClient
	onFail func(error)
	onDeny func()

	mu    sync.Mutex
	hits  map[string]*rateWindow
	swept time.Time
}

type rateWindow struct {
	start time.Time
	count int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{name: "local", limit: limit, window: window, hits: map[string]*rateWindow{}}
}

// limiter returns a named limiter that shares its counts through Redis when
// it is configured.
func (a *API) limiter(name string, limit int, window time.Duration) *rateLimiter {
	if a.rateLimitScale > 1 {
		limit *= a.rateLimitScale
	}
	l := newRateLimiter(limit, window)
	l.name = name
	l.shared = a.redis
	l.onFail = func(err error) {
		a.metrics.Count("aside_rate_limit_redis_errors_total", name)
		a.log().Warn("shared rate limit unavailable; using local limit", "limiter", name, "error", err.Error())
	}
	l.onDeny = func() { a.metrics.Count("aside_rate_limited_total", name) }
	return l
}

func (l *rateLimiter) allow(key string, now time.Time) bool {
	return l.allowCtx(context.Background(), key, now)
}

func (l *rateLimiter) allowCtx(ctx context.Context, key string, now time.Time) bool {
	ok := l.check(ctx, key, now)
	if !ok && l.onDeny != nil {
		l.onDeny()
	}
	return ok
}

func (l *rateLimiter) check(ctx context.Context, key string, now time.Time) bool {
	if l.shared != nil {
		n, err := l.shared.incrWindow(ctx, l.sharedKey(key, now), l.window)
		if err == nil {
			return n <= int64(l.limit)
		}
		if l.onFail != nil {
			l.onFail(err)
		}
	}
	return l.allowLocal(key, now)
}

// sharedKey hashes the client key so Redis never holds raw IP addresses.
func (l *rateLimiter) sharedKey(key string, now time.Time) string {
	sum := sha256.Sum256([]byte(l.name + "|" + key))
	slot := now.UnixMilli() / l.window.Milliseconds()
	return "wmt:rl:" + l.name + ":" + strconv.FormatInt(slot, 10) + ":" + hex.EncodeToString(sum[:12])
}

func (l *rateLimiter) allowLocal(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.swept) > l.window {
		for k, v := range l.hits {
			if now.Sub(v.start) >= l.window {
				delete(l.hits, k)
			}
		}
		l.swept = now
	}
	entry, ok := l.hits[key]
	if !ok || now.Sub(entry.start) >= l.window {
		l.hits[key] = &rateWindow{start: now, count: 1}
		return true
	}
	if entry.count >= l.limit {
		return false
	}
	entry.count++
	return true
}

// clientIP uses the connection address. When TRUST_PROXY_HEADERS=true (the API
// is only reachable through the bundled Nginx gateway), it uses the right-most
// X-Forwarded-For entry, which is the address the gateway itself observed.
func clientIP(r *http.Request) string {
	if os.Getenv("TRUST_PROXY_HEADERS") == "true" {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *API) rateLimited(l *rateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.allowCtx(r.Context(), clientIP(r), time.Now()) {
			w.Header().Set("Retry-After", strconv.Itoa(int(l.window.Seconds())))
			problem(w, 429, "RATE_LIMITED", "Too many requests. Wait a little and try again.")
			return
		}
		next(w, r)
	}
}

// writeLimited caps every state-changing API request per client address, on
// top of the stricter limits on individual routes. Provider webhooks are exempt:
// they come from a few provider addresses and are verified by signature.
func (a *API) writeLimited(l *rateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if strings.HasPrefix(r.URL.Path, "/api/v1/") && !strings.HasPrefix(r.URL.Path, "/api/v1/webhooks/") && !l.allowCtx(r.Context(), clientIP(r), time.Now()) {
				w.Header().Set("Retry-After", strconv.Itoa(int(l.window.Seconds())))
				problem(w, 429, "RATE_LIMITED", "Too many requests. Wait a little and try again.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

func requireUUIDPath(name string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !validUUID(r.PathValue(name)) {
			problem(w, 404, "NOT_FOUND", "This record is not available.")
			return
		}
		next(w, r)
	}
}
