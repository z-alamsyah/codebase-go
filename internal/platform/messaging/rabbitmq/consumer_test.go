package rabbitmq

import (
	"context"
	"errors"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"

	"github.com/z-alamsyah/codebase-go/internal/platform/messaging"
)

func TestDecide(t *testing.T) {
	errBoom := errors.New("boom")
	tests := []struct {
		name    string
		err     error
		attempt int
		want    action
	}{
		{name: "success is acked", err: nil, attempt: 0, want: actionAck},
		{name: "failure is retried", err: errBoom, attempt: 0, want: actionRetry},
		{name: "failure on last allowed retry is retried", err: errBoom, attempt: 2, want: actionRetry},
		{name: "failure after max retries goes to DLQ", err: errBoom, attempt: 3, want: actionDeadLetter},
		{name: "permanent failure skips retries", err: messaging.Permanent(errBoom), attempt: 0, want: actionDeadLetter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, decide(tt.err, tt.attempt, 3))
		})
	}
}

func TestRetryCount(t *testing.T) {
	assert.Equal(t, 0, retryCount(nil))
	assert.Equal(t, 2, retryCount(amqp.Table{headerRetryCount: int32(2)}))
	assert.Equal(t, 3, retryCount(amqp.Table{headerRetryCount: int64(3)}))
	assert.Equal(t, 0, retryCount(amqp.Table{headerRetryCount: "x"}))
}

func TestSafeHandle_RecoversPanic(t *testing.T) {
	err := safeHandle(context.Background(), func(context.Context, messaging.Message) error {
		panic("kaboom")
	}, messaging.Message{})
	assert.ErrorContains(t, err, "handler panic: kaboom")
}
