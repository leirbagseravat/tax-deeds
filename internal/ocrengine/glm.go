package ocrengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// glmPrompt is GLM-OCR's official task prompt for transcription; the model
// also accepts "Table Recognition:" and "Formula Recognition:" for cropped
// regions, but whole pages go through plain text recognition.
const glmPrompt = "Text Recognition:"

// glmEngine calls GLM-OCR through any OpenAI-compatible chat-completions
// endpoint (vLLM, SGLang, Ollama), one page image per call.
type glmEngine struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
}

func newGLM(cfg Config) (*glmEngine, error) {
	if cfg.GLMBaseURL == "" {
		return nil, errors.New("GLM_BASE_URL is required for the glm engine")
	}
	model := cfg.GLMModel
	if model == "" {
		model = "glm-ocr"
	}
	return &glmEngine{
		baseURL: strings.TrimRight(cfg.GLMBaseURL, "/"),
		model:   model,
		apiKey:  cfg.GLMAPIKey,
		http:    &http.Client{Timeout: cfg.ModelTimeout},
	}, nil
}

func (*glmEngine) Name() string { return "glm-ocr" }

func (*glmEngine) NeedsImage() bool { return true }

func (g *glmEngine) RecognizePage(ctx context.Context, page Page) (Result, error) {
	// Sampling parameters mirror the glmocr SDK defaults. repetition_penalty
	// is not part of the OpenAI spec: vLLM honors it, Ollama ignores it.
	body, err := json.Marshal(map[string]any{
		"model": g.model,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": glmPrompt},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": pngDataURI(page.PNG)}},
				},
			},
		},
		"max_tokens":         8192,
		"temperature":        0.0,
		"top_p":              0.00001,
		"repetition_penalty": 1.1,
	})
	if err != nil {
		return Result{}, fmt.Errorf("glm: marshal request: %w", err)
	}

	respBody, err := doWithRetry(ctx, g.http, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/v1/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if g.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+g.apiKey)
		}
		return req, nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("glm: %w", err)
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Result{}, fmt.Errorf("glm: decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Result{}, fmt.Errorf("glm: response has no choices: %s", truncate(respBody, 200))
	}
	return Result{Text: parsed.Choices[0].Message.Content}, nil
}
