package dto

import (
	"time"

	"github.com/z-alamsyah/codebase-go/internal/model"
	"github.com/z-alamsyah/codebase-go/internal/service/user"
)

// CreateUserRequest is the input for creating a user (REST body / gRPC message).
//
// The `validate` tags are enforced at runtime and also read by swaggo to
// mark required fields and min/max lengths in the Swagger spec.
type CreateUserRequest struct {
	Name     string `json:"name" validate:"required,min=2,max=100" example:"Andi Pratama"`
	Email    string `json:"email" validate:"required,email,max=255" format:"email" example:"andi@example.com"`
	Phone    string `json:"phone,omitempty" validate:"omitempty,e164" example:"+6281234567890"`
	Password string `json:"password" validate:"required,min=8,max=72" example:"Password123!"` // bcrypt limit is 72 bytes
}

func (r CreateUserRequest) ToInput() user.CreateInput {
	return user.CreateInput{Name: r.Name, Email: r.Email, Phone: r.Phone, Password: r.Password}
}

// UserResponse is the public representation of a user.
type UserResponse struct {
	ID        string    `json:"id" validate:"required" format:"uuid" example:"01928f6a-0000-7000-8000-000000000001"`
	Name      string    `json:"name" validate:"required" example:"Andi Pratama"`
	Email     string    `json:"email" validate:"required" example:"andi@example.com"`
	Phone     string    `json:"phone" validate:"required" example:"+6281200000001"`
	CreatedAt time.Time `json:"created_at" validate:"required" format:"date-time"`
	UpdatedAt time.Time `json:"updated_at" validate:"required" format:"date-time"`
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
