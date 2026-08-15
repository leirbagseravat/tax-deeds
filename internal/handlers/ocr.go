package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"tax-deeds/internal/dto"
	"tax-deeds/internal/store"
	"tax-deeds/pkg/response"
)

// OCRReader is the slice of the ingest service the OCR endpoint needs.
type OCRReader interface {
	OCRResults(ctx context.Context, id string) (store.Document, []store.OCRResult, error)
}

// OCR serves recognized text for a document.
type OCR struct {
	Log *slog.Logger
	Svc OCRReader
}

// Get returns the OCR pages for a document. While OCR has not finished it
// answers 409 not_ready so clients know to keep polling.
func (h *OCR) Get(w http.ResponseWriter, r *http.Request) {
	doc, results, err := h.Svc.OCRResults(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			response.Error(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		h.Log.Error("get ocr failed", "error", err)
		response.Error(w, http.StatusInternalServerError, "internal", "could not load OCR results")
		return
	}
	if len(results) == 0 {
		response.Error(w, http.StatusConflict, "not_ready", "OCR has not completed for this document")
		return
	}

	resp := dto.OCRResponse{DocumentID: doc.ID, Status: doc.Status}
	for _, res := range results {
		resp.Pages = append(resp.Pages, dto.OCRPage{
			PageNumber: res.PageNumber,
			Text:       res.Text,
			Confidence: res.Confidence,
			Engine:     res.Engine,
		})
	}
	response.JSON(w, http.StatusOK, resp)
}
