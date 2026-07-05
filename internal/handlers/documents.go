package handlers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"mortgage/internal/dto"
	"mortgage/internal/store"
	"mortgage/pkg/response"
)

// Ingestor is the slice of the ingest service the handlers need.
type Ingestor interface {
	CreateAndProcess(ctx context.Context, filename string, pdf io.Reader) (store.Document, error)
	Get(ctx context.Context, id string) (store.Document, error)
}

// Documents serves upload and status endpoints.
type Documents struct {
	Log            *slog.Logger
	Svc            Ingestor
	MaxUploadBytes int64
}

var pdfMagic = []byte("%PDF-")

// Upload accepts a multipart "file" field containing a PDF and answers 202
// with the new document ID; processing continues in the background.
func (h *Documents) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.MaxUploadBytes)

	file, header, err := r.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.Error(w, http.StatusRequestEntityTooLarge, "too_large", "upload exceeds the size limit")
			return
		}
		response.Error(w, http.StatusBadRequest, "bad_request", `multipart field "file" is required`)
		return
	}
	defer file.Close()

	head := make([]byte, len(pdfMagic))
	if _, err := io.ReadFull(file, head); err != nil || !bytes.Equal(head, pdfMagic) {
		response.Error(w, http.StatusUnsupportedMediaType, "not_pdf", "file is not a PDF")
		return
	}
	pdf := io.MultiReader(bytes.NewReader(head), file)

	doc, err := h.Svc.CreateAndProcess(r.Context(), header.Filename, pdf)
	if err != nil {
		h.Log.Error("upload failed", "error", err)
		response.Error(w, http.StatusInternalServerError, "internal", "could not store the document")
		return
	}
	response.JSON(w, http.StatusAccepted, dto.UploadResponse{ID: doc.ID, Status: doc.Status})
}

// Get answers the document status DTO.
func (h *Documents) Get(w http.ResponseWriter, r *http.Request) {
	doc, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			response.Error(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		h.Log.Error("get document failed", "error", err)
		response.Error(w, http.StatusInternalServerError, "internal", "could not load the document")
		return
	}
	response.JSON(w, http.StatusOK, dto.DocumentFromStore(doc))
}
