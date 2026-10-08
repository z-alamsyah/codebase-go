// Command app runs the service. Which components start (REST, gRPC,
// consumer, OpenTelemetry) is controlled by environment variables; see
// .env.example.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/z-alamsyah/codebase-go/internal/app"
	"github.com/z-alamsyah/codebase-go/internal/config"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	a, err := app.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("startup: %w", err)
	}
	return a.Run(ctx)
}
