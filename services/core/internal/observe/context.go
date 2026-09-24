// Package observe provides request IDs, structured JSON logging, panic
// recovery, Prometheus-format metrics and Sentry error reporting without
// third-party dependencies.
package observe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

type ctxKey int

const requestIDKey ctxKey = 1

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// WithRequestID stores a request ID on the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID returns the request ID stored on the context, or "".
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// NewRequestID returns a random 32-character hex identifier.
func NewRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// acceptRequestID keeps an upstream ID (for example Nginx's $request_id) when
// it is well formed, so one ID follows a request through every service.
func acceptRequestID(candidate string) string {
	if requestIDPattern.MatchString(candidate) {
		return candidate
	}
	return NewRequestID()
}

// markReported records that the error on this context already went to the
// reporter, so logging it does not send a duplicate event.
func markReported(ctx context.Context) context.Context {
	return context.WithValue(ctx, reportedKey{}, true)
}

func alreadyReported(ctx context.Context) bool {
	v, _ := ctx.Value(reportedKey{}).(bool)
	return v
}

type reportedKey struct{}
