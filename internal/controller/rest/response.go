package rest

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httplog/v3"
	"github.com/go-chi/render"

	"github.com/z-alamsyah/codebase-go/internal/model"
)

// Envelope is the JSON shape of every REST response.
type Envelope struct {
	Data  any        `json:"data,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
	Meta  Meta       `json:"meta"`
}

type ErrorBody struct {
	Code    string             `json:"code"`
	Message string             `json:"message"`
	Details []model.FieldError `json:"details,omitempty"`
}

type Meta struct {
	RequestID string `json:"request_id,omitempty"`
}

// bind decodes the JSON body into v and runs v.Bind (validation) via
// chi/render. A body that cannot be decoded becomes an ErrInvalidInput (400).
func bind(r *http.Request, v render.Binder) error {
	err := render.Bind(r, v)
	if err != nil && !errors.Is(err, model.ErrInvalidInput) {
		return fmt.Errorf("%w: malformed JSON body: %w", model.ErrInvalidInput, err)
	}
	return err
}

func respond(w http.ResponseWriter, r *http.Request, status int, data any) {
	render.Status(r, status)
	render.JSON(w, r, Envelope{Data: data, Meta: Meta{RequestID: middleware.GetReqID(r.Context())}})
}

// respondError maps a domain error to an HTTP status and renders it.
// Internal error details are logged (via the request log) but never returned.
func respondError(w http.ResponseWriter, r *http.Request, err error) {
	status, code := statusFor(err)
	body := &ErrorBody{Code: code, Message: err.Error()}
	if status == http.StatusInternalServerError {
		body.Message = "internal server error"
	}
	var verr *model.ValidationError
	if errors.As(err, &verr) {
		body.Message = "request validation failed"
		body.Details = verr.Fields
	}

	_ = httplog.SetError(r.Context(), err) // attaches the cause to the request log
	render.Status(r, status)
	render.JSON(w, r, Envelope{Error: body, Meta: Meta{RequestID: middleware.GetReqID(r.Context())}})
}

func statusFor(err error) (int, string) {
	switch {
	case errors.Is(err, model.ErrInvalidInput):
		return http.StatusBadRequest, "INVALID_INPUT"
	case errors.Is(err, model.ErrUnauthorized):
		return http.StatusUnauthorized, "UNAUTHORIZED"
	case errors.Is(err, model.ErrForbidden):
		return http.StatusForbidden, "FORBIDDEN"
	case errors.Is(err, model.ErrNotFound):
		return http.StatusNotFound, "NOT_FOUND"
	case errors.Is(err, model.ErrConflict):
		return http.StatusConflict, "CONFLICT"
	default:
		return http.StatusInternalServerError, "INTERNAL"
	}
}
