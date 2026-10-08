// Command migrate applies schema migrations and local seed data.
//
//	go run ./cmd/migrate up          apply all schema migrations
//	go run ./cmd/migrate down [N]    roll back N schema migrations (default 1)
//	go run ./cmd/migrate version     print the current schema version
//	go run ./cmd/migrate force V     mark version V as applied (fix a dirty state)
//	go run ./cmd/migrate seed        apply seed data (local/dev only)
//	go run ./cmd/migrate seed-down   remove seed data
//
// Schema and seeds are tracked in separate tables (schema_migrations and
// seed_migrations), so seeding never changes the schema version.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/z-alamsyah/codebase-go/internal/config"
	"github.com/z-alamsyah/codebase-go/migrations"
	"github.com/z-alamsyah/codebase-go/seeds"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: migrate up|down [N]|version|force V|seed|seed-down")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch args[0] {
	case "up":
		return withMigrate(cfg, migrations.FS, "schema_migrations", func(m *migrate.Migrate) error { return m.Up() })
	case "down":
		steps := 1
		if len(args) > 1 {
			if steps, err = strconv.Atoi(args[1]); err != nil || steps < 1 {
				return fmt.Errorf("invalid step count %q", args[1])
			}
		}
		return withMigrate(cfg, migrations.FS, "schema_migrations", func(m *migrate.Migrate) error { return m.Steps(-steps) })
	case "version":
		return withMigrate(cfg, migrations.FS, "schema_migrations", func(m *migrate.Migrate) error {
			v, dirty, err := m.Version()
			if err != nil {
				return err
			}
			fmt.Printf("version=%d dirty=%t\n", v, dirty)
			return nil
		})
	case "force":
		if len(args) < 2 {
			return errors.New("usage: migrate force V")
		}
		v, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid version %q", args[1])
		}
		return withMigrate(cfg, migrations.FS, "schema_migrations", func(m *migrate.Migrate) error { return m.Force(v) })
	case "seed":
		if strings.EqualFold(cfg.App.Env, "production") {
			return errors.New("refusing to seed dummy data when APP_ENV=production")
		}
		return withMigrate(cfg, seeds.FS, "seed_migrations", func(m *migrate.Migrate) error { return m.Up() })
	case "seed-down":
		return withMigrate(cfg, seeds.FS, "seed_migrations", func(m *migrate.Migrate) error { return m.Down() })
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func withMigrate(cfg config.Config, files fs.FS, table string, fn func(*migrate.Migrate) error) error {
	src, err := iofs.New(files, ".")
	if err != nil {
		return fmt.Errorf("open migration files: %w", err)
	}
	dbURL := strings.Replace(cfg.Postgres.DSN(), "postgres://", "pgx5://", 1) + "&x-migrations-table=" + table

	m, err := migrate.NewWithSourceInstance("iofs", src, dbURL)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := fn(m); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	fmt.Println("ok")
	return nil
}
