// Package ocrengine implements the OCR backends behind cmd/ocr.
//
// Engine is a strategy like llm.Extractor: the worker depends only on it and
// cmd/ocr picks the implementation from OCR_ENGINE. Adding a backend means one
// file implementing Engine plus a case in New.
package ocrengine

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Page is one page image to transcribe. PNG is nil when the engine reports
// NeedsImage() == false.
type Page struct {
	Number int
	PNG    []byte
}

// Result is the transcription of one page. A nil Confidence is stored as SQL
// NULL — engines that report no confidence must not fabricate one.
type Result struct {
	Text       string
	Confidence *float64
}

// Engine transcribes page images.
type Engine interface {
	// Name identifies the backend in ocr_results.engine.
	Name() string
	// NeedsImage reports whether RecognizePage reads Page.PNG; when false
	// the worker skips the GCS download entirely.
	NeedsImage() bool
	RecognizePage(ctx context.Context, page Page) (Result, error)
}

// Config carries the settings for every backend; each engine reads only its
// own fields.
type Config struct {
	// stub
	StubDelay    time.Duration
	StubFailRate float64

	// glm: any OpenAI-compatible endpoint serving GLM-OCR (vLLM, SGLang,
	// Ollama).
	GLMBaseURL string
	GLMModel   string
	GLMAPIKey  string

	// glm-maas: Zhipu's hosted layout_parsing API.
	ZhipuAPIKey  string
	ZhipuBaseURL string

	// ModelTimeout bounds each HTTP call to a model endpoint.
	ModelTimeout time.Duration

	Logger *slog.Logger
}

// New builds the Engine selected by name.
func New(name string, cfg Config) (Engine, error) {
	switch name {
	case "stub":
		return newStub(cfg), nil
	case "glm":
		return newGLM(cfg)
	case "glm-maas":
		return newMaaS(cfg)
	default:
		return nil, fmt.Errorf("unknown OCR engine %q (want stub, glm or glm-maas)", name)
	}
}

const (
	retryAttempts    = 2
	retryBackoff     = 2 * time.Second
	maxResponseBytes = 32 << 20
)

// doWithRetry executes the request built by build, retrying once on failures
// that plausibly clear on their own (network errors, 429, 5xx). Other non-2xx
// statuses are terminal. It returns the response body of the successful call.
func doWithRetry(ctx context.Context, client *http.Client, build func() (*http.Request, error)) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < retryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryBackoff):
			}
		}
		req, err := build()
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			return body, nil
		}
		lastErr = fmt.Errorf("model endpoint answered %s: %s", resp.Status, truncate(body, 200))
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return nil, lastErr
		}
	}
	return nil, fmt.Errorf("after %d attempts: %w", retryAttempts, lastErr)
}

func pngDataURI(png []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
