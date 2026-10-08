package rest

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/render"
)

// Check is one readiness dependency check (database, cache, broker, ...).
type Check struct {
	Name string
	Fn   func(ctx context.Context) error
}

// HealthHandler serves liveness and readiness probes. Only dependencies that
// are enabled are registered as checks.
type HealthHandler struct {
	checks  []Check
	timeout time.Duration
}

func NewHealthHandler(checks ...Check) *HealthHandler {
	return &HealthHandler{checks: checks, timeout: 2 * time.Second}
}

// HealthResponse is the body of /healthz and /readyz.
type HealthResponse struct {
	Status string            `json:"status" example:"ok"`
	Checks map[string]string `json:"checks,omitempty"`
}

// Liveness handles GET /healthz: the process is up. No dependency checks.
//
//	@ID			liveness
//	@Summary	Liveness probe
//	@Tags		health
//	@Produce	json
//	@Success	200	{object}	HealthResponse
//	@Router		/healthz [get]
func (h *HealthHandler) Liveness(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, HealthResponse{Status: "ok"})
}

// Readiness handles GET /readyz: every enabled dependency is reachable.
//
//	@ID				readiness
//	@Summary		Readiness probe
//	@Description	Checks every enabled dependency (PostgreSQL, Redis, RabbitMQ).
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	HealthResponse
//	@Failure		503	{object}	HealthResponse
//	@Router			/readyz [get]
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	res := HealthResponse{Status: "ok", Checks: make(map[string]string, len(h.checks))}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range h.checks {
		wg.Go(func() {
			result := "ok"
			if err := c.Fn(ctx); err != nil {
				result = "error: " + err.Error()
			}
			mu.Lock()
			defer mu.Unlock()
			res.Checks[c.Name] = result
			if result != "ok" {
				res.Status = "unavailable"
			}
		})
	}
	wg.Wait()

	if res.Status != "ok" {
		render.Status(r, http.StatusServiceUnavailable)
	}
	render.JSON(w, r, res)
}
