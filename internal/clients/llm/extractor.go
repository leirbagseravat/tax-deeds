// Package llm extracts structured matrícula data from OCR text.
//
// The Extractor interface is a strategy: the extraction service depends only
// on it, and cmd/api picks the implementation from config. Swapping LangChain
// for a direct SDK (or another provider) means adding one file that implements
// Extractor and wiring it in the factory below.
package llm

import (
	"context"
	"errors"
	"fmt"

	"tax-deeds/internal/dto"
)

// Usage reports token consumption of one extraction call.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Extractor turns the OCR text of a matrícula into structured data.
type Extractor interface {
	// ExtractMatricula returns the parsed aggregate, the raw model output
	// (stored for audit), and token usage.
	ExtractMatricula(ctx context.Context, ocrText string) (dto.ExtractedMatricula, []byte, Usage, error)
	// Model identifies the underlying model for provenance records.
	Model() string
}

// ErrTransient marks failures worth retrying (rate limits, 5xx, malformed
// output). Terminal errors (bad request, auth) are returned bare.
var ErrTransient = errors.New("transient llm failure")

// Transient wraps err as retryable.
func Transient(err error) error {
	return fmt.Errorf("%w: %w", ErrTransient, err)
}

// IsTransient reports whether the extraction service should retry with backoff.
func IsTransient(err error) bool {
	return errors.Is(err, ErrTransient)
}

// New builds the Extractor strategy selected by provider. baseURL is only
// used by providers that talk to a self-hosted server (ollama).
func New(provider, model, baseURL string) (Extractor, error) {
	switch provider {
	case "anthropic":
		return newLangChainAnthropic(model)
	case "ollama":
		return newLangChainOllama(model, baseURL)
	case "stub":
		return newStub(), nil
	default:
		return nil, fmt.Errorf("unknown LLM provider %q (want anthropic, ollama or stub)", provider)
	}
}
