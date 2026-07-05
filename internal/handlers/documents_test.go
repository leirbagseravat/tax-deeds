package handlers

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mortgage/internal/store"
)

type fakeIngestor struct {
	created store.Document
	err     error
}

func (f *fakeIngestor) CreateAndProcess(ctx context.Context, filename string, pdf io.Reader) (store.Document, error) {
	if f.err != nil {
		return store.Document{}, f.err
	}
	_, _ = io.Copy(io.Discard, pdf)
	return f.created, nil
}

func (f *fakeIngestor) Get(ctx context.Context, id string) (store.Document, error) {
	if f.err != nil {
		return store.Document{}, f.err
	}
	return f.created, nil
}

func newHandler(t *testing.T) *Documents {
	t.Helper()
	return &Documents{
		Log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		Svc:            &fakeIngestor{created: store.Document{ID: "abc", Status: store.StatusUploaded}},
		MaxUploadBytes: 1 << 20,
	}
}

func multipartBody(t *testing.T, field, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestUploadAcceptsPDF(t *testing.T) {
	body, ct := multipartBody(t, "file", "matricula.pdf", []byte("%PDF-1.4 fake"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	newHandler(t).Upload(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"id":"abc"`) {
		t.Fatalf("body missing id: %s", rec.Body)
	}
}

func TestUploadRejectsNonPDF(t *testing.T) {
	body, ct := multipartBody(t, "file", "notes.txt", []byte("hello world"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	newHandler(t).Upload(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415; body: %s", rec.Code, rec.Body)
	}
}

func TestUploadRejectsMissingFile(t *testing.T) {
	body, ct := multipartBody(t, "wrong_field", "matricula.pdf", []byte("%PDF-1.4"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	newHandler(t).Upload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body)
	}
}

func TestUploadRejectsOversize(t *testing.T) {
	big := append([]byte("%PDF-1.4 "), bytes.Repeat([]byte("a"), 2<<20)...)
	body, ct := multipartBody(t, "file", "big.pdf", big)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	newHandler(t).Upload(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body: %s", rec.Code, rec.Body)
	}
}
