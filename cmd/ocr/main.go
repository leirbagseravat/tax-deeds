// Command ocr is the OCR worker: it accepts jobs per docs/ocr-contract.md,
// transcribes the page images with the backend selected by OCR_ENGINE
// (stub, glm, glm-maas), upserts the text into ocr_results and flips the
// document status. The stub engine keeps the whole pipeline runnable without
// a model endpoint.
//
// Env: DATABASE_URL (use the ocr_service role), PORT (default 9090),
// OCR_ENGINE (default stub), OCR_PAGE_CONCURRENCY (default 2),
// OCR_JOB_TIMEOUT (default 8m), OCR_MODEL_TIMEOUT (default 120s),
// LOG_LEVEL (default info), APP_ENV (default dev),
// OTEL_EXPORTER_OTLP_ENDPOINT (optional; ships logs to an OTLP collector).
// Stub only: DELAY (default 2s), FAIL_RATE (0..1, default 0).
// glm: GLM_BASE_URL (required), GLM_MODEL (default glm-ocr), GLM_API_KEY.
// glm-maas: ZHIPU_API_KEY (required), ZHIPU_BASE_URL.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"tax-deeds/internal/clients/gcs"
	"tax-deeds/internal/ocrengine"
	"tax-deeds/pkg/logger"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log, logShutdown, err := logger.NewWithOTLP(ctx, logger.Options{
		Level:        getenv("LOG_LEVEL", "info"),
		Service:      "tax-deeds-ocr",
		Environment:  getenv("APP_ENV", "dev"),
		OTLPEndpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}
	flush := func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := logShutdown(flushCtx); err != nil {
			fmt.Fprintln(os.Stderr, "log flush:", err)
		}
	}
	fatal := func(msg string, args ...any) {
		log.Error(msg, args...)
		flush()
		os.Exit(1)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fatal("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fatal("connect", "error", err)
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
		fatal("build engine", "error", err)
	}

	var fetch imageFetcher
	if engine.NeedsImage() {
		d, err := gcs.NewDownloader(ctx)
		if err != nil {
			fatal("gcs downloader", "error", err)
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
	srv := &http.Server{Addr: ":" + port, Handler: mux}

	errCh := make(chan error, 1)
	go func() {
		log.Info("ocr worker listening", "port", port, "engine", engine.Name())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		fatal("serve", "error", err)
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("shutdown", "error", err)
	}
	pool.Close()
	flush()
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
