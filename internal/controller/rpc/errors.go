package rpc

import (
	"context"
	"errors"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/z-alamsyah/codebase-go/internal/model"
)

// toStatus maps a domain error to a gRPC status. Validation errors carry
// field details (google.rpc.BadRequest). Internal error details are added
// to the request log but never returned to the client.
func toStatus(ctx context.Context, err error) error {
	code := codeFor(err)
	msg := err.Error()
	if code == codes.Internal {
		msg = "internal error"
	}
	// Shows the real cause in the "finished call" log line.
	logging.AddFields(ctx, logging.Fields{"grpc.error_cause", err.Error()})

	st := status.New(code, msg)
	var verr *model.ValidationError
	if errors.As(err, &verr) {
		br := &errdetails.BadRequest{}
		for _, f := range verr.Fields {
			br.FieldViolations = append(br.FieldViolations, &errdetails.BadRequest_FieldViolation{Field: f.Field, Description: f.Message})
		}
		if detailed, derr := st.WithDetails(br); derr == nil {
			st = detailed
		}
	}
	return st.Err()
}

func codeFor(err error) codes.Code {
	switch {
	case errors.Is(err, model.ErrInvalidInput):
		return codes.InvalidArgument
	case errors.Is(err, model.ErrUnauthorized):
		return codes.Unauthenticated
	case errors.Is(err, model.ErrForbidden):
		return codes.PermissionDenied
	case errors.Is(err, model.ErrNotFound):
		return codes.NotFound
	case errors.Is(err, model.ErrConflict):
		return codes.AlreadyExists
	default:
		return codes.Internal
	}
}
