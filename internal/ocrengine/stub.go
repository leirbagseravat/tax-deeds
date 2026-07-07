package ocrengine

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

//go:embed ocr_fixture.txt
var fixtureText string

// stubEngine is an offline backend for local dev, demos and CI: it never
// reads the page image and returns fixture text, so the whole pipeline runs
// without a model endpoint. DELAY simulates latency; FAIL_RATE exercises the
// failure path.
type stubEngine struct {
	delay    time.Duration
	failRate float64
}

func newStub(cfg Config) *stubEngine {
	return &stubEngine{delay: cfg.StubDelay, failRate: cfg.StubFailRate}
}

func (*stubEngine) Name() string { return "stub" }

func (*stubEngine) NeedsImage() bool { return false }

func (s *stubEngine) RecognizePage(ctx context.Context, page Page) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-time.After(s.delay):
	}
	if rand.Float64() < s.failRate {
		return Result{}, errors.New("stub: simulated OCR failure")
	}
	text := fixtureText
	if page.Number > 1 {
		text = fmt.Sprintf("(continuação da matrícula — página %d)", page.Number)
	}
	conf := 0.98
	return Result{Text: text, Confidence: &conf}, nil
}
