// Package consumer is the message consumer controller layer: it decodes
// message payloads and calls the service.
package consumer

//go:generate go tool mockgen -source=ports.go -destination=mocks/mock_ports.go -package=mocks

import (
	"context"

	"github.com/z-alamsyah/codebase-go/internal/model"
)

// WelcomeSender is the part of the user service this controller needs.
type WelcomeSender interface {
	SendWelcomeEmail(ctx context.Context, ev model.UserCreatedEvent) error
}
