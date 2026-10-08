package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"

	"github.com/google/uuid"
	grpcmw "github.com/grpc-ecosystem/go-grpc-middleware/v2"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/selector"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/z-alamsyah/codebase-go/internal/platform/logger"
)

const requestIDMetadataKey = "x-request-id"

// SkipBuiltinServices matches every service except the built-in grpc.*
// services (health check, reflection), so auth and request logging skip them.
var SkipBuiltinServices = selector.MatchFunc(func(_ context.Context, c interceptors.CallMeta) bool {
	return !strings.HasPrefix(c.Service, "grpc.")
})

// UnaryRequestID reads x-request-id from incoming metadata (or creates one),
// stores it for logging and returns it in the response header.
func UnaryRequestID(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	return handler(withRequestID(ctx), req)
}

// StreamRequestID is the streaming version of UnaryRequestID.
func StreamRequestID(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	wrapped := grpcmw.WrapServerStream(ss)
	wrapped.WrappedContext = withRequestID(ss.Context())
	return handler(srv, wrapped)
}

func withRequestID(ctx context.Context) context.Context {
	id := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(requestIDMetadataKey); len(v) > 0 {
			id = v[0]
		}
	}
	if id == "" {
		id = uuid.NewString()
	}
	_ = grpc.SetHeader(ctx, metadata.Pairs(requestIDMetadataKey, id))
	return logger.WithRequestID(ctx, id)
}

// GRPCAuth is the authentication placeholder used with the
// go-grpc-middleware auth interceptor.
func GRPCAuth(ctx context.Context) (context.Context, error) {
	// TODO: implement authentication here, for example:
	//   token, err := auth.AuthFromMD(ctx, "bearer")
	//   if err != nil { return nil, err }  // already a codes.Unauthenticated status
	//   validate the token, then return a context that carries the principal.
	// For now every call is allowed.
	return ctx, nil
}

// verboseFields are already part of the one-line message (or rarely useful),
// so they are only logged at debug level.
var verboseFields = map[string]bool{
	logging.SystemTag[0]:       true,
	logging.ComponentFieldKey:  true,
	logging.ServiceFieldKey:    true,
	logging.MethodFieldKey:     true,
	logging.MethodTypeFieldKey: true,
	"peer.address":             true,
	"grpc.start_time":          true,
	"grpc.request.deadline":    true,
	"grpc.code":                true,
	"grpc.time_ms":             true,
	"grpc.error":               true, // grpc.error_cause carries the real error
}

// GRPCLogger adapts slog to the go-grpc-middleware logging interface and
// rewrites "finished call" into a readable one-liner:
// "gRPC /user.v1.UserService/GetUser => OK (1.2ms)". Outside debug level the
// fields repeated in that message are dropped.
func GRPCLogger(l *slog.Logger, debug bool) logging.Logger {
	return logging.LoggerFunc(func(ctx context.Context, lvl logging.Level, msg string, fields ...any) {
		if msg == "finished call" {
			msg = finishedCallMessage(fields)
		}
		if !debug {
			fields = dropFields(fields, verboseFields)
		}
		l.Log(ctx, slog.Level(lvl), msg, fields...)
	})
}

func dropFields(fields []any, drop map[string]bool) []any {
	out := make([]any, 0, len(fields))
	for i := 0; i+1 < len(fields); i += 2 {
		if key, _ := fields[i].(string); drop[key] {
			continue
		}
		out = append(out, fields[i], fields[i+1])
	}
	return out
}

// GRPCCodeToLevel mirrors the REST request log: client errors are warnings,
// server errors are errors.
func GRPCCodeToLevel(code codes.Code) logging.Level {
	switch code {
	case codes.OK:
		return logging.LevelInfo
	case codes.InvalidArgument, codes.NotFound, codes.AlreadyExists, codes.Unauthenticated,
		codes.PermissionDenied, codes.FailedPrecondition, codes.OutOfRange, codes.Canceled, codes.ResourceExhausted:
		return logging.LevelWarn
	default:
		return logging.LevelError
	}
}

func finishedCallMessage(fields []any) string {
	var svc, method, code, dur string
	for i := 0; i+1 < len(fields); i += 2 {
		key, _ := fields[i].(string)
		val := fmt.Sprint(fields[i+1])
		switch key {
		case logging.ServiceFieldKey:
			svc = val
		case logging.MethodFieldKey:
			method = val
		case "grpc.code":
			code = val
		case "grpc.time_ms":
			dur = val + "ms"
		}
	}
	return fmt.Sprintf("gRPC /%s/%s => %s (%s)", svc, method, code, dur)
}

// GRPCLogOptions logs one line per call with the (redacted) request payload.
// Debug level also logs call start and every payload received/sent.
func GRPCLogOptions(debug bool) []logging.Option {
	events := []logging.LoggableEvent{logging.FinishCall}
	if debug {
		events = []logging.LoggableEvent{logging.StartCall, logging.PayloadReceived, logging.PayloadSent, logging.FinishCall}
	}
	return []logging.Option{
		logging.WithLogOnEvents(events...),
		logging.WithLevels(GRPCCodeToLevel),
		logging.WithFieldsFromContextAndCallMeta(func(_ context.Context, c interceptors.CallMeta) logging.Fields {
			if c.ReqOrNil == nil {
				return nil
			}
			return logging.Fields{logger.KeyGRPCRequest, c.ReqOrNil}
		}),
	}
}

// GRPCRecovery converts panics into codes.Internal and logs the stack trace.
func GRPCRecovery(l *slog.Logger) func(ctx context.Context, p any) error {
	return func(ctx context.Context, p any) error {
		l.ErrorContext(ctx, "panic recovered", slog.Any("panic", p), slog.String("stack", string(debug.Stack())))
		return status.Error(codes.Internal, "internal error")
	}
}
