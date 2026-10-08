// Package middleware holds HTTP middlewares and gRPC interceptors that apply
// across endpoints: request id, request logging, auth and tracing helpers.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httplog/v3"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/z-alamsyah/codebase-go/internal/config"
	"github.com/z-alamsyah/codebase-go/internal/platform/logger"
)

const maxRequestIDLen = 128

// RequestID reuses the caller's X-Request-Id header (so ids can be traced
// across services) or generates a UUID. The id is stored where chi
// (middleware.GetReqID) and the logger can read it, and echoed back in the
// response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(chimw.RequestIDHeader)
		if id == "" || len(id) > maxRequestIDLen {
			id = uuid.NewString()
		}
		w.Header().Set(chimw.RequestIDHeader, id)

		ctx := context.WithValue(r.Context(), chimw.RequestIDKey, id)
		ctx = logger.WithRequestID(ctx, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestLogger writes one log line per request using go-chi/httplog.
//
//   - info and above: one line with method, path, status, duration and the
//     request body (sensitive fields redacted by the logger). With
//     LOG_FORMAT=json the same data is also emitted as separate fields
//     (OpenTelemetry names) so log collectors can filter on them.
//   - debug: full detail, including headers, client IP, user agent and the
//     response body.
func RequestLogger(l *slog.Logger, cfg config.Log) func(http.Handler) http.Handler {
	debug := cfg.IsDebug()
	concise := !debug && !strings.EqualFold(cfg.Format, "json")
	opts := &httplog.Options{
		Level:          slog.LevelInfo,
		Schema:         httplog.SchemaOTEL.Concise(concise),
		RecoverPanics:  true,
		LogRequestBody: func(*http.Request) bool { return true },
		// Log the full body; the redactor masks it first and then truncates
		// to LOG_BODY_MAX_BYTES, so truncation never breaks the JSON.
		LogBodyMaxLen: -1,
	}
	if debug {
		opts.Level = slog.LevelDebug
		opts.LogRequestHeaders = []string{"Content-Type", "Origin", "User-Agent", "Authorization", "Cookie", chimw.RequestIDHeader}
		opts.LogResponseHeaders = []string{"Content-Type", "Location"}
		opts.LogResponseBody = func(*http.Request) bool { return true }
	}
	return httplog.RequestLogger(l, opts)
}

// Auth is the authentication middleware placeholder for REST endpoints.
func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO: implement authentication here, for example:
		//   1. read the token from the Authorization header ("Bearer <token>")
		//   2. validate it (JWT signature/expiry, or call an auth service)
		//   3. on failure: respond 401 and return
		//   4. on success: store the principal in the context and continue
		// For now every request is allowed.
		next.ServeHTTP(w, r)
	})
}

// RouteTag runs right after otelhttp. chi sets r.Pattern while routing, but
// with nested routers (r.Route) the request otelhttp sees only gets the outer
// pattern ("/api/v1/*"). Once routing is done, RouteTag writes the full
// pattern ("/api/v1/users/{id}") back, so otelhttp uses it for the span name
// and the http.route metric label (low-cardinality: no raw ids).
func RouteTag(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		pattern := RoutePattern(r)
		if pattern == "" {
			return
		}
		r.Pattern = pattern
		span := trace.SpanFromContext(r.Context())
		span.SetName(r.Method + " " + pattern)
		span.SetAttributes(attribute.String("http.route", pattern))
	})
}

// RoutePattern returns the matched chi route pattern, e.g. /api/v1/users/{id}.
func RoutePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		return rctx.RoutePattern()
	}
	return ""
}
