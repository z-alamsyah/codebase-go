// Package app wires every layer together (dependency injection) and runs the
// enabled servers until shutdown.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"gorm.io/gorm"

	"github.com/z-alamsyah/codebase-go/internal/config"
	"github.com/z-alamsyah/codebase-go/internal/controller/consumer"
	"github.com/z-alamsyah/codebase-go/internal/controller/rest"
	"github.com/z-alamsyah/codebase-go/internal/controller/rpc"
	"github.com/z-alamsyah/codebase-go/internal/platform/cache"
	"github.com/z-alamsyah/codebase-go/internal/platform/database"
	"github.com/z-alamsyah/codebase-go/internal/platform/logger"
	"github.com/z-alamsyah/codebase-go/internal/platform/messaging"
	"github.com/z-alamsyah/codebase-go/internal/platform/messaging/rabbitmq"
	"github.com/z-alamsyah/codebase-go/internal/platform/password"
	"github.com/z-alamsyah/codebase-go/internal/platform/telemetry"
	"github.com/z-alamsyah/codebase-go/internal/repository/mailer"
	"github.com/z-alamsyah/codebase-go/internal/repository/postgres"
	redisrepo "github.com/z-alamsyah/codebase-go/internal/repository/redis"
	"github.com/z-alamsyah/codebase-go/internal/router"
	"github.com/z-alamsyah/codebase-go/internal/service/user"
)

type App struct {
	cfg config.Config
	log *slog.Logger
	tel *telemetry.Telemetry

	db  *gorm.DB
	rdb *redis.Client
	mq  *rabbitmq.Client // nil when MQ_ENABLED=false

	httpServer *http.Server
	grpcServer *grpc.Server // nil when GRPC_ENABLED=false
	grpcHealth *health.Server
	consumer   *rabbitmq.Consumer // nil when CONSUMER_ENABLED=false
}

// New initializes infrastructure and wires repository -> service ->
// controller -> router. Components that are disabled are not created and
// open no connections.
func New(ctx context.Context, cfg config.Config) (a *App, err error) {
	tel, err := telemetry.Setup(ctx, cfg.App, cfg.Otel)
	if err != nil {
		return nil, err
	}
	log, _ := logger.New(cfg.Log, cfg.App.Name, tel.LogHandler)
	slog.SetDefault(log)

	a = &App{cfg: cfg, log: log, tel: tel}
	defer func() {
		if err != nil {
			a.close()
		}
	}()

	// Infrastructure.
	if a.db, err = database.NewPostgres(ctx, cfg.Postgres, cfg.Log, log, cfg.Otel.Enabled); err != nil {
		return a, err
	}
	if a.rdb, err = cache.NewRedis(ctx, cfg.Redis, log, cfg.Otel.Enabled); err != nil {
		return a, err
	}
	checks := []rest.Check{
		{Name: "postgres", Fn: database.Ping(a.db)},
		{Name: "redis", Fn: cache.Ping(a.rdb)},
	}

	var publisher messaging.Publisher = messaging.NoopPublisher{Logger: log}
	if cfg.MQ.Enabled {
		a.mq = rabbitmq.NewClient(cfg.MQ.URL, cfg.App.Name, log)
		if err = a.mq.Connect(); err != nil {
			return a, err
		}
		if publisher, err = rabbitmq.NewPublisher(a.mq, cfg.MQ.Exchange, log); err != nil {
			return a, err
		}
		checks = append(checks, rest.Check{Name: "rabbitmq", Fn: a.mq.Ping})
	}

	// Data layer.
	userRepo := postgres.NewUserRepository(a.db)
	userCache := redisrepo.NewUserCache(a.rdb, cfg.App.Name, cfg.Redis.CacheTTL)
	idempotency := redisrepo.NewIdempotencyStore(a.rdb, cfg.App.Name)

	// Business logic layer.
	userSvc := user.NewService(user.Deps{
		Repo:        userRepo,
		Cache:       userCache,
		Publisher:   publisher,
		Idempotency: idempotency,
		Hasher:      password.NewBcrypt(),
		Mailer:      mailer.NewLogMailer(log),
		Logger:      log,
	})

	// Controller + router layers.
	a.httpServer = &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.HTTP.Port),
		Handler: router.NewHTTP(router.HTTPDeps{
			Config: cfg,
			Logger: log,
			Health: rest.NewHealthHandler(checks...),
			User:   rest.NewUserHandler(userSvc),
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	if cfg.GRPC.Enabled {
		a.grpcServer, a.grpcHealth = router.NewGRPC(router.GRPCDeps{
			Config: cfg,
			Logger: log,
			User:   rpc.NewUserServer(userSvc),
		})
	}

	if cfg.Consumer.Enabled {
		a.consumer = rabbitmq.NewConsumer(a.mq, rabbitmq.ConsumerConfig{
			Exchange:   cfg.MQ.Exchange,
			Prefetch:   cfg.Consumer.Prefetch,
			MaxRetries: cfg.Consumer.MaxRetries,
			RetryDelay: cfg.Consumer.RetryDelay,
		}, router.ConsumerRoutes(router.ConsumerDeps{
			App:  cfg.App,
			User: consumer.NewUserHandler(userSvc),
		}), log)
	}

	return a, nil
}

// Run starts every enabled component and blocks until ctx is canceled (e.g.
// SIGTERM) or a component fails, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	defer a.close()

	var grpcListener net.Listener
	if a.grpcServer != nil {
		var err error
		var lc net.ListenConfig
		if grpcListener, err = lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", a.cfg.GRPC.Port)); err != nil {
			return fmt.Errorf("grpc listen: %w", err)
		}
	}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		if err := a.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})
	if a.grpcServer != nil {
		g.Go(func() error {
			if err := a.grpcServer.Serve(grpcListener); err != nil {
				return fmt.Errorf("grpc server: %w", err)
			}
			return nil
		})
	}
	if a.consumer != nil {
		g.Go(func() error { return a.consumer.Run(gctx) })
	}
	g.Go(func() error {
		<-gctx.Done()
		return a.shutdownServers()
	})

	a.log.Info("service started",
		slog.String("env", a.cfg.App.Env),
		slog.Int("http_port", a.cfg.HTTP.Port),
		slog.Bool("rest", a.cfg.HTTP.RESTEnabled),
		slog.Bool("grpc", a.cfg.GRPC.Enabled),
		slog.Bool("mq", a.cfg.MQ.Enabled),
		slog.Bool("consumer", a.cfg.Consumer.Enabled),
		slog.Bool("otel", a.cfg.Otel.Enabled),
		slog.String("log_level", a.cfg.Log.Level),
	)

	done := make(chan error, 1)
	go func() { done <- g.Wait() }()

	select {
	case err := <-done:
		return err
	case <-gctx.Done():
		// Shutdown started: wait for in-flight work, but not forever.
		select {
		case err := <-done:
			return err
		case <-time.After(a.cfg.App.ShutdownTimeout):
			return fmt.Errorf("graceful shutdown exceeded %s", a.cfg.App.ShutdownTimeout)
		}
	}
}

// shutdownServers stops accepting new work and waits for in-flight requests.
// The consumer stops on its own because it watches the same context.
func (a *App) shutdownServers() error {
	a.log.Info("shutting down: draining in-flight requests and messages")
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.App.ShutdownTimeout)
	defer cancel()

	var errs []error
	if a.grpcHealth != nil {
		a.grpcHealth.Shutdown() // report NOT_SERVING to probes first
	}
	if err := a.httpServer.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("http shutdown: %w", err))
	}
	if a.grpcServer != nil {
		stopped := make(chan struct{})
		go func() {
			a.grpcServer.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-ctx.Done():
			a.grpcServer.Stop()
			errs = append(errs, errors.New("grpc graceful stop timed out"))
		}
	}
	return errors.Join(errs...)
}

// close releases connections, then flushes telemetry last so the shutdown
// logs are exported too.
func (a *App) close() {
	if a.mq != nil {
		if err := a.mq.Close(); err != nil {
			a.log.Warn("close rabbitmq", slog.Any("error", err))
		}
	}
	if a.rdb != nil {
		if err := a.rdb.Close(); err != nil {
			a.log.Warn("close redis", slog.Any("error", err))
		}
	}
	if a.db != nil {
		if err := database.Close(a.db); err != nil {
			a.log.Warn("close postgres", slog.Any("error", err))
		}
	}
	a.log.Info("service stopped")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.tel.Shutdown(ctx); err != nil {
		a.log.Warn("flush telemetry", slog.Any("error", err))
	}
}
