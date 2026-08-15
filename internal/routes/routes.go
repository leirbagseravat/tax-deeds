// Package routes registers all HTTP endpoints on a ServeMux.
package routes

import (
	"net/http"

	"tax-deeds/internal/handlers"
)

// Handlers groups the handler dependencies needed to build the mux.
type Handlers struct {
	Health     *handlers.Health
	Documents  *handlers.Documents
	OCR        *handlers.OCR
	Matriculas *handlers.Matriculas
}

// NewMux registers every route and returns the mux.
func NewMux(h Handlers) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.Health.Healthz)
	mux.HandleFunc("GET /readyz", h.Health.Readyz)

	mux.HandleFunc("POST /api/v1/documents", h.Documents.Upload)
	mux.HandleFunc("GET /api/v1/documents/{id}", h.Documents.Get)
	mux.HandleFunc("GET /api/v1/documents/{id}/ocr", h.OCR.Get)

	mux.HandleFunc("GET /api/v1/documents/{id}/matricula", h.Matriculas.ByDocument)
	mux.HandleFunc("GET /api/v1/matriculas/{id}/proprietarios", h.Matriculas.Proprietarios)
	mux.HandleFunc("GET /api/v1/matriculas/{id}/atos", h.Matriculas.Atos)
	mux.HandleFunc("GET /api/v1/matriculas/{id}/onus", h.Matriculas.Onus)

	return mux
}
