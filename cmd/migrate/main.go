// Command migrate applies database migrations embedded from
// internal/store/migrations. Usage: migrate <up|down|status>.
//
// Env: DATABASE_URL (required), LOG_LEVEL (default info), APP_ENV (default
// dev), OTEL_EXPORTER_OTLP_ENDPOINT (optional; ships logs to an OTLP
// collector).
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"mortgage/internal/store/migrations"
	"mortgage/pkg/logger"
)

func main() {
	log, logShutdown, err := logger.NewWithOTLP(context.Background(), logger.Options{
		Level:        getenv("LOG_LEVEL", "info"),
		Service:      "mortgage-migrate",
		Environment:  getenv("APP_ENV", "dev"),
		OTLPEndpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}

	cmd, runErr := run()
	if runErr != nil {
		log.Error("migration failed", "cmd", cmd, "error", runErr)
	} else {
		log.Info("migration complete", "cmd", cmd)
	}

	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := logShutdown(flushCtx); err != nil {
		fmt.Fprintln(os.Stderr, "log flush:", err)
	}
	if runErr != nil {
		os.Exit(1)
	}
}

func run() (string, error) {
	if len(os.Args) < 2 {
		return "", fmt.Errorf("usage: migrate <up|down|status>")
	}
	cmd := os.Args[1]

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return cmd, fmt.Errorf("DATABASE_URL is required")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return cmd, fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return cmd, err
	}

	switch cmd {
	case "up":
		return cmd, goose.Up(db, ".")
	case "down":
		return cmd, goose.Down(db, ".")
	case "status":
		return cmd, goose.Status(db, ".")
	default:
		return cmd, fmt.Errorf("unknown command %q (want up, down or status)", cmd)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
