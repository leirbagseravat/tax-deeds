package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mortgage/internal/clients/gcs"
	"mortgage/internal/clients/llm"
	"mortgage/internal/clients/ocr"
	"mortgage/internal/config"
	"mortgage/internal/handlers"
	"mortgage/internal/routes"
	"mortgage/internal/services/extraction"
	"mortgage/internal/services/ingest"
	"mortgage/internal/services/matriculas"
	"mortgage/internal/services/pdfconvert"
	"mortgage/internal/store"
	"mortgage/pkg/logger"
	"mortgage/pkg/middleware"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(cfg.LogLevel)

	pool, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	storage, err := gcs.New(ctx, cfg.GCSBucket)
	if err != nil {
		return err
	}
	defer storage.Close()

	docs := store.NewDocuments(pool)
	ocrResults := store.NewOCR(pool)
	matStore := store.NewMatriculas(pool)
	converter := &pdfconvert.Poppler{MaxPages: cfg.MaxPages}
	ocrClient := ocr.New(cfg.OCRServiceURL, cfg.OCRDispatchTimeout)
	ingestSvc := ingest.New(log, docs, ocrResults, storage, converter, ocrClient)

	// The extraction poller is optional: without a working LLM strategy the
	// pipeline still runs and documents park at ocr_done.
	if extractor, err := llm.New(cfg.LLMProvider, cfg.LLMModel, cfg.LLMBaseURL); err != nil {
		log.Warn("LLM extractor disabled; documents will stop at ocr_done", "error", err)
	} else {
		poller := &extraction.Poller{
			Log:          log,
			Docs:         docs,
			Ingest:       ingestSvc,
			Extraction:   extraction.NewService(log, docs, ocrResults, matStore, extractor),
			Interval:     cfg.PollInterval,
			StuckTimeout: cfg.StuckTimeout,
			MaxAttempts:  cfg.MaxExtractionAttempts,
		}
		go poller.Run(ctx)
	}

	mux := routes.NewMux(routes.Handlers{
		Health: &handlers.Health{DB: pool},
		Documents: &handlers.Documents{
			Log:            log,
			Svc:            ingestSvc,
			MaxUploadBytes: cfg.MaxUploadMB << 20,
		},
		OCR:        &handlers.OCR{Log: log, Svc: ingestSvc},
		Matriculas: &handlers.Matriculas{Log: log, Svc: matriculas.New(docs, matStore)},
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           middleware.Chain(mux, middleware.Recover(log), middleware.RequestID, middleware.Logging(log)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	// Wait for in-flight background ingestion; the janitor recovers anything
	// that doesn't finish in time.
	if err := ingestSvc.Shutdown(shutdownCtx); err != nil {
		log.Warn("background work did not finish before shutdown", "error", err)
	}
	return nil
}
