// Command ocr-stub is a development stand-in for the Python OCR service and
// executable documentation of docs/ocr-contract.md: it accepts OCR jobs,
// upserts fixture text into ocr_results, and flips the document status —
// exactly what the real service must do.
//
// Env: DATABASE_URL (use the ocr_service role), PORT (default 9090),
// DELAY (default 2s), FAIL_RATE (0..1, default 0).
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed ocr_fixture.txt
var fixtureText string

type pageRef struct {
	PageNumber int    `json:"page_number"`
	GCSBucket  string `json:"gcs_bucket"`
	GCSObject  string `json:"gcs_object"`
}

type jobRequest struct {
	DocumentID string    `json:"document_id"`
	Pages      []pageRef `json:"pages"`
}

type stub struct {
	log      *slog.Logger
	pool     *pgxpool.Pool
	delay    time.Duration
	failRate float64
}

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

	delay := 2 * time.Second
	if v := os.Getenv("DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			delay = d
		}
	}
	failRate := 0.0
	if v := os.Getenv("FAIL_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			failRate = f
		}
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "9090"
	}

	s := &stub{log: log, pool: pool, delay: delay, failRate: failRate}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/ocr-jobs", s.handleJob)

	log.Info("ocr-stub listening", "port", port, "delay", delay.String(), "fail_rate", failRate)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Error("serve", "error", err)
		os.Exit(1)
	}
}

func (s *stub) handleJob(w http.ResponseWriter, r *http.Request) {
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DocumentID == "" || len(req.Pages) == 0 {
		http.Error(w, `{"error":"invalid job"}`, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintf(w, `{"job_id":%q}`, req.DocumentID)

	go s.process(req)
}

func (s *stub) process(req jobRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	time.Sleep(s.delay)

	if rand.Float64() < s.failRate {
		// Contract: on failure, mark the document failed with a message.
		_, err := s.pool.Exec(ctx, `
			UPDATE documents
			SET status = 'failed', failed_stage = 'ocr', error_message = 'stub: simulated OCR failure', updated_at = now()
			WHERE id = $1::uuid AND status = 'processing'`,
			req.DocumentID)
		s.log.Info("simulated failure", "document_id", req.DocumentID, "error", err)
		return
	}

	// Contract: upsert one ocr_results row per page (duplicate dispatches are
	// expected), then flip the status with a guarded update.
	for _, p := range req.Pages {
		text := fixtureText
		if p.PageNumber > 1 {
			text = fmt.Sprintf("(continuação da matrícula — página %d)", p.PageNumber)
		}
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO ocr_results (document_id, page_number, text, confidence, engine)
			VALUES ($1::uuid, $2, $3, 0.98, 'stub')
			ON CONFLICT (document_id, page_number)
			DO UPDATE SET text = EXCLUDED.text, confidence = EXCLUDED.confidence, engine = EXCLUDED.engine`,
			req.DocumentID, p.PageNumber, text); err != nil {
			s.log.Error("upsert ocr result", "document_id", req.DocumentID, "page", p.PageNumber, "error", err)
			return
		}
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE documents SET status = 'ocr_done', updated_at = now()
		WHERE id = $1::uuid AND status = 'processing'`,
		req.DocumentID)
	if err != nil {
		s.log.Error("flip status", "document_id", req.DocumentID, "error", err)
		return
	}
	s.log.Info("ocr complete", "document_id", req.DocumentID, "pages", len(req.Pages), "status_flipped", tag.RowsAffected() == 1)
}
