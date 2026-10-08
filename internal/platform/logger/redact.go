package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Redacted replaces the value of every sensitive field.
const Redacted = "[REDACTED]"

// Attribute keys whose string value is a JSON payload. Their content is parsed
// and redacted field by field instead of being dropped as a whole.
const (
	KeyHTTPRequestBody  = "http.request.body.content" // httplog SchemaOTEL
	KeyHTTPResponseBody = "http.response.body.content"
	KeyGRPCRequest      = "grpc.request.content" // go-grpc-middleware logging
	KeyGRPCResponse     = "grpc.response.content"
	KeyPayload          = "payload" // consumer messages
)

var payloadKeys = map[string]struct{}{
	KeyHTTPRequestBody:  {},
	KeyHTTPResponseBody: {},
	KeyGRPCRequest:      {},
	KeyGRPCResponse:     {},
	KeyPayload:          {},
}

// Redactor masks sensitive values in log attributes and JSON payloads.
// Key matching is case-insensitive and applies at any nesting depth.
type Redactor struct {
	keys     map[string]struct{}
	maxBytes int
}

// NewRedactor builds a Redactor. maxBytes <= 0 disables payload truncation.
func NewRedactor(keys []string, maxBytes int) *Redactor {
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			set[k] = struct{}{}
		}
	}
	return &Redactor{keys: set, maxBytes: maxBytes}
}

// IsSensitive reports whether a field or header name must be masked.
func (r *Redactor) IsSensitive(key string) bool {
	_, ok := r.keys[strings.ToLower(key)]
	return ok
}

// JSON redacts a raw JSON payload and returns it as a compact string.
// Non-JSON payloads are never logged verbatim because they cannot be
// redacted reliably; only their size is reported.
func (r *Redactor) JSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Sprintf("[non-JSON payload omitted, %d bytes]", len(raw))
	}
	out, err := json.Marshal(r.Value(v))
	if err != nil {
		return fmt.Sprintf("[payload omitted: %v]", err)
	}
	return r.truncate(string(out))
}

// Proto redacts a protobuf message using its JSON representation.
func (r *Redactor) Proto(m proto.Message) string {
	raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(m)
	if err != nil {
		return fmt.Sprintf("[payload omitted: %v]", err)
	}
	return r.JSON(raw)
}

// Value walks decoded JSON (maps and slices) and masks sensitive keys.
func (r *Redactor) Value(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if r.IsSensitive(k) {
				out[k] = Redacted
				continue
			}
			out[k] = r.Value(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = r.Value(val)
		}
		return out
	default:
		return v
	}
}

func (r *Redactor) truncate(s string) string {
	if r.maxBytes <= 0 || len(s) <= r.maxBytes {
		return s
	}
	return s[:r.maxBytes] + "...[truncated]"
}

// Attr returns a copy of a with sensitive data masked.
func (r *Redactor) Attr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if r.IsSensitive(a.Key) {
		return slog.String(a.Key, Redacted)
	}

	switch a.Value.Kind() {
	case slog.KindGroup:
		group := a.Value.Group()
		out := make([]slog.Attr, len(group))
		for i, ga := range group {
			out[i] = r.Attr(ga)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	case slog.KindString:
		if _, ok := payloadKeys[a.Key]; ok {
			if a.Value.String() == "" {
				return slog.Attr{} // empty payload (e.g. GET): handlers skip empty attrs
			}
			return slog.String(a.Key, r.JSON([]byte(a.Value.String())))
		}
	case slog.KindAny:
		switch v := a.Value.Any().(type) {
		case proto.Message:
			return slog.String(a.Key, r.Proto(v))
		case []byte:
			if _, ok := payloadKeys[a.Key]; ok {
				return slog.String(a.Key, r.JSON(v))
			}
		}
	}
	return a
}

// redactHandler applies the Redactor to every record before passing it on,
// so all sinks (stdout and OTel) receive already-masked data.
type redactHandler struct {
	next slog.Handler
	r    *Redactor
}

func (h *redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *redactHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.r.Attr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = h.r.Attr(a)
	}
	return &redactHandler{next: h.next.WithAttrs(out), r: h.r}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{next: h.next.WithGroup(name), r: h.r}
}
