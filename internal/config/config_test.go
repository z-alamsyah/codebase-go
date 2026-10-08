package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func validConfig() Config {
	return Config{
		HTTP: HTTP{Port: 8080},
		GRPC: GRPC{Port: 9090},
		Log:  Log{Level: "info", Format: "pretty"},
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr string
	}{
		{name: "defaults are valid", mutate: func(*Config) {}},
		{name: "unknown log level", mutate: func(c *Config) { c.Log.Level = "verbose" }, wantErr: "LOG_LEVEL"},
		{name: "unknown log format", mutate: func(c *Config) { c.Log.Format = "xml" }, wantErr: "LOG_FORMAT"},
		{
			name:    "consumer requires MQ",
			mutate:  func(c *Config) { c.Consumer = Consumer{Enabled: true, Prefetch: 1} },
			wantErr: "CONSUMER_ENABLED=true requires MQ_ENABLED=true",
		},
		{
			name:    "otel requires endpoint",
			mutate:  func(c *Config) { c.Otel.Enabled = true },
			wantErr: "OTEL_EXPORTER_OTLP_ENDPOINT",
		},
		{
			name:    "grpc and http ports must differ",
			mutate:  func(c *Config) { c.GRPC = GRPC{Enabled: true, Port: 8080} },
			wantErr: "GRPC_PORT and HTTP_PORT must differ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(&c)
			err := c.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestPostgres_DSN_EscapesPassword(t *testing.T) {
	p := Postgres{Host: "db", Port: 5432, User: "app", Password: "p@ss:word/1", Name: "app_db", SSLMode: "disable"}
	assert.Equal(t, "postgres://app:p%40ss%3Aword%2F1@db:5432/app_db?sslmode=disable", p.DSN())
}
