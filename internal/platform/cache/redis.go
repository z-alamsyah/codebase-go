// Package cache creates the Redis client.
package cache

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"

	"github.com/z-alamsyah/codebase-go/internal/config"
)

// NewRedis creates the client. An unreachable Redis at startup is logged as a
// warning instead of stopping the app: the cache is optional for correctness
// and go-redis reconnects automatically.
func NewRedis(ctx context.Context, cfg config.Redis, log *slog.Logger, otelEnabled bool) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	if otelEnabled {
		if err := redisotel.InstrumentTracing(rdb); err != nil {
			return nil, fmt.Errorf("redis otel tracing: %w", err)
		}
		if err := redisotel.InstrumentMetrics(rdb); err != nil {
			return nil, fmt.Errorf("redis otel metrics: %w", err)
		}
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		log.WarnContext(ctx, "redis not reachable at startup, continuing without cache", slog.String("addr", cfg.Addr), slog.Any("error", err))
	}
	return rdb, nil
}

// Ping is used by the readiness check.
func Ping(rdb *redis.Client) func(ctx context.Context) error {
	return func(ctx context.Context) error { return rdb.Ping(ctx).Err() }
}
