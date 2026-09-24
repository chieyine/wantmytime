package observe

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"
)

type recorder struct {
	http.ResponseWriter
	status  int
	bytes   int
	written bool
}

func (r *recorder) WriteHeader(status int) {
	if !r.written {
		r.status = status
		r.written = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.written {
		r.status = http.StatusOK
		r.written = true
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Middleware assigns a request ID (keeping a well-formed upstream X-Request-ID),
// echoes it in the response, recovers panics as a JSON 500, records metrics,
// reports server errors and writes one structured access-log line per request.
// Route labels use the matched ServeMux pattern, never the raw path.
func Middleware(next http.Handler, logger *slog.Logger, reporter *Reporter, metrics *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := acceptRequestID(r.Header.Get("X-Request-ID"))
		ctx := WithRequestID(r.Context(), id)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", id)
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			panicked := false
			if value := recover(); value != nil {
				panicked = true
				if value == http.ErrAbortHandler {
					panic(value)
				}
				route := routeOf(r)
				reporter.CapturePanic(ctx, value, map[string]string{"route": route, "method": r.Method})
				logger.ErrorContext(markReported(ctx), "panic recovered", "route", route, "panic", fmt.Sprint(value), "stack", string(debug.Stack()))
				if !rec.written {
					rec.Header().Set("Content-Type", "application/json; charset=utf-8")
					rec.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(rec).Encode(map[string]any{"error": map[string]string{"code": "INTERNAL_ERROR", "message": "Something went wrong on our side. Quote reference " + id + " if you contact support."}})
				}
			}
			elapsed := time.Since(start)
			route := routeOf(r)
			metrics.observe(route, r.Method, rec.status, elapsed)
			level := slog.LevelInfo
			if route == "GET /health" || route == "GET /metrics" {
				level = slog.LevelDebug
			}
			if rec.status >= 500 {
				level = slog.LevelWarn
			}
			if rec.status >= 500 && !panicked {
				reporter.CaptureMessage(ctx, "warning", "HTTP "+strconv.Itoa(rec.status)+" on "+route, map[string]string{"route": route, "method": r.Method, "status": strconv.Itoa(rec.status)})
			}
			logger.Log(ctx, level, "request", "method", r.Method, "route", route, "status", rec.status, "duration_ms", elapsed.Milliseconds(), "bytes", rec.bytes)
		}()
		next.ServeHTTP(rec, r)
	})
}

func routeOf(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return "unmatched"
}
