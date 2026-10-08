package router

import (
	"log/slog"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/selector"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	userv1 "github.com/z-alamsyah/codebase-go/gen/proto/user/v1"
	"github.com/z-alamsyah/codebase-go/internal/config"
	"github.com/z-alamsyah/codebase-go/internal/controller/rpc"
	"github.com/z-alamsyah/codebase-go/internal/middleware"
)

type GRPCDeps struct {
	Config config.Config
	Logger *slog.Logger
	User   *rpc.UserServer
}

// NewGRPC builds the gRPC server with the standard health service
// (grpc.health.v1) and, optionally, server reflection.
//
// Interceptor order: request id -> request log -> auth -> recovery.
// Logging and auth skip the built-in grpc.* services (health, reflection).
func NewGRPC(d GRPCDeps) (*grpc.Server, *health.Server) {
	logger := middleware.GRPCLogger(d.Logger, d.Config.Log.IsDebug())
	logOpts := middleware.GRPCLogOptions(d.Config.Log.IsDebug())
	recoveryOpt := recovery.WithRecoveryHandlerContext(middleware.GRPCRecovery(d.Logger))

	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			middleware.UnaryRequestID,
			selector.UnaryServerInterceptor(logging.UnaryServerInterceptor(logger, logOpts...), middleware.SkipBuiltinServices),
			selector.UnaryServerInterceptor(auth.UnaryServerInterceptor(middleware.GRPCAuth), middleware.SkipBuiltinServices),
			recovery.UnaryServerInterceptor(recoveryOpt),
		),
		grpc.ChainStreamInterceptor(
			middleware.StreamRequestID,
			selector.StreamServerInterceptor(logging.StreamServerInterceptor(logger, logOpts...), middleware.SkipBuiltinServices),
			selector.StreamServerInterceptor(auth.StreamServerInterceptor(middleware.GRPCAuth), middleware.SkipBuiltinServices),
			recovery.StreamServerInterceptor(recoveryOpt),
		),
	}
	if d.Config.Otel.Enabled {
		opts = append(opts, grpc.StatsHandler(otelgrpc.NewServerHandler()))
	}

	srv := grpc.NewServer(opts...)

	// Register services here.
	userv1.RegisterUserServiceServer(srv, d.User)

	hs := health.NewServer()
	healthpb.RegisterHealthServer(srv, hs)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus(userv1.UserService_ServiceDesc.ServiceName, healthpb.HealthCheckResponse_SERVING)

	if d.Config.GRPC.ReflectionEnabled {
		reflection.Register(srv)
	}
	return srv, hs
}
