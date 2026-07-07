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

func maasConfig(baseURL string) Config {
	return Config{ZhipuAPIKey: "zhipu-key", ZhipuBaseURL: baseURL, ModelTimeout: 5 * time.Second}
}

func TestMaaSRequestShape(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/layout_parsing" {
			t.Errorf("path = %q, want /layout_parsing", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "zhipu-key" {
			t.Errorf("Authorization = %q, want the bare API key", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"md_content":"# MATRÍCULA\ntexto"}`))
	}))
	defer srv.Close()

	e, err := newMaaS(maasConfig(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.RecognizePage(context.Background(), Page{Number: 1, PNG: []byte("fake png")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "# MATRÍCULA\ntexto" {
		t.Errorf("text = %q", res.Text)
	}
	if res.Confidence != nil {
		t.Errorf("confidence = %v, want nil", *res.Confidence)
	}
	if got["model"] != "glm-ocr" {
		t.Errorf("model = %q, want glm-ocr", got["model"])
	}
	if !strings.HasPrefix(got["file"], "data:image/png;base64,") {
		t.Errorf("file %q lacks data-URI prefix", got["file"])
	}
}

func TestMaaSOversizedImageIsTerminal(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	e, _ := newMaaS(maasConfig(srv.URL))
	// 8 MiB of raw bytes base64-encode past the 10 MiB data-URI cap.
	if _, err := e.RecognizePage(context.Background(), Page{Number: 1, PNG: make([]byte, 8<<20)}); err == nil {
		t.Fatal("want error for oversized image")
	}
	if hits != 0 {
		t.Errorf("hits = %d, want 0 (no HTTP call for oversized image)", hits)
	}
}

func TestMaaSRequiresAPIKey(t *testing.T) {
	if _, err := New("glm-maas", Config{}); err == nil {
		t.Fatal("want error without ZHIPU_API_KEY")
	}
}

func TestExtractMaaSText(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{"top-level md_content", `{"md_content":"texto"}`, "texto", false},
		{"data.md_content", `{"data":{"md_content":"texto"}}`, "texto", false},
		{"data.content", `{"data":{"content":"texto"}}`, "texto", false},
		{"data.text", `{"data":{"text":"texto"}}`, "texto", false},
		{"openai choices", `{"choices":[{"message":{"content":"texto"}}]}`, "texto", false},
		{"empty md_content is valid", `{"md_content":""}`, "", false},
		{"unknown shape", `{"result":"texto"}`, "", true},
		{"not json", `<html>`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractMaaSText([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
		})
	}
}
