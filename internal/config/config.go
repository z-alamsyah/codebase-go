// Package config loads application configuration from environment variables
// (and an optional .env file for local development) into a typed struct.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config is the root configuration. Every field maps to one environment variable.
type Config struct {
	App      App
	HTTP     HTTP
	GRPC     GRPC
	Log      Log
	Postgres Postgres
	Redis    Redis
	MQ       MQ
	Consumer Consumer
	Otel     Otel
}

type App struct {
	Name            string        `env:"APP_NAME" envDefault:"codebase-go"`
	Env             string        `env:"APP_ENV" envDefault:"local"`
	Version         string        `env:"APP_VERSION" envDefault:"dev"`
	ShutdownTimeout time.Duration `env:"APP_SHUTDOWN_TIMEOUT" envDefault:"15s"`
}

// HTTP configures the HTTP server. The server always runs to serve health
// checks; RESTEnabled only toggles the business routes under /api.
type HTTP struct {
	RESTEnabled    bool          `env:"REST_ENABLED" envDefault:"true"`
	Port           int           `env:"HTTP_PORT" envDefault:"8080"`
	ReadTimeout    time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"10s"`
	WriteTimeout   time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"15s"`
	RequestTimeout time.Duration `env:"HTTP_REQUEST_TIMEOUT" envDefault:"10s"`
	// TrustedProxies is how many reverse proxies (load balancer, ingress) sit
	// in front of the app. 0 = use the TCP peer address and ignore
	// X-Forwarded-For, which clients can forge.
	TrustedProxies int `env:"HTTP_TRUSTED_PROXIES" envDefault:"0"`
}

type GRPC struct {
	Enabled           bool `env:"GRPC_ENABLED" envDefault:"false"`
	Port              int  `env:"GRPC_PORT" envDefault:"9090"`
	ReflectionEnabled bool `env:"GRPC_REFLECTION_ENABLED" envDefault:"true"`
}

type Log struct {
	Level        string   `env:"LOG_LEVEL" envDefault:"info"`
	Format       string   `env:"LOG_FORMAT" envDefault:"pretty"`
	RedactKeys   []string `env:"LOG_REDACT_KEYS" envSeparator:"," envDefault:"password,token,access_token,refresh_token,secret,authorization,cookie,pin,otp,cvv,card_number"`
	BodyMaxBytes int      `env:"LOG_BODY_MAX_BYTES" envDefault:"4096"`
}

// IsDebug reports whether detailed (debug) logging is enabled.
func (l Log) IsDebug() bool { return strings.EqualFold(l.Level, "debug") }

type Postgres struct {
	Host               string        `env:"DB_HOST" envDefault:"localhost"`
	Port               int           `env:"DB_PORT" envDefault:"5432"`
	User               string        `env:"DB_USER" envDefault:"postgres"`
	Password           string        `env:"DB_PASSWORD" envDefault:"postgres"`
	Name               string        `env:"DB_NAME" envDefault:"codebase_go"`
	SSLMode            string        `env:"DB_SSLMODE" envDefault:"disable"`
	MaxOpenConns       int           `env:"DB_MAX_OPEN_CONNS" envDefault:"25"`
	MaxIdleConns       int           `env:"DB_MAX_IDLE_CONNS" envDefault:"10"`
	ConnMaxLifetime    time.Duration `env:"DB_CONN_MAX_LIFETIME" envDefault:"30m"`
	ConnMaxIdleTime    time.Duration `env:"DB_CONN_MAX_IDLE_TIME" envDefault:"5m"`
	SlowQueryThreshold time.Duration `env:"DB_SLOW_QUERY_THRESHOLD" envDefault:"200ms"`
}

// DSN returns a postgres:// URL usable by GORM and golang-migrate.
func (p Postgres) DSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(p.User, p.Password),
		Host:     fmt.Sprintf("%s:%d", p.Host, p.Port),
		Path:     p.Name,
		RawQuery: url.Values{"sslmode": []string{p.SSLMode}}.Encode(),
	}
	return u.String()
}

type Redis struct {
	Addr     string        `env:"REDIS_ADDR" envDefault:"localhost:6379"`
	Password string        `env:"REDIS_PASSWORD"`
	DB       int           `env:"REDIS_DB" envDefault:"0"`
	CacheTTL time.Duration `env:"CACHE_TTL" envDefault:"5m"`
}

// MQ configures the broker connection. When enabled, the publisher is active.
type MQ struct {
	Enabled  bool   `env:"MQ_ENABLED" envDefault:"false"`
	URL      string `env:"RABBITMQ_URL" envDefault:"amqp://guest:guest@localhost:5672/"`
	Exchange string `env:"RABBITMQ_EXCHANGE" envDefault:"codebase-go.events"`
}

// Consumer configures the message consumer workers. Requires MQ.Enabled.
type Consumer struct {
	Enabled    bool          `env:"CONSUMER_ENABLED" envDefault:"false"`
	Prefetch   int           `env:"CONSUMER_PREFETCH" envDefault:"10"`
	MaxRetries int           `env:"CONSUMER_MAX_RETRIES" envDefault:"3"`
	RetryDelay time.Duration `env:"CONSUMER_RETRY_DELAY" envDefault:"5s"`
}

// Otel configures OpenTelemetry. Exporter details (endpoint, protocol,
// sampler, resource attributes) are read by the OTel SDK itself from the
// standard OTEL_* variables; they are listed here only for validation.
type Otel struct {
	Enabled  bool   `env:"OTEL_ENABLED" envDefault:"false"`
	Endpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
}

// Load reads .env (if present) and the process environment, then validates.
func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}

	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse env: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

// Validate checks values and flag combinations so the app fails fast at startup.
func (c Config) Validate() error {
	var errs []error

	if !slices.Contains([]string{"debug", "info", "warn", "error"}, strings.ToLower(c.Log.Level)) {
		errs = append(errs, fmt.Errorf("LOG_LEVEL must be one of debug|info|warn|error, got %q", c.Log.Level))
	}
	if !slices.Contains([]string{"pretty", "json"}, strings.ToLower(c.Log.Format)) {
		errs = append(errs, fmt.Errorf("LOG_FORMAT must be pretty|json, got %q", c.Log.Format))
	}
	if c.Consumer.Enabled && !c.MQ.Enabled {
		errs = append(errs, errors.New("CONSUMER_ENABLED=true requires MQ_ENABLED=true"))
	}
	if c.Consumer.Enabled && c.Consumer.Prefetch < 1 {
		errs = append(errs, errors.New("CONSUMER_PREFETCH must be >= 1"))
	}
	if c.Otel.Enabled && c.Otel.Endpoint == "" {
		errs = append(errs, errors.New("OTEL_ENABLED=true requires OTEL_EXPORTER_OTLP_ENDPOINT"))
	}
	if c.GRPC.Enabled && c.GRPC.Port == c.HTTP.Port {
		errs = append(errs, errors.New("GRPC_PORT and HTTP_PORT must differ"))
	}

	return errors.Join(errs...)
}
