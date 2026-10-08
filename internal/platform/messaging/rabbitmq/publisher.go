package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/z-alamsyah/codebase-go/internal/platform/logger"
	"github.com/z-alamsyah/codebase-go/internal/platform/messaging"
)

const tracerName = "github.com/z-alamsyah/codebase-go/internal/platform/messaging/rabbitmq"

// Publisher publishes JSON envelopes to a topic exchange with publisher
// confirms, so Publish returns only after the broker accepted the message.
type Publisher struct {
	client   *Client
	exchange string
	log      *slog.Logger

	mu sync.Mutex
	ch *amqp.Channel
}

var _ messaging.Publisher = (*Publisher)(nil)

// NewPublisher opens a confirm-mode channel and declares the exchange.
func NewPublisher(client *Client, exchange string, log *slog.Logger) (*Publisher, error) {
	p := &Publisher{client: client, exchange: exchange, log: log}
	if _, err := p.channel(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Publisher) channel() (*amqp.Channel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch != nil && !p.ch.IsClosed() {
		return p.ch, nil
	}
	ch, err := p.client.Channel()
	if err != nil {
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("enable publisher confirms: %w", err)
	}
	if err := declareExchange(ch, p.exchange); err != nil {
		_ = ch.Close()
		return nil, err
	}
	p.ch = ch
	return ch, nil
}

// Publish wraps payload in a messaging.Envelope and sends it with routingKey.
func (p *Publisher) Publish(ctx context.Context, routingKey string, payload any) (err error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	env := messaging.Envelope{
		ID:         uuid.NewString(),
		Type:       routingKey,
		OccurredAt: time.Now().UTC(),
		Version:    1,
		Payload:    body,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("encode envelope: %w", err)
	}

	ctx, span := otel.Tracer(tracerName).Start(ctx, "publish "+routingKey,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.operation.type", "send"),
			attribute.String("messaging.destination.name", p.exchange),
			attribute.String("messaging.rabbitmq.destination.routing_key", routingKey),
			attribute.String("messaging.message.id", env.ID),
		))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	headers := amqp.Table{}
	otel.GetTextMapPropagator().Inject(ctx, headerCarrier(headers))

	ch, err := p.channel()
	if err != nil {
		return err
	}
	confirm, err := ch.PublishWithDeferredConfirmWithContext(ctx, p.exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    env.ID,
		Type:         routingKey,
		Timestamp:    env.OccurredAt,
		Headers:      headers,
		Body:         data,
	})
	if err != nil {
		return fmt.Errorf("publish %s: %w", routingKey, err)
	}
	acked, err := confirm.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("wait publish confirm: %w", err)
	}
	if !acked {
		return errors.New("broker rejected the message (nack)")
	}

	p.log.DebugContext(ctx, "message published",
		slog.String("routing_key", routingKey),
		slog.String("message_id", env.ID),
		slog.String(logger.KeyPayload, string(body)))
	return nil
}
