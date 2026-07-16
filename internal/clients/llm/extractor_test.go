package llm

import (
	"strings"
	"testing"
)

func TestNewSelectsProvider(t *testing.T) {
	if _, err := New("stub", "", ""); err != nil {
		t.Errorf("stub: %v", err)
	}

	// Constructing the ollama client does not dial the server.
	ex, err := New("ollama", "some-model", "http://localhost:11434")
	if err != nil {
		t.Fatalf("ollama: %v", err)
	}
	if ex.Model() != "some-model" {
		t.Errorf("ollama Model() = %q", ex.Model())
	}
}

func TestNewRejectsUnknownProvider(t *testing.T) {
	_, err := New("bogus", "m", "")
	if err == nil {
		t.Fatal("unknown provider accepted")
	}
	if !strings.Contains(err.Error(), `"bogus"`) {
		t.Errorf("error does not name the provider: %v", err)
	}
}
