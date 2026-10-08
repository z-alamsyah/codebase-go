// Package database opens the PostgreSQL connection through GORM.
package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/z-alamsyah/codebase-go/internal/config"
)

// NewPostgres opens a pooled GORM connection and verifies it with a ping.
// SQL statements are logged only when LOG_LEVEL=debug; slow queries and
// errors are always logged. Queries are logged with placeholders, never with
// bound values, so user data does not leak into logs.
//
// GORM runs on top of the pgx driver. When OTel is enabled, otelpgx traces
// every query at the driver level (lighter than the GORM otel plugin, which
// also links the MySQL and ClickHouse drivers into the binary).
func NewPostgres(ctx context.Context, cfg config.Postgres, logCfg config.Log, log *slog.Logger, otelEnabled bool) (*gorm.DB, error) {
	level := gormlogger.Warn
	if logCfg.IsDebug() {
		level = gormlogger.Info
	}

	connCfg, err := pgx.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	if otelEnabled {
		connCfg.Tracer = otelpgx.NewTracer()
	}

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: stdlib.OpenDB(*connCfg)}), &gorm.Config{
		Logger: gormlogger.NewSlogLogger(log, gormlogger.Config{
			LogLevel:                  level,
			SlowThreshold:             cfg.SlowQueryThreshold,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
		}),
		TranslateError: true, // maps driver errors to gorm.ErrDuplicatedKey, etc.
		NowFunc:        func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return db, nil
}

// Close closes the underlying connection pool.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Ping is used by the readiness check.
func Ping(db *gorm.DB) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.PingContext(ctx)
	}
}
