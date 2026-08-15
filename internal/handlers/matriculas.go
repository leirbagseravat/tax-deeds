package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"tax-deeds/internal/dto"
	"tax-deeds/internal/services/matriculas"
	"tax-deeds/internal/store"
	"tax-deeds/pkg/response"
)

// MatriculaReader is the slice of the matrículas service these endpoints need.
type MatriculaReader interface {
	ByDocument(ctx context.Context, documentID string) (dto.MatriculaResponse, error)
	Proprietarios(ctx context.Context, matriculaID string) (dto.ProprietariosResponse, error)
	Atos(ctx context.Context, matriculaID, kind string) (dto.AtosResponse, error)
	Onus(ctx context.Context, matriculaID, status string) (dto.OnusResponse, error)
}

// Matriculas serves structured registry data extracted from documents.
type Matriculas struct {
	Log *slog.Logger
	Svc MatriculaReader
}

// ByDocument returns the full nested aggregate for a document. 409 not_ready
// tells clients to keep polling; 404 means the id is wrong.
func (h *Matriculas) ByDocument(w http.ResponseWriter, r *http.Request) {
	resp, err := h.Svc.ByDocument(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, err, "document")
		return
	}
	response.JSON(w, http.StatusOK, resp)
}

// Proprietarios returns current owners and the cadeia dominial.
func (h *Matriculas) Proprietarios(w http.ResponseWriter, r *http.Request) {
	resp, err := h.Svc.Proprietarios(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, err, "matricula")
		return
	}
	response.JSON(w, http.StatusOK, resp)
}

// Atos returns the act timeline, optionally filtered with ?kind=registro|averbacao.
func (h *Matriculas) Atos(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != "registro" && kind != "averbacao" {
		response.Error(w, http.StatusBadRequest, "invalid_kind", "kind must be registro or averbacao")
		return
	}
	resp, err := h.Svc.Atos(r.Context(), r.PathValue("id"), kind)
	if err != nil {
		h.writeError(w, err, "matricula")
		return
	}
	response.JSON(w, http.StatusOK, resp)
}

// Onus returns liens, optionally filtered with ?status=ativo|cancelado.
func (h *Matriculas) Onus(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "" && status != "ativo" && status != "cancelado" {
		response.Error(w, http.StatusBadRequest, "invalid_status", "status must be ativo or cancelado")
		return
	}
	resp, err := h.Svc.Onus(r.Context(), r.PathValue("id"), status)
	if err != nil {
		h.writeError(w, err, "matricula")
		return
	}
	response.JSON(w, http.StatusOK, resp)
}

func (h *Matriculas) writeError(w http.ResponseWriter, err error, what string) {
	var notReady *matriculas.NotReadyError
	var failed *matriculas.FailedError
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Error(w, http.StatusNotFound, "not_found", what+" not found")
	case errors.As(err, &notReady):
		response.Error(w, http.StatusConflict, "not_ready",
			"matricula not extracted yet (document status: "+notReady.Status+")")
	case errors.As(err, &failed):
		response.Error(w, http.StatusConflict, "failed",
			"document processing failed at "+failed.Stage+": "+failed.Message)
	default:
		h.Log.Error("matricula read failed", "error", err)
		response.Error(w, http.StatusInternalServerError, "internal", "could not load matricula")
	}
}
