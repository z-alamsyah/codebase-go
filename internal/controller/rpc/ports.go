// Package rpc is the gRPC controller layer: it maps protobuf messages to
// domain input, calls the service, and maps domain errors to gRPC status codes.
package rpc

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
