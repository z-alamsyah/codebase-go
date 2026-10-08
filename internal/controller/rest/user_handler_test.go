package rest_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/z-alamsyah/codebase-go/internal/controller/rest"
	"github.com/z-alamsyah/codebase-go/internal/controller/rest/mocks"
	"github.com/z-alamsyah/codebase-go/internal/model"
	"github.com/z-alamsyah/codebase-go/internal/service/user"
)

// newRouter mounts only the handlers under test (no middleware), served in memory.
func newRouter(svc rest.UserService) http.Handler {
	h := rest.NewUserHandler(svc)
	r := chi.NewRouter()
	r.Post("/users", h.Create)
	r.Get("/users/{id}", h.GetByID)
	return r
}

func do(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, rest.Envelope) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var env rest.Envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), rec.Body.String())
	return rec, env
}

func TestUserHandler_GetByID(t *testing.T) {
	id := uuid.MustParse("01928f6a-0000-7000-8000-000000000001")
	u := model.User{ID: id, Name: "Andi", Email: "andi@example.com", CreatedAt: time.Now().UTC()}

	tests := []struct {
		name       string
		path       string
		setup      func(m *mocks.MockUserService)
		wantStatus int
		wantCode   string
	}{
		{
			name:       "found",
			path:       "/users/" + id.String(),
			setup:      func(m *mocks.MockUserService) { m.EXPECT().GetByID(gomock.Any(), id).Return(u, nil) },
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid uuid",
			path:       "/users/not-a-uuid",
			setup:      func(*mocks.MockUserService) {},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name: "not found",
			path: "/users/" + id.String(),
			setup: func(m *mocks.MockUserService) {
				m.EXPECT().GetByID(gomock.Any(), id).Return(model.User{}, fmt.Errorf("%w: user", model.ErrNotFound))
			},
			wantStatus: http.StatusNotFound,
			wantCode:   "NOT_FOUND",
		},
		{
			name: "internal error hides details",
			path: "/users/" + id.String(),
			setup: func(m *mocks.MockUserService) {
				m.EXPECT().GetByID(gomock.Any(), id).Return(model.User{}, errors.New("pq: connection refused"))
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mocks.NewMockUserService(gomock.NewController(t))
			tt.setup(svc)

			rec, env := do(t, newRouter(svc), http.MethodGet, tt.path, "")

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantCode == "" {
				assert.Nil(t, env.Error)
				assert.Equal(t, id.String(), env.Data.(map[string]any)["id"])
				return
			}
			require.NotNil(t, env.Error)
			assert.Equal(t, tt.wantCode, env.Error.Code)
			assert.NotContains(t, rec.Body.String(), "connection refused")
		})
	}
}

func TestUserHandler_Create(t *testing.T) {
	id := uuid.MustParse("01928f6a-0000-7000-8000-0000000000aa")
	valid := `{"name":"Andi","email":"andi@example.com","phone":"+6281200000001","password":"Password123!"}`

	tests := []struct {
		name        string
		body        string
		setup       func(m *mocks.MockUserService)
		wantStatus  int
		wantCode    string
		wantDetails []string
	}{
		{
			name: "created",
			body: valid,
			setup: func(m *mocks.MockUserService) {
				in := user.CreateInput{Name: "Andi", Email: "andi@example.com", Phone: "+6281200000001", Password: "Password123!"}
				m.EXPECT().Create(gomock.Any(), in).Return(model.User{ID: id, Name: "Andi", Email: "andi@example.com"}, nil)
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:        "validation errors are reported per field",
			body:        `{"name":"","email":"not-an-email","password":"short"}`,
			setup:       func(*mocks.MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "INVALID_INPUT",
			wantDetails: []string{"name", "email", "password"},
		},
		{
			name:       "malformed JSON",
			body:       `{"name":`,
			setup:      func(*mocks.MockUserService) {},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name: "email conflict",
			body: valid,
			setup: func(m *mocks.MockUserService) {
				m.EXPECT().Create(gomock.Any(), gomock.Any()).Return(model.User{}, fmt.Errorf("%w: email taken", model.ErrConflict))
			},
			wantStatus: http.StatusConflict,
			wantCode:   "CONFLICT",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mocks.NewMockUserService(gomock.NewController(t))
			tt.setup(svc)

			rec, env := do(t, newRouter(svc), http.MethodPost, "/users", tt.body)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantCode == "" {
				assert.Equal(t, "/api/v1/users/"+id.String(), rec.Header().Get("Location"))
				assert.NotContains(t, rec.Body.String(), "password")
				return
			}
			require.NotNil(t, env.Error)
			assert.Equal(t, tt.wantCode, env.Error.Code)
			var fields []string
			for _, d := range env.Error.Details {
				fields = append(fields, d.Field)
			}
			assert.ElementsMatch(t, tt.wantDetails, fields)
		})
	}
}
