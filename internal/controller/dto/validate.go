// Package dto holds request/response shapes shared by the REST and gRPC
// controllers, plus input validation. DTOs are mapped to domain types here so
// the service never sees transport-specific structs.
package dto

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/z-alamsyah/codebase-go/internal/model"
)

var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	// Report JSON field names (e.g. "email") instead of Go names ("Email").
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})
	return v
}

// Validate checks struct tags and returns *model.ValidationError on failure.
func Validate(s any) error {
	err := validate.Struct(s)
	if err == nil {
		return nil
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return err
	}
	fields := make([]model.FieldError, len(verrs))
	for i, fe := range verrs {
		fields[i] = model.FieldError{Field: fe.Field(), Message: message(fe)}
	}
	return &model.ValidationError{Fields: fields}
}

func message(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "e164":
		return "must be a phone number in E.164 format, e.g. +6281234567890"
	case "min":
		return fmt.Sprintf("must be at least %s characters", fe.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", fe.Param())
	case "uuid":
		return "must be a valid UUID"
	default:
		return "failed on rule " + fe.Tag()
	}
}
