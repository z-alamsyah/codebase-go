package consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/z-alamsyah/codebase-go/internal/controller/consumer"
	"github.com/z-alamsyah/codebase-go/internal/controller/consumer/mocks"
	"github.com/z-alamsyah/codebase-go/internal/model"
	"github.com/z-alamsyah/codebase-go/internal/platform/messaging"
)

func message(payload string) messaging.Message {
	return messaging.Message{Envelope: messaging.Envelope{ID: "m-1", Type: model.EventUserCreated, Payload: json.RawMessage(payload)}}
}

func TestUserHandler_HandleUserCreated(t *testing.T) {
	ev := model.UserCreatedEvent{UserID: "u-1", Name: "Andi", Email: "andi@example.com"}
	raw, _ := json.Marshal(ev)

	t.Run("valid event calls the service", func(t *testing.T) {
		svc := mocks.NewMockWelcomeSender(gomock.NewController(t))
		svc.EXPECT().SendWelcomeEmail(gomock.Any(), ev).Return(nil)

		err := consumer.NewUserHandler(svc).HandleUserCreated(context.Background(), message(string(raw)))
		require.NoError(t, err)
	})

	t.Run("service error is returned so the message is retried", func(t *testing.T) {
		svc := mocks.NewMockWelcomeSender(gomock.NewController(t))
		svc.EXPECT().SendWelcomeEmail(gomock.Any(), ev).Return(errors.New("smtp down"))

		err := consumer.NewUserHandler(svc).HandleUserCreated(context.Background(), message(string(raw)))
		require.Error(t, err)
		assert.False(t, messaging.IsPermanent(err))
	})

	t.Run("malformed payload is permanent", func(t *testing.T) {
		svc := mocks.NewMockWelcomeSender(gomock.NewController(t))

		err := consumer.NewUserHandler(svc).HandleUserCreated(context.Background(), message(`not-json`))
		assert.True(t, messaging.IsPermanent(err))
	})

	t.Run("missing required fields is permanent", func(t *testing.T) {
		svc := mocks.NewMockWelcomeSender(gomock.NewController(t))

		err := consumer.NewUserHandler(svc).HandleUserCreated(context.Background(), message(`{"name":"x"}`))
		assert.True(t, messaging.IsPermanent(err))
	})
}
