package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/z-alamsyah/codebase-go/internal/model"
	"github.com/z-alamsyah/codebase-go/internal/platform/messaging"
)

type UserHandler struct {
	svc WelcomeSender
}

func NewUserHandler(svc WelcomeSender) *UserHandler {
	return &UserHandler{svc: svc}
}

// HandleUserCreated consumes model.EventUserCreated. Invalid payloads are
// marked permanent so they go to the dead letter queue without retries.
func (h *UserHandler) HandleUserCreated(ctx context.Context, msg messaging.Message) error {
	var ev model.UserCreatedEvent
	if err := json.Unmarshal(msg.Payload, &ev); err != nil {
		return messaging.Permanent(fmt.Errorf("decode %s payload: %w", model.EventUserCreated, err))
	}
	if ev.UserID == "" || ev.Email == "" {
		return messaging.Permanent(errors.New("user_id and email are required"))
	}
	return h.svc.SendWelcomeEmail(ctx, ev)
}
