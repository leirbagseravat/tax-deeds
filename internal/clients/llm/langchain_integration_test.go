//go:build llm_integration

// Run with a real API key:
//
//	ANTHROPIC_API_KEY=... go test -tags llm_integration ./internal/clients/llm -run TestRealExtraction -v
package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRealExtraction(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	fixture, err := os.ReadFile("../../ocrengine/ocr_fixture.txt")
	if err != nil {
		t.Fatal(err)
	}

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "claude-sonnet-5"
	}
	ex, err := New("anthropic", model, "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	m, raw, usage, err := ex.ExtractMatricula(ctx, string(fixture))
	if err != nil {
		t.Fatalf("extract: %v (raw: %s)", err, raw)
	}
	t.Logf("tokens: in=%d out=%d", usage.InputTokens, usage.OutputTokens)

	if !strings.Contains(m.Numero, "45.678") {
		t.Errorf("numero = %q, want 45.678", m.Numero)
	}
	if len(m.Proprietarios) == 0 {
		t.Error("no proprietarios extracted")
	}
	if len(m.Atos) < 5 {
		t.Errorf("atos = %d, want >= 5", len(m.Atos))
	}
	var hasPenhora bool
	for _, o := range m.Onus {
		if strings.Contains(strings.ToLower(o.Tipo), "penhora") {
			hasPenhora = true
		}
	}
	if !hasPenhora {
		t.Errorf("penhora not found in onus: %+v", m.Onus)
	}
}
