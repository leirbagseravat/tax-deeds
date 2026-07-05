// Package logger provides a slog-based JSON logger configured from a level string.
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a JSON slog.Logger writing to stderr at the given level
// ("debug", "info", "warn", "error"; anything else falls back to info).
func New(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
