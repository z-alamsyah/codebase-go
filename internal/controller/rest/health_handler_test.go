package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/z-alamsyah/codebase-go/internal/controller/rest"
)

func TestHealthHandler(t *testing.T) {
	ok := func(context.Context) error { return nil }
	down := func(context.Context) error { return errors.New("connection refused") }

	tests := []struct {
		name       string
		checks     []rest.Check
		wantStatus int
		wantBody   string
	}{
		{name: "all dependencies healthy", checks: []rest.Check{{Name: "postgres", Fn: ok}, {Name: "redis", Fn: ok}}, wantStatus: http.StatusOK, wantBody: "ok"},
		{name: "one dependency down", checks: []rest.Check{{Name: "postgres", Fn: ok}, {Name: "redis", Fn: down}}, wantStatus: http.StatusServiceUnavailable, wantBody: "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			rest.NewHealthHandler(tt.checks...).Readiness(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			assert.Equal(t, tt.wantStatus, rec.Code)
			var body struct {
				Status string            `json:"status"`
				Checks map[string]string `json:"checks"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.wantBody, body.Status)
			assert.Len(t, body.Checks, len(tt.checks))
		})
	}

	t.Run("liveness never checks dependencies", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rest.NewHealthHandler(rest.Check{Name: "redis", Fn: down}).Liveness(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}
