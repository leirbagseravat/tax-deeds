// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime configuration for the orchestrator.
type Config struct {
	Port     string
	LogLevel string

	DatabaseURL string

	GCSBucket string

	MaxUploadMB int64
	MaxPages    int

	OCRServiceURL      string
	OCRDispatchTimeout time.Duration

	LLMProvider string
	LLMModel    string
	LLMBaseURL  string

	PollInterval          time.Duration
	StuckTimeout          time.Duration
	MaxExtractionAttempts int
}

// Load reads configuration from the environment, applying dev-friendly defaults.
// DATABASE_URL is required.
func Load() (Config, error) {
	cfg := Config{
		Port:          getenv("PORT", "8080"),
		LogLevel:      getenv("LOG_LEVEL", "info"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		GCSBucket:     getenv("GCS_BUCKET", "matriculas"),
		OCRServiceURL: getenv("OCR_SERVICE_URL", "http://localhost:9090"),
		LLMProvider:   getenv("LLM_PROVIDER", "anthropic"),
		LLMModel:      getenv("LLM_MODEL", "claude-sonnet-5"),
		LLMBaseURL:    getenv("LLM_BASE_URL", "http://localhost:11434"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	var err error
	if cfg.MaxUploadMB, err = intEnv("MAX_UPLOAD_MB", 25); err != nil {
		return Config{}, err
	}
	maxPages, err := intEnv("MAX_PAGES", 100)
	if err != nil {
		return Config{}, err
	}
	cfg.MaxPages = int(maxPages)
	maxAttempts, err := intEnv("MAX_EXTRACTION_ATTEMPTS", 3)
	if err != nil {
		return Config{}, err
	}
	cfg.MaxExtractionAttempts = int(maxAttempts)

	if cfg.OCRDispatchTimeout, err = durationEnv("OCR_DISPATCH_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.PollInterval, err = durationEnv("POLL_INTERVAL", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.StuckTimeout, err = durationEnv("STUCK_TIMEOUT", 10*time.Minute); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func intEnv(key string, def int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", key, v)
	}
	return n, nil
}

func durationEnv(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q", key, v)
	}
	return d, nil
}
