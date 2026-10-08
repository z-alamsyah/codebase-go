package dto

import (
	"time"

	"github.com/z-alamsyah/codebase-go/internal/model"
	"github.com/z-alamsyah/codebase-go/internal/service/user"
)

// CreateUserRequest is the input for creating a user (REST body / gRPC message).
type CreateUserRequest struct {
	Name     string `json:"name" validate:"required,min=2,max=100"`
	Email    string `json:"email" validate:"required,email,max=255"`
	Phone    string `json:"phone" validate:"omitempty,e164"`
	Password string `json:"password" validate:"required,min=8,max=72"` // bcrypt limit is 72 bytes
}

func (r CreateUserRequest) ToInput() user.CreateInput {
	return user.CreateInput{Name: r.Name, Email: r.Email, Phone: r.Phone, Password: r.Password}
}

// UserResponse is the public representation of a user.
type UserResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewUserResponse(u model.User) UserResponse {
	return UserResponse{
		ID:        u.ID.String(),
		Name:      u.Name,
		Email:     u.Email,
		Phone:     u.Phone,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}
