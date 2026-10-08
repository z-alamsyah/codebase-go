package user

//go:generate go tool mockgen -source=ports.go -destination=mocks/mock_ports.go -package=mocks

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/z-alamsyah/codebase-go/internal/model"
)

// The service depends only on these interfaces. Concrete implementations
// live in the data layer (internal/repository, internal/platform) and are
// injected in internal/app. Tests use the generated mocks.

// Repository persists users.
type Repository interface {
	FindByID(ctx context.Context, id uuid.UUID) (model.User, error)
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	Create(ctx context.Context, u *model.User) error
}

// Cache stores users for fast reads (cache-aside).
type Cache interface {
	// Get returns found=false on a cache miss.
	Get(ctx context.Context, id uuid.UUID) (u model.User, found bool, err error)
	Set(ctx context.Context, u model.User) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// Publisher sends domain events to the message broker.
type Publisher interface {
	Publish(ctx context.Context, routingKey string, payload any) error
}

// IdempotencyStore guarantees a piece of work runs once per key.
type IdempotencyStore interface {
	// Acquire returns true only for the first caller of key within ttl.
	Acquire(ctx context.Context, key string, ttl time.Duration) (bool, error)
	// Release removes key so the work can be retried.
	Release(ctx context.Context, key string) error
}

// PasswordHasher hashes plain-text passwords.
type PasswordHasher interface {
	Hash(plain string) (string, error)
}

// Mailer sends emails.
type Mailer interface {
	SendWelcomeEmail(ctx context.Context, to, name string) error
}
