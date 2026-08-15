// Package handlers contains HTTP request handlers. Handlers only parse/validate
// requests and call services; they contain no database logic.
package handlers

import (
	"context"
	"net/http"

	"tax-deeds/pkg/response"
)

// Pinger reports whether a dependency (the database) is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Health serves liveness and readiness probes.
type Health struct {
	DB Pinger
}

// Healthz is a liveness probe: the process is up.
func (h *Health) Healthz(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz is a readiness probe: dependencies are reachable.
func (h *Health) Readyz(w http.ResponseWriter, r *http.Request) {
	if err := h.DB.Ping(r.Context()); err != nil {
		response.Error(w, http.StatusServiceUnavailable, "db_unavailable", "database is unreachable")
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
