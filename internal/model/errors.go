package model

import (
	"errors"
	"strings"
)

// Domain errors. Wrap them with fmt.Errorf("%w: ...") to add context; each
// controller maps them to transport codes (HTTP status, gRPC code) with errors.Is.
var (
	ErrInvalidInput = errors.New("invalid input")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
)

// FieldError describes a validation problem on a single input field.
type FieldError struct {
	Field   string `json:"field" validate:"required" example:"email"`
	Message string `json:"message" validate:"required" example:"must be a valid email address"`
}

// ValidationError carries per-field details and matches ErrInvalidInput.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + " " + f.Message
	}
	return ErrInvalidInput.Error() + ": " + strings.Join(parts, "; ")
}

// Is makes errors.Is(err, ErrInvalidInput) true for validation errors.
func (e *ValidationError) Is(target error) bool { return target == ErrInvalidInput }

// NewValidationError is a shortcut for a single-field validation error.
func NewValidationError(field, message string) error {
	return &ValidationError{Fields: []FieldError{{Field: field, Message: message}}}
}
