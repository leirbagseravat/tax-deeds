// Command ocr is the OCR worker: it accepts jobs per docs/ocr-contract.md,
// transcribes the page images with the backend selected by OCR_ENGINE
// (stub, glm, glm-maas), upserts the text into ocr_results and flips the
// document status. The stub engine keeps the whole pipeline runnable without
// a model endpoint.
//
// Env: DATABASE_URL (use the ocr_service role), PORT (default 9090),
// OCR_ENGINE (default stub), OCR_PAGE_CONCURRENCY (default 2),
// OCR_JOB_TIMEOUT (default 8m), OCR_MODEL_TIMEOUT (default 120s).
// Stub only: DELAY (default 2s), FAIL_RATE (0..1, default 0).
// glm: GLM_BASE_URL (required), GLM_MODEL (default glm-ocr), GLM_API_KEY.
// glm-maas: ZHIPU_API_KEY (required), ZHIPU_BASE_URL.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mortgage/internal/clients/gcs"
	"mortgage/internal/ocrengine"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Error("connect", "error", err)
		os.Exit(1)
	}

	engineName := getenv("OCR_ENGINE", "stub")
	engine, err := ocrengine.New(engineName, ocrengine.Config{
		StubDelay:    durationEnv("DELAY", 2*time.Second),
		StubFailRate: floatEnv("FAIL_RATE", 0),
		GLMBaseURL:   os.Getenv("GLM_BASE_URL"),
		GLMModel:     os.Getenv("GLM_MODEL"),
		GLMAPIKey:    os.Getenv("GLM_API_KEY"),
		ZhipuAPIKey:  os.Getenv("ZHIPU_API_KEY"),
		ZhipuBaseURL: os.Getenv("ZHIPU_BASE_URL"),
		ModelTimeout: durationEnv("OCR_MODEL_TIMEOUT", 120*time.Second),
		Logger:       log,
	})
	if err != nil {
		log.Error("build engine", "error", err)
		os.Exit(1)
	}

	var fetch imageFetcher
	if engine.NeedsImage() {
		d, err := gcs.NewDownloader(context.Background())
		if err != nil {
			log.Error("gcs downloader", "error", err)
			os.Exit(1)
		}
		fetch = d
	}

	w := &worker{
		log:         log,
		store:       &pgStore{pool: pool},
		fetch:       fetch,
		engine:      engine,
		jobTimeout:  durationEnv("OCR_JOB_TIMEOUT", 8*time.Minute),
		concurrency: intEnv("OCR_PAGE_CONCURRENCY", 2),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/ocr-jobs", w.handleJob)

	port := getenv("PORT", "9090")
	log.Info("ocr worker listening", "port", port, "engine", engine.Name())
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Error("serve", "error", err)
		os.Exit(1)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func floatEnv(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
