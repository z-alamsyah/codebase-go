// Package user contains the business logic for user accounts.
package user

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/z-alamsyah/codebase-go/internal/model"
)

const welcomeEmailDedupTTL = 24 * time.Hour

// Deps groups the service dependencies so the constructor stays readable.
type Deps struct {
	Repo        Repository
	Cache       Cache
	Publisher   Publisher
	Idempotency IdempotencyStore
	Hasher      PasswordHasher
	Mailer      Mailer
	Logger      *slog.Logger
}

// Service implements user use cases. It is shared by every transport
// (REST, gRPC, consumer).
type Service struct {
	repo   Repository
	cache  Cache
	pub    Publisher
	idem   IdempotencyStore
	hasher PasswordHasher
	mailer Mailer
	log    *slog.Logger
}

func NewService(d Deps) *Service {
	return &Service{
		repo:   d.Repo,
		cache:  d.Cache,
		pub:    d.Publisher,
		idem:   d.Idempotency,
		hasher: d.Hasher,
		mailer: d.Mailer,
		log:    d.Logger,
	}
}

// CreateInput is the data needed to register a user. Field format is
// validated by the controller; the service enforces business rules.
type CreateInput struct {
	Name     string
	Email    string
	Phone    string
	Password string
}

// GetByID returns a user using the cache-aside pattern. Cache failures are
// logged and never fail the request.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (model.User, error) {
	u, found, err := s.cache.Get(ctx, id)
	switch {
	case err != nil:
		s.log.WarnContext(ctx, "cache get failed, falling back to database", slog.String("user_id", id.String()), slog.Any("error", err))
	case found:
		return u, nil
	}

	u, err = s.repo.FindByID(ctx, id)
	if err != nil {
		return model.User{}, err
	}

	if err := s.cache.Set(ctx, u); err != nil {
		s.log.WarnContext(ctx, "cache set failed", slog.String("user_id", id.String()), slog.Any("error", err))
	}
	return u, nil
}

// Create registers a new user and publishes model.EventUserCreated.
func (s *Service) Create(ctx context.Context, in CreateInput) (model.User, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))

	exists, err := s.repo.ExistsByEmail(ctx, email)
	if err != nil {
		return model.User{}, fmt.Errorf("check email: %w", err)
	}
	if exists {
		return model.User{}, fmt.Errorf("%w: email %s is already registered", model.ErrConflict, email)
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return model.User{}, fmt.Errorf("hash password: %w", err)
	}

	u := model.User{
		Name:         strings.TrimSpace(in.Name),
		Email:        email,
		Phone:        strings.TrimSpace(in.Phone),
		PasswordHash: hash,
	}
	if err := s.repo.Create(ctx, &u); err != nil {
		return model.User{}, err
	}

	// The user is already stored, so a publish failure is logged instead of
	// failing the request. For guaranteed delivery use the transactional
	// outbox pattern (see README).
	event := model.UserCreatedEvent{UserID: u.ID.String(), Name: u.Name, Email: u.Email, CreatedAt: u.CreatedAt}
	if err := s.pub.Publish(ctx, model.EventUserCreated, event); err != nil {
		s.log.ErrorContext(ctx, "publish event failed", slog.String("event", model.EventUserCreated), slog.String("user_id", event.UserID), slog.Any("error", err))
	}

	return u, nil
}

// SendWelcomeEmail handles model.EventUserCreated. Brokers deliver at least
// once, so the idempotency store makes sure each user gets one email even if
// the same message is processed twice.
func (s *Service) SendWelcomeEmail(ctx context.Context, ev model.UserCreatedEvent) error {
	key := "welcome-email:" + ev.UserID

	first, err := s.idem.Acquire(ctx, key, welcomeEmailDedupTTL)
	if err != nil {
		return fmt.Errorf("acquire idempotency key: %w", err)
	}
	if !first {
		s.log.InfoContext(ctx, "welcome email already sent, skipping", slog.String("user_id", ev.UserID))
		return nil
	}

	if err := s.mailer.SendWelcomeEmail(ctx, ev.Email, ev.Name); err != nil {
		// Release the key so the retry can send the email again.
		if rerr := s.idem.Release(ctx, key); rerr != nil {
			s.log.WarnContext(ctx, "release idempotency key failed", slog.String("key", key), slog.Any("error", rerr))
		}
		return fmt.Errorf("send welcome email: %w", err)
	}
	return nil
}
