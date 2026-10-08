// Package model holds domain entities and domain errors shared by every layer.
// It must not depend on any transport, database or framework package.
package model

import (
	"time"

	"github.com/google/uuid"
)

// User is the domain representation of a user account.
type User struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Phone        string    `json:"phone"`
	PasswordHash string    `json:"-"` // never serialized (logs, cache, responses)
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// EventUserCreated is the routing key / event type published after a user is created.
const EventUserCreated = "user.created"

// UserCreatedEvent is the payload of EventUserCreated.
type UserCreatedEvent struct {
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}
