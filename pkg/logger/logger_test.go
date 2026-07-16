package logger

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"WARN":    slog.LevelWarn,
		"error":   slog.LevelError,
		"":        slog.LevelInfo,
		"bogus":   slog.LevelInfo,
		"Verbose": slog.LevelInfo,
	}
	for in, want := range cases {
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNewWithOTLPNoEndpoint(t *testing.T) {
	log, shutdown, err := NewWithOTLP(context.Background(), Options{Level: "info", Service: "test"})
	if err != nil {
		t.Fatalf("NewWithOTLP: %v", err)
	}
	if log == nil {
		t.Fatal("nil logger")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("no-op shutdown returned %v", err)
	}
}

func TestNewWithOTLPEndpoint(t *testing.T) {
	// The exporter must not dial eagerly: construction succeeds and shutdown
	// returns promptly even though nothing listens at the endpoint.
	log, shutdown, err := NewWithOTLP(context.Background(), Options{
		Level:        "info",
		Service:      "test",
		OTLPEndpoint: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("NewWithOTLP: %v", err)
	}
	log.Info("hello", "k", "v")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- shutdown(ctx) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown hung")
	}
}

type recordingHandler struct {
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func TestLevelHandlerFilters(t *testing.T) {
	inner := &recordingHandler{}
	log := slog.New(levelHandler{min: slog.LevelWarn, Handler: inner})

	log.Info("dropped")
	log.Warn("kept")

	if len(inner.records) != 1 {
		t.Fatalf("got %d records, want 1", len(inner.records))
	}
	if inner.records[0].Message != "kept" {
		t.Errorf("got message %q, want %q", inner.records[0].Message, "kept")
	}
}
