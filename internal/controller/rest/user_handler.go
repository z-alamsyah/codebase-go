package rest

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/z-alamsyah/codebase-go/internal/controller/dto"
	"github.com/z-alamsyah/codebase-go/internal/model"
)

type UserHandler struct {
	svc UserService
}

func NewUserHandler(svc UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// createUserRequest plugs the shared DTO into chi/render: render.Bind decodes
// the body and then calls Bind, where validation runs.
type createUserRequest struct {
	dto.CreateUserRequest
}

func (c *createUserRequest) Bind(*http.Request) error {
	return dto.Validate(c.CreateUserRequest)
}

// GetByID handles GET /api/v1/users/{id}.
func (h *UserHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, r, model.NewValidationError("id", "must be a valid UUID"))
		return
	}

	u, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respond(w, r, http.StatusOK, dto.NewUserResponse(u))
}

// Create handles POST /api/v1/users.
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := bind(r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	u, err := h.svc.Create(r.Context(), req.ToInput())
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/users/"+u.ID.String())
	respond(w, r, http.StatusCreated, dto.NewUserResponse(u))
}
