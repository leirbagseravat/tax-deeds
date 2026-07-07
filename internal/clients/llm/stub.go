package llm

import (
	"context"
	_ "embed"
	"encoding/json"

	"mortgage/internal/dto"
)

//go:embed stub_fixture.json
var stubFixture []byte

// stub is an offline strategy for local dev, demos and CI: it returns a
// canned extraction matching the OCR stub engine's fixture text
// (internal/ocrengine), so the whole pipeline runs without an API key.
type stub struct{}

func newStub() Extractor { return stub{} }

func (stub) Model() string { return "stub" }

func (stub) ExtractMatricula(ctx context.Context, ocrText string) (dto.ExtractedMatricula, []byte, Usage, error) {
	var m dto.ExtractedMatricula
	if err := json.Unmarshal(stubFixture, &m); err != nil {
		return m, nil, Usage{}, err
	}
	return m, stubFixture, Usage{}, nil
}
