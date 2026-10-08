// Package messaging defines broker-agnostic types for publishing and
// consuming events. Broker implementations (e.g. rabbitmq) live in
// sub-packages, so a broker can be swapped without touching services.
package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

// Envelope is the standard wire format for every message.
type Envelope struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Version    int             `json:"version"`
	Payload    json.RawMessage `json:"payload"`
}

// Message is what a consumer handler receives.
type Message struct {
	Envelope
	Queue      string
	RoutingKey string
	// Attempt starts at 0 and increases on every retry.
	Attempt int
}

// HandlerFunc processes one message. Returning an error triggers a retry;
// wrap it with Permanent to send the message straight to the dead letter queue.
type HandlerFunc func(ctx context.Context, msg Message) error

// Route binds a queue (and the routing key it listens to) to a handler.
type Route struct {
	Queue      string
	RoutingKey string
	Handler    HandlerFunc
}

// Publisher publishes payloads wrapped in an Envelope.
type Publisher interface {
	Publish(ctx context.Context, routingKey string, payload any) error
}

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent marks an error as not worth retrying (e.g. a malformed payload).
func Permanent(err error) error { return permanentError{err: err} }

// IsPermanent reports whether err was wrapped with Permanent.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

// NoopPublisher is used when MQ_ENABLED=false. Events are dropped with a debug log.
type NoopPublisher struct {
	Logger *slog.Logger
}

func (p NoopPublisher) Publish(ctx context.Context, routingKey string, _ any) error {
	p.Logger.DebugContext(ctx, "event not published: MQ disabled", slog.String("routing_key", routingKey))
	return nil
}
