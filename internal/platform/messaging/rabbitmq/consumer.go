package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/z-alamsyah/codebase-go/internal/platform/logger"
	"github.com/z-alamsyah/codebase-go/internal/platform/messaging"
)

const (
	headerRetryCount       = "x-retry-count"
	headerLastError        = "x-last-error"
	headerOriginalRouteKey = "x-original-routing-key"

	maxReconnectDelay = 30 * time.Second
)

// ConsumerConfig controls topology and retry behaviour.
type ConsumerConfig struct {
	Exchange   string
	Prefetch   int
	MaxRetries int
	RetryDelay time.Duration
}

// Consumer runs one worker pool per route.
//
// Topology declared for every route with queue Q:
//   - Q        bound to the exchange with the route's routing key
//   - Q.retry  messages wait here for RetryDelay (per-message TTL), then go back to Q
//   - Q.dlq    dead letter queue for messages that failed MaxRetries times
type Consumer struct {
	client *Client
	cfg    ConsumerConfig
	routes []messaging.Route
	log    *slog.Logger
}

func NewConsumer(client *Client, cfg ConsumerConfig, routes []messaging.Route, log *slog.Logger) *Consumer {
	return &Consumer{client: client, cfg: cfg, routes: routes, log: log}
}

// Run blocks until ctx is canceled and every in-flight message is finished.
// Lost connections are re-established with exponential backoff.
func (c *Consumer) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, r := range c.routes {
		wg.Go(func() { c.runRoute(ctx, r) })
	}
	wg.Wait()
	return nil
}

func (c *Consumer) runRoute(ctx context.Context, r messaging.Route) {
	delay := time.Second
	for {
		started, err := c.consume(ctx, r)
		if ctx.Err() != nil {
			return
		}
		if started {
			delay = time.Second
		}
		c.log.WarnContext(ctx, "consumer stopped, reconnecting",
			slog.String("queue", r.Queue), slog.Duration("retry_in", delay), slog.Any("error", err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, maxReconnectDelay)
	}
}

// consume returns when the delivery channel closes: on shutdown (ctx canceled)
// or when the channel/connection drops. started reports whether consuming began.
func (c *Consumer) consume(ctx context.Context, r messaging.Route) (started bool, err error) {
	ch, err := c.client.Channel()
	if err != nil {
		return false, err
	}
	defer func() { _ = ch.Close() }()

	if err := c.declare(ch, r); err != nil {
		return false, err
	}
	if err := ch.Qos(c.cfg.Prefetch, 0, false); err != nil {
		return false, fmt.Errorf("set prefetch: %w", err)
	}
	closed := ch.NotifyClose(make(chan *amqp.Error, 1))

	// ConsumeWithContext cancels the consumer when ctx is done; the broker then
	// stops delivering and the deliveries channel is closed.
	deliveries, err := ch.ConsumeWithContext(ctx, r.Queue, "", false, false, false, false, nil)
	if err != nil {
		return false, fmt.Errorf("consume %s: %w", r.Queue, err)
	}
	c.log.InfoContext(ctx, "consumer started",
		slog.String("queue", r.Queue), slog.String("routing_key", r.RoutingKey), slog.Int("prefetch", c.cfg.Prefetch))

	// Handlers keep running during shutdown (graceful drain), so they get a
	// context that is not canceled together with ctx.
	handlerCtx := context.WithoutCancel(ctx)
	var wg sync.WaitGroup
	for range c.cfg.Prefetch {
		wg.Go(func() {
			for d := range deliveries {
				c.handle(handlerCtx, ch, r, d)
			}
		})
	}
	wg.Wait()

	select {
	case amqpErr := <-closed:
		if amqpErr != nil {
			return true, amqpErr
		}
	default:
	}
	if ctx.Err() == nil {
		return true, errors.New("delivery channel closed")
	}
	return true, nil
}

func (c *Consumer) declare(ch *amqp.Channel, r messaging.Route) error {
	if err := declareExchange(ch, c.cfg.Exchange); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(r.Queue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare queue %s: %w", r.Queue, err)
	}
	if err := ch.QueueBind(r.Queue, r.RoutingKey, c.cfg.Exchange, false, nil); err != nil {
		return fmt.Errorf("bind queue %s: %w", r.Queue, err)
	}
	// Expired messages in the retry queue are dead-lettered through the
	// default exchange straight back to the main queue (not to every queue
	// bound to the routing key). The delay is set per message (Expiration),
	// not as a queue argument, so CONSUMER_RETRY_DELAY can change without
	// re-creating the queue.
	if _, err := ch.QueueDeclare(retryQueue(r.Queue), true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": r.Queue,
	}); err != nil {
		return fmt.Errorf("declare retry queue: %w", err)
	}
	if _, err := ch.QueueDeclare(deadLetterQueue(r.Queue), true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead letter queue: %w", err)
	}
	return nil
}

func (c *Consumer) handle(ctx context.Context, ch *amqp.Channel, r messaging.Route, d amqp.Delivery) {
	start := time.Now()
	attempt := retryCount(d.Headers)
	routingKey := d.RoutingKey
	if orig, ok := d.Headers[headerOriginalRouteKey].(string); ok {
		routingKey = orig
	}

	ctx = otel.GetTextMapPropagator().Extract(ctx, headerCarrier(d.Headers))
	ctx, span := otel.Tracer(tracerName).Start(ctx, "process "+r.Queue,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.operation.type", "process"),
			attribute.String("messaging.destination.name", r.Queue),
			attribute.String("messaging.rabbitmq.destination.routing_key", routingKey),
			attribute.String("messaging.message.id", d.MessageId),
			attribute.Int("messaging.retry.attempt", attempt),
		))
	defer span.End()

	payload := string(d.Body)
	var env messaging.Envelope
	err := json.Unmarshal(d.Body, &env)
	if err != nil {
		err = messaging.Permanent(fmt.Errorf("decode envelope: %w", err))
	} else {
		payload = string(env.Payload)
		ctx = logger.WithRequestID(ctx, env.ID)
		err = safeHandle(ctx, r.Handler, messaging.Message{Envelope: env, Queue: r.Queue, RoutingKey: routingKey, Attempt: attempt})
	}

	act := decide(err, attempt, c.cfg.MaxRetries)
	attrs := []slog.Attr{
		slog.String("queue", r.Queue),
		slog.String("routing_key", routingKey),
		slog.String("message_id", d.MessageId),
		slog.Int("attempt", attempt),
		slog.String(logger.KeyPayload, payload),
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		attrs = append(attrs, slog.Any("error", err))
	}

	var settleErr error
	switch act {
	case actionAck:
		settleErr = d.Ack(false)
	case actionRetry:
		settleErr = c.forward(ctx, ch, d, retryQueue(r.Queue), routingKey, attempt+1, c.cfg.RetryDelay, err)
	case actionDeadLetter:
		settleErr = c.forward(ctx, ch, d, deadLetterQueue(r.Queue), routingKey, attempt, 0, err)
	}
	if settleErr != nil {
		// Could not move the message: put it back on the queue so it is not lost.
		_ = d.Nack(false, true)
		attrs = append(attrs, slog.Any("settle_error", settleErr))
		act = actionRequeue
	}

	level := slog.LevelInfo
	switch act {
	case actionRetry:
		level = slog.LevelWarn
	case actionDeadLetter, actionRequeue:
		level = slog.LevelError
	}
	msg := fmt.Sprintf("CONSUME %s => %s (%s)", r.Queue, act, time.Since(start).Round(time.Microsecond))
	c.log.LogAttrs(ctx, level, msg, attrs...)
}

// forward republishes d to target through the default exchange, then acks
// the original delivery. ttl > 0 sets the per-message expiration (retry delay).
func (c *Consumer) forward(ctx context.Context, ch *amqp.Channel, d amqp.Delivery, target, routingKey string, attempt int, ttl time.Duration, cause error) error {
	headers := amqp.Table{}
	for k, v := range d.Headers {
		headers[k] = v
	}
	headers[headerRetryCount] = clampInt32(int64(attempt))
	headers[headerOriginalRouteKey] = routingKey
	// Keep retries in the same trace as the attempt that failed.
	otel.GetTextMapPropagator().Inject(ctx, headerCarrier(headers))
	if cause != nil {
		msg := cause.Error()
		if len(msg) > 512 {
			msg = msg[:512]
		}
		headers[headerLastError] = msg
	}

	msg := amqp.Publishing{
		ContentType:  d.ContentType,
		DeliveryMode: amqp.Persistent,
		MessageId:    d.MessageId,
		Type:         d.Type,
		Timestamp:    d.Timestamp,
		Headers:      headers,
		Body:         d.Body,
	}
	if ttl > 0 {
		msg.Expiration = strconv.FormatInt(ttl.Milliseconds(), 10)
	}
	err := ch.PublishWithContext(ctx, "", target, false, false, msg)
	if err != nil {
		return fmt.Errorf("forward to %s: %w", target, err)
	}
	return d.Ack(false)
}

type action string

const (
	actionAck        action = "ack"
	actionRetry      action = "retry"
	actionDeadLetter action = "dead-letter"
	actionRequeue    action = "requeue"
)

// decide picks what happens to a message after its handler returned err.
func decide(err error, attempt, maxRetries int) action {
	switch {
	case err == nil:
		return actionAck
	case messaging.IsPermanent(err), attempt >= maxRetries:
		return actionDeadLetter
	default:
		return actionRetry
	}
}

func safeHandle(ctx context.Context, h messaging.HandlerFunc, msg messaging.Message) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("handler panic: %v", rec)
		}
	}()
	return h(ctx, msg)
}

func retryCount(h amqp.Table) int {
	switch v := h[headerRetryCount].(type) {
	case int32:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	case int16:
		return int(v)
	case int8:
		return int(v)
	default:
		return 0
	}
}

// clampInt32 converts to int32 (the AMQP header integer type) without overflow.
func clampInt32(n int64) int32 {
	return int32(max(min(n, math.MaxInt32), math.MinInt32)) //nolint:gosec // bounded above
}

func retryQueue(q string) string      { return q + ".retry" }
func deadLetterQueue(q string) string { return q + ".dlq" }
