// Package logger builds the application slog.Logger.
//
// Handler chain: redact -> context (request_id, trace_id) -> fan-out to
// stdout (pretty or JSON) and, when enabled, the OpenTelemetry log bridge.
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
	"go.opentelemetry.io/otel/trace"

	"github.com/z-alamsyah/codebase-go/internal/config"
)

// New creates the root logger. extra is an optional additional sink (e.g. the
// OTel bridge); pass nil when telemetry is disabled.
func New(cfg config.Log, service string, extra slog.Handler) (*slog.Logger, *Redactor) {
	level := ParseLevel(cfg.Level)
	debug := level <= slog.LevelDebug

	var stdout slog.Handler
	if strings.EqualFold(cfg.Format, "json") {
		stdout = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level, AddSource: debug})
	} else {
		stdout = tint.NewTextHandler(os.Stdout, &tint.Options{Level: level, AddSource: debug, TimeFormat: time.TimeOnly, ReplaceAttr: shortKeys})
	}

	sink := stdout
	if extra != nil {
		sink = slog.NewMultiHandler(stdout, &leveledHandler{Handler: extra, level: level})
	}

	redactor := NewRedactor(cfg.RedactKeys, cfg.BodyMaxBytes)
	l := slog.New(&redactHandler{next: &contextHandler{next: sink}, r: redactor})
	if strings.EqualFold(cfg.Format, "json") {
		l = l.With(slog.String("service", service))
	}
	return l, redactor
}

// prettyKeys shortens long semantic-convention keys in pretty (local) output.
// JSON output keeps the full OpenTelemetry names for log collectors.
var prettyKeys = map[string]string{
	KeyHTTPRequestBody:             "body",
	KeyHTTPResponseBody:            "response",
	KeyGRPCRequest:                 "body",
	KeyGRPCResponse:                "response",
	"http.request.header":          "req_headers",
	"http.response.header":         "resp_headers",
	"client.address":               "client_ip",
	"user_agent.original":          "user_agent",
	"http.request.body.size":       "req_bytes",
	"http.response.body.size":      "resp_bytes",
	"http.response.status_code":    "status",
	"http.server.request.duration": "duration_s",
}

func shortKeys(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 {
		if short, ok := prettyKeys[a.Key]; ok {
			a.Key = short
		}
	}
	return a
}

// ParseLevel converts debug|info|warn|error to slog.Level (default info).
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type requestIDKey struct{}

// WithRequestID stores the request id so every log written with this context includes it.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request id stored in ctx, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// contextHandler enriches records with request_id and trace/span ids from ctx.
type contextHandler struct{ next slog.Handler }

func (h *contextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *contextHandler) Handle(ctx context.Context, rec slog.Record) error {
	if id := RequestID(ctx); id != "" {
		rec.AddAttrs(slog.String("request_id", id))
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		rec.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return h.next.Handle(ctx, rec)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{next: h.next.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{next: h.next.WithGroup(name)}
}

// leveledHandler applies the configured minimum level to a sink that does not
// filter on its own (the OTel bridge accepts every level by default).
type leveledHandler struct {
	slog.Handler
	level slog.Level
}

func (h *leveledHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.level && h.Handler.Enabled(ctx, l)
}

func (h *leveledHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &leveledHandler{Handler: h.Handler.WithAttrs(attrs), level: h.level}
}

func (h *leveledHandler) WithGroup(name string) slog.Handler {
	return &leveledHandler{Handler: h.Handler.WithGroup(name), level: h.level}
}
