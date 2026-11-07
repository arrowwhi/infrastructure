package server

import (
	"net/http"

	"github.com/go-chi/render"
)

// HealthChecker interface for custom health checks
type HealthChecker interface {
	Check() error
}

type healthHandler struct {
	checkers []HealthChecker
}

func (h *healthHandler) healthz(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, map[string]string{"status": "ok"})
}

func (h *healthHandler) readiness(w http.ResponseWriter, r *http.Request) {

	// Проверяем дополнительные health checkers
	for _, checker := range h.checkers {
		if err := checker.Check(); err != nil {
			render.Status(r, http.StatusServiceUnavailable)
			render.JSON(w, r, map[string]any{
				"status": "not ready",
				"error":  err.Error(),
			})

			return
		}
	}

	render.JSON(w, r, map[string]string{"status": "ready"})
}
