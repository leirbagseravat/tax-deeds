// Package routes registers all HTTP endpoints on a ServeMux.
package routes

import (
	"net/http"

	"mortgage/internal/handlers"
)

// Handlers groups the handler dependencies needed to build the mux.
type Handlers struct {
	Health *handlers.Health
}

// NewMux registers every route and returns the mux.
func NewMux(h Handlers) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.Health.Healthz)
	mux.HandleFunc("GET /readyz", h.Health.Readyz)

	return mux
}
