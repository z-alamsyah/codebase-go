// Package rabbitmq implements messaging.Publisher and a consumer runner on
// top of github.com/rabbitmq/amqp091-go, adding what the client library does
// not provide: reconnects, retry with delay, dead letter queue, trace
// propagation and graceful drain.
package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Client owns one AMQP connection and re-dials it on demand after it drops.
type Client struct {
	url  string
	name string
	log  *slog.Logger

	mu   sync.Mutex
	conn *amqp.Connection
}

// NewClient creates a client; name is shown in the RabbitMQ management UI.
func NewClient(url, name string, log *slog.Logger) *Client {
	return &Client{url: url, name: name, log: log}
}

// Connect dials the broker once. Call it at startup to fail fast.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dialLocked()
}

func (c *Client) dialLocked() error {
	props := amqp.NewConnectionProperties()
	props.SetClientConnectionName(c.name)
	conn, err := amqp.DialConfig(c.url, amqp.Config{
		Heartbeat:  10 * time.Second,
		Locale:     "en_US",
		Properties: props,
	})
	if err != nil {
		return fmt.Errorf("dial rabbitmq: %w", err)
	}
	c.conn = conn
	return nil
}

// Channel opens a new channel, re-dialing first if the connection was lost.
func (c *Client) Channel() (*amqp.Channel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil || c.conn.IsClosed() {
		if err := c.dialLocked(); err != nil {
			return nil, err
		}
		c.log.Info("rabbitmq connection re-established")
	}
	return c.conn.Channel()
}

// Ping is used by the readiness check.
func (c *Client) Ping(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil || c.conn.IsClosed() {
		return errors.New("rabbitmq connection is closed")
	}
	return nil
}

// Close closes the connection and every channel opened from it.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil || c.conn.IsClosed() {
		return nil
	}
	return c.conn.Close()
}

func declareExchange(ch *amqp.Channel, name string) error {
	if err := ch.ExchangeDeclare(name, amqp.ExchangeTopic, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange %s: %w", name, err)
	}
	return nil
}

// headerCarrier lets OpenTelemetry read and write trace context in AMQP headers.
type headerCarrier amqp.Table

func (h headerCarrier) Get(key string) string {
	v, _ := h[key].(string)
	return v
}

func (h headerCarrier) Set(key, value string) { h[key] = value }

func (h headerCarrier) Keys() []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	return keys
}
