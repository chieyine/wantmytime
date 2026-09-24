package main

import (
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// rateLimiter is a fixed-window, per-client limiter held in process memory.
// It protects a single API instance; run a shared limiter (for example at the
// ingress) when several API instances serve traffic.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*rateWindow
	swept  time.Time
}

type rateWindow struct {
	start time.Time
	count int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: map[string]*rateWindow{}}
}

func (l *rateLimiter) allow(key string, now time.Time) bool {
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
		if !l.allow(clientIP(r), time.Now()) {
			w.Header().Set("Retry-After", "60")
			problem(w, 429, "RATE_LIMITED", "Too many requests. Wait a minute and try again.")
			return
		}
		next(w, r)
	}
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
