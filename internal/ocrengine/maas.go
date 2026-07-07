package ocrengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// maasMaxFileBytes is the documented per-image cap of the layout_parsing API.
const maasMaxFileBytes = 10 << 20

// maasEngine calls GLM-OCR through Zhipu's hosted layout_parsing API, one
// page image per call, sent as a base64 data URI.
type maasEngine struct {
	baseURL string
	apiKey  string
	http    *http.Client
	log     *slog.Logger
}

func newMaaS(cfg Config) (*maasEngine, error) {
	if cfg.ZhipuAPIKey == "" {
		return nil, errors.New("ZHIPU_API_KEY is required for the glm-maas engine")
	}
	baseURL := cfg.ZhipuBaseURL
	if baseURL == "" {
		baseURL = "https://open.bigmodel.cn/api/paas/v4"
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &maasEngine{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  cfg.ZhipuAPIKey,
		http:    &http.Client{Timeout: cfg.ModelTimeout},
		log:     log,
	}, nil
}

func (*maasEngine) Name() string { return "glm-ocr-maas" }

func (*maasEngine) NeedsImage() bool { return true }

func (m *maasEngine) RecognizePage(ctx context.Context, page Page) (Result, error) {
	uri := pngDataURI(page.PNG)
	if len(uri) > maasMaxFileBytes {
		return Result{}, fmt.Errorf("maas: page image data URI is %d bytes, over the 10MB API limit", len(uri))
	}
	body, err := json.Marshal(map[string]string{"model": "glm-ocr", "file": uri})
	if err != nil {
		return Result{}, fmt.Errorf("maas: marshal request: %w", err)
	}

	respBody, err := doWithRetry(ctx, m.http, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/layout_parsing", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", m.apiKey)
		return req, nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("maas: %w", err)
	}

	text, err := extractMaaSText(respBody)
	if err != nil {
		m.log.Error("maas: unrecognized response shape", "page", page.Number, "body", truncate(respBody, 2000))
		return Result{}, fmt.Errorf("maas: %w", err)
	}
	return Result{Text: text}, nil
}

// extractMaaSText pulls the parsed document text out of a layout_parsing
// response. Public docs don't pin the exact shape, so probe the plausible
// fields in order; the caller logs the raw body when nothing matches.
func extractMaaSText(body []byte) (string, error) {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if s, ok := doc["md_content"].(string); ok {
		return s, nil
	}
	if data, ok := doc["data"].(map[string]any); ok {
		for _, key := range []string{"md_content", "content", "text"} {
			if s, ok := data[key].(string); ok {
				return s, nil
			}
		}
	}
	if choices, ok := doc["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if msg, ok := choice["message"].(map[string]any); ok {
				if s, ok := msg["content"].(string); ok {
					return s, nil
				}
			}
		}
	}
	return "", errors.New("no recognized text field in response")
}
