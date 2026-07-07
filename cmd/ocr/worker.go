package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"mortgage/internal/ocrengine"
)

type pageRef struct {
	PageNumber int    `json:"page_number"`
	GCSBucket  string `json:"gcs_bucket"`
	GCSObject  string `json:"gcs_object"`
}

type jobRequest struct {
	DocumentID string    `json:"document_id"`
	Pages      []pageRef `json:"pages"`
}

// store is the contract's write surface (see docs/ocr-contract.md), matched
// by pgStore and by fakes in tests.
type store interface {
	UpsertPage(ctx context.Context, documentID string, page int, text string, confidence *float64, engine string) error
	MarkOCRDone(ctx context.Context, documentID string) (bool, error)
	MarkFailed(ctx context.Context, documentID, message string) error
}

// imageFetcher downloads one page image; nil when the engine needs no image.
type imageFetcher interface {
	Download(ctx context.Context, bucket, object string) ([]byte, error)
}

// errStore marks a failed result write. The document is left in 'processing'
// so the orchestrator's janitor re-dispatches the job, instead of being
// flipped to 'failed' like an OCR error.
var errStore = errors.New("store write failed")

type worker struct {
	log         *slog.Logger
	store       store
	fetch       imageFetcher
	engine      ocrengine.Engine
	jobTimeout  time.Duration
	concurrency int
}

func (w *worker) handleJob(rw http.ResponseWriter, r *http.Request) {
	var req jobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DocumentID == "" || len(req.Pages) == 0 {
		http.Error(rw, `{"error":"invalid job"}`, http.StatusBadRequest)
		return
	}
	rw.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintf(rw, `{"job_id":%q}`, req.DocumentID)

	go w.process(req)
}

func (w *worker) process(req jobRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), w.jobTimeout)
	defer cancel()

	if err := w.recognizeAll(ctx, req); err != nil {
		if errors.Is(err, errStore) {
			// Contract: don't flip the status; the janitor re-dispatches.
			w.log.Error("result write failed, leaving document in processing",
				"document_id", req.DocumentID, "error", err)
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("ocr: job timed out after %s", w.jobTimeout)
		}
		w.fail(req.DocumentID, err)
		return
	}

	flipped, err := w.store.MarkOCRDone(ctx, req.DocumentID)
	if err != nil {
		w.log.Error("flip status", "document_id", req.DocumentID, "error", err)
		return
	}
	w.log.Info("ocr complete", "document_id", req.DocumentID, "pages", len(req.Pages),
		"engine", w.engine.Name(), "status_flipped", flipped)
}

// recognizeAll runs the pages through the engine with bounded concurrency,
// upserting each result as it completes. The first error cancels the rest.
func (w *worker) recognizeAll(ctx context.Context, req jobRequest) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, w.concurrency)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for _, p := range req.Pages {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(p pageRef) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := w.page(ctx, req.DocumentID, p); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()
	if firstErr == nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return firstErr
}

func (w *worker) page(ctx context.Context, documentID string, p pageRef) error {
	page := ocrengine.Page{Number: p.PageNumber}
	if w.engine.NeedsImage() {
		png, err := w.fetch.Download(ctx, p.GCSBucket, p.GCSObject)
		if err != nil {
			return fmt.Errorf("ocr: page %d: %w", p.PageNumber, err)
		}
		page.PNG = png
	}
	res, err := w.engine.RecognizePage(ctx, page)
	if err != nil {
		return fmt.Errorf("ocr: page %d: %w", p.PageNumber, err)
	}
	if err := w.store.UpsertPage(ctx, documentID, p.PageNumber, res.Text, res.Confidence, w.engine.Name()); err != nil {
		return fmt.Errorf("%w: page %d: %w", errStore, p.PageNumber, err)
	}
	return nil
}

// fail marks the document failed on a fresh context: the job context may
// already be expired or canceled when we get here.
func (w *worker) fail(documentID string, cause error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	msg := cause.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	if err := w.store.MarkFailed(ctx, documentID, msg); err != nil {
		w.log.Error("mark failed", "document_id", documentID, "error", err, "cause", cause)
		return
	}
	w.log.Info("ocr failed", "document_id", documentID, "error", msg)
}
