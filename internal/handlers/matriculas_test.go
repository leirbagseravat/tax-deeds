package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"tax-deeds/internal/dto"
	"tax-deeds/internal/services/matriculas"
	"tax-deeds/internal/store"
)

// fakeMatriculaReader returns canned responses, or err when set.
type fakeMatriculaReader struct {
	err      error
	response dto.MatriculaResponse
	gotKind  string
	gotState string
}

func (f *fakeMatriculaReader) ByDocument(_ context.Context, _ string) (dto.MatriculaResponse, error) {
	return f.response, f.err
}

func (f *fakeMatriculaReader) Proprietarios(_ context.Context, _ string) (dto.ProprietariosResponse, error) {
	return dto.ProprietariosResponse{MatriculaID: f.response.ID}, f.err
}

func (f *fakeMatriculaReader) Atos(_ context.Context, _, kind string) (dto.AtosResponse, error) {
	f.gotKind = kind
	return dto.AtosResponse{MatriculaID: f.response.ID, Kind: kind}, f.err
}

func (f *fakeMatriculaReader) Onus(_ context.Context, _, status string) (dto.OnusResponse, error) {
	f.gotState = status
	return dto.OnusResponse{MatriculaID: f.response.ID, Status: status}, f.err
}

// matriculaServer mirrors the matrícula route patterns from internal/routes
// (which can't be imported here without a cycle).
func matriculaServer(t *testing.T, fake *fakeMatriculaReader) *httptest.Server {
	t.Helper()
	h := &Matriculas{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Svc: fake}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/documents/{id}/matricula", h.ByDocument)
	mux.HandleFunc("GET /api/v1/matriculas/{id}/proprietarios", h.Proprietarios)
	mux.HandleFunc("GET /api/v1/matriculas/{id}/atos", h.Atos)
	mux.HandleFunc("GET /api/v1/matriculas/{id}/onus", h.Onus)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) (*http.Response, map[string]any) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return res, body
}

func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func TestGetMatriculaOK(t *testing.T) {
	fake := &fakeMatriculaReader{response: dto.MatriculaResponse{
		ID: "m1", DocumentID: "d1",
		ExtractedMatricula: dto.ExtractedMatricula{Numero: "45.678"},
	}}
	srv := matriculaServer(t, fake)

	res, body := get(t, srv.URL+"/api/v1/documents/d1/matricula")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if body["numero"] != "45.678" || body["document_id"] != "d1" {
		t.Errorf("body = %v", body)
	}
}

func TestGetMatriculaNotFound(t *testing.T) {
	srv := matriculaServer(t, &fakeMatriculaReader{err: store.ErrNotFound})
	res, body := get(t, srv.URL+"/api/v1/documents/nope/matricula")
	if res.StatusCode != http.StatusNotFound || errCode(body) != "not_found" {
		t.Fatalf("status = %d, code = %s", res.StatusCode, errCode(body))
	}
}

func TestGetMatriculaNotReady(t *testing.T) {
	srv := matriculaServer(t, &fakeMatriculaReader{err: &matriculas.NotReadyError{Status: "processing"}})
	res, body := get(t, srv.URL+"/api/v1/documents/d1/matricula")
	if res.StatusCode != http.StatusConflict || errCode(body) != "not_ready" {
		t.Fatalf("status = %d, code = %s", res.StatusCode, errCode(body))
	}
}

func TestGetMatriculaFailedDocument(t *testing.T) {
	srv := matriculaServer(t, &fakeMatriculaReader{err: &matriculas.FailedError{Stage: "ocr", Message: "boom"}})
	res, body := get(t, srv.URL+"/api/v1/documents/d1/matricula")
	if res.StatusCode != http.StatusConflict || errCode(body) != "failed" {
		t.Fatalf("status = %d, code = %s", res.StatusCode, errCode(body))
	}
}

func TestAtosKindValidation(t *testing.T) {
	fake := &fakeMatriculaReader{}
	srv := matriculaServer(t, fake)

	res, body := get(t, srv.URL+"/api/v1/matriculas/m1/atos?kind=banana")
	if res.StatusCode != http.StatusBadRequest || errCode(body) != "invalid_kind" {
		t.Fatalf("status = %d, code = %s", res.StatusCode, errCode(body))
	}

	if res, _ := get(t, srv.URL+"/api/v1/matriculas/m1/atos?kind=averbacao"); res.StatusCode != http.StatusOK {
		t.Fatalf("valid kind rejected: %d", res.StatusCode)
	}
	if fake.gotKind != "averbacao" {
		t.Errorf("kind not passed through: %q", fake.gotKind)
	}
}

func TestOnusStatusValidation(t *testing.T) {
	fake := &fakeMatriculaReader{}
	srv := matriculaServer(t, fake)

	res, body := get(t, srv.URL+"/api/v1/matriculas/m1/onus?status=pendente")
	if res.StatusCode != http.StatusBadRequest || errCode(body) != "invalid_status" {
		t.Fatalf("status = %d, code = %s", res.StatusCode, errCode(body))
	}

	if res, _ := get(t, srv.URL+"/api/v1/matriculas/m1/onus?status=ativo"); res.StatusCode != http.StatusOK {
		t.Fatalf("valid status rejected: %d", res.StatusCode)
	}
	if fake.gotState != "ativo" {
		t.Errorf("status not passed through: %q", fake.gotState)
	}
}

func TestProprietariosNotFound(t *testing.T) {
	srv := matriculaServer(t, &fakeMatriculaReader{err: store.ErrNotFound})
	res, body := get(t, srv.URL+"/api/v1/matriculas/nope/proprietarios")
	if res.StatusCode != http.StatusNotFound || errCode(body) != "not_found" {
		t.Fatalf("status = %d, code = %s", res.StatusCode, errCode(body))
	}
}
