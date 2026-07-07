package ocrengine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testConfig(baseURL string) Config {
	return Config{GLMBaseURL: baseURL, ModelTimeout: 5 * time.Second}
}

func glmOK(text string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": text}}},
	})
	return string(b)
}

func TestGLMRequestShape(t *testing.T) {
	var got struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(glmOK("MATRÍCULA Nº 45.678")))
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	cfg.GLMAPIKey = "secret"
	e, err := newGLM(cfg)
	if err != nil {
		t.Fatal(err)
	}

	res, err := e.RecognizePage(context.Background(), Page{Number: 1, PNG: []byte("fake png")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "MATRÍCULA Nº 45.678" {
		t.Errorf("text = %q", res.Text)
	}
	if res.Confidence != nil {
		t.Errorf("confidence = %v, want nil", *res.Confidence)
	}
	if got.Model != "glm-ocr" {
		t.Errorf("model = %q, want glm-ocr", got.Model)
	}
	if len(got.Messages) != 1 || len(got.Messages[0].Content) != 2 {
		t.Fatalf("messages = %+v, want 1 message with 2 content parts", got.Messages)
	}
	if got.Messages[0].Content[0].Text != glmPrompt {
		t.Errorf("prompt = %q, want %q", got.Messages[0].Content[0].Text, glmPrompt)
	}
	if !strings.HasPrefix(got.Messages[0].Content[1].ImageURL.URL, "data:image/png;base64,") {
		t.Errorf("image url %q lacks data-URI prefix", got.Messages[0].Content[1].ImageURL.URL[:40])
	}
}

func TestGLMRetriesServerError(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(glmOK("ok")))
	}))
	defer srv.Close()

	e, _ := newGLM(testConfig(srv.URL))
	res, err := e.RecognizePage(context.Background(), Page{Number: 1, PNG: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "ok" || hits != 2 {
		t.Errorf("text = %q, hits = %d; want ok after 2 hits", res.Text, hits)
	}
}

func TestGLMBadRequestIsTerminal(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "bad param", http.StatusBadRequest)
	}))
	defer srv.Close()

	e, _ := newGLM(testConfig(srv.URL))
	if _, err := e.RecognizePage(context.Background(), Page{Number: 1, PNG: []byte("x")}); err == nil {
		t.Fatal("want error")
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1 (400 must not be retried)", hits)
	}
}

func TestGLMEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	e, _ := newGLM(testConfig(srv.URL))
	if _, err := e.RecognizePage(context.Background(), Page{Number: 1, PNG: []byte("x")}); err == nil {
		t.Fatal("want error for empty choices")
	}
}

func TestGLMRequiresBaseURL(t *testing.T) {
	if _, err := New("glm", Config{}); err == nil {
		t.Fatal("want error without GLM_BASE_URL")
	}
}

func TestNewUnknownEngine(t *testing.T) {
	if _, err := New("tesseract", Config{}); err == nil {
		t.Fatal("want error for unknown engine")
	}
}
