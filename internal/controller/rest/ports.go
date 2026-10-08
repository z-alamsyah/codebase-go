// Package rest is the REST (HTTP/JSON) controller layer: it binds and
// validates input, calls the service, and renders the JSON envelope.
package rest

//go:generate go tool mockgen -source=ports.go -destination=mocks/mock_ports.go -package=mocks

import (
	"context"

	"github.com/google/uuid"

	"github.com/z-alamsyah/codebase-go/internal/model"
	"github.com/z-alamsyah/codebase-go/internal/service/user"
)

// UserService is the part of the user service this controller needs.
type UserService interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.User, error)
	Create(ctx context.Context, in user.CreateInput) (model.User, error)
}
