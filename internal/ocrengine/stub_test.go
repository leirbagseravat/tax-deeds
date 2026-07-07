package ocrengine

import (
	"context"
	"strings"
	"testing"
)

func TestStubFirstPageIsFixture(t *testing.T) {
	e, err := New("stub", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if e.Name() != "stub" {
		t.Errorf("name = %q", e.Name())
	}
	if e.NeedsImage() {
		t.Error("stub must not need images")
	}

	res, err := e.RecognizePage(context.Background(), Page{Number: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "45.678") {
		t.Errorf("page 1 text does not look like the fixture: %.60q", res.Text)
	}
	if res.Confidence == nil || *res.Confidence != 0.98 {
		t.Errorf("confidence = %v, want 0.98", res.Confidence)
	}
}

func TestStubLaterPagesAreContinuation(t *testing.T) {
	e, _ := New("stub", Config{})
	res, err := e.RecognizePage(context.Background(), Page{Number: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "página 3") {
		t.Errorf("page 3 text = %q", res.Text)
	}
}

func TestStubFailRate(t *testing.T) {
	e, _ := New("stub", Config{StubFailRate: 1})
	if _, err := e.RecognizePage(context.Background(), Page{Number: 1}); err == nil {
		t.Fatal("want simulated failure with FAIL_RATE=1")
	}
}
