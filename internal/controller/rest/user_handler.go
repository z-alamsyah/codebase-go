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
//
//	@ID				getUser
//	@Summary		Get a user by ID
//	@Description	Reads from Redis first (cache-aside), then PostgreSQL.
//	@Tags			users
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"User ID (UUID)"	format(uuid)
//	@Success		200	{object}	DataResponse{data=dto.UserResponse}
//	@Failure		400	{object}	ErrorResponse	"Invalid ID"
//	@Failure		401	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse	"User not found"
//	@Failure		500	{object}	ErrorResponse
//	@Router			/api/v1/users/{id} [get]
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
//
//	@ID				createUser
//	@Summary		Create a user
//	@Description	Registers a user and publishes the user.created event (when MQ is enabled).
//	@Tags			users
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.CreateUserRequest	true	"User to create"
//	@Success		201		{object}	DataResponse{data=dto.UserResponse}
//	@Header			201		{string}	Location		"URL of the created user"
//	@Failure		400		{object}	ErrorResponse	"Validation failed (see error.details)"
//	@Failure		401		{object}	ErrorResponse
//	@Failure		409		{object}	ErrorResponse	"Email already registered"
//	@Failure		500		{object}	ErrorResponse
//	@Router			/api/v1/users [post]
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
