// Command api runs the mortgage orchestrator HTTP server.
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

	"mortgage/internal/config"
	"mortgage/internal/handlers"
	"mortgage/internal/routes"
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

	mux := routes.NewMux(routes.Handlers{
		Health: &handlers.Health{DB: pool},
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
	return srv.Shutdown(shutdownCtx)
}
