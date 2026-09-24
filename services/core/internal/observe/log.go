package observe

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// NewLogger returns a JSON logger. Records at Error level and above are also
// sent to the reporter (which may be nil). The request ID on the context, if
// any, is added to every record.
func NewLogger(w io.Writer, reporter *Reporter) *slog.Logger {
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("LOG_LEVEL"), "debug") {
		level = slog.LevelDebug
	}
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(&contextHandler{Handler: base, reporter: reporter})
}

type contextHandler struct {
	slog.Handler
	reporter *Reporter
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	if r.Level >= slog.LevelError && h.reporter != nil && !alreadyReported(ctx) {
		tags := map[string]string{}
		r.Attrs(func(a slog.Attr) bool {
			if len(tags) < 20 && a.Key != "stack" && a.Key != "request_id" {
				v := a.Value.String()
				if len(v) > 200 { // Sentry's tag value limit
					v = v[:200]
				}
				tags[a.Key] = v
			}
			return true
		})
		h.reporter.CaptureMessage(ctx, "error", r.Message, tags)
	}
	return h.Handler.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs), reporter: h.reporter}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithGroup(name), reporter: h.reporter}
}
