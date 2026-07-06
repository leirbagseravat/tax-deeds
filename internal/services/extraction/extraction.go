package extraction

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mortgage/internal/clients/llm"
	"mortgage/internal/store"
)

// Service turns OCR text into the persisted matrícula aggregate.
type Service struct {
	log        *slog.Logger
	docs       *store.Documents
	ocr        *store.OCR
	matriculas *store.Matriculas
	extractor  llm.Extractor
}

func NewService(log *slog.Logger, docs *store.Documents, ocr *store.OCR, matriculas *store.Matriculas, extractor llm.Extractor) *Service {
	return &Service{log: log, docs: docs, ocr: ocr, matriculas: matriculas, extractor: extractor}
}

// ExtractDocument processes one claimed document (already in "extracting").
// Transient LLM failures send it back to the queue with backoff; terminal
// ones mark it failed.
func (s *Service) ExtractDocument(ctx context.Context, doc store.Document) {
	err := s.extract(ctx, doc)
	if err == nil {
		return
	}
	if llm.IsTransient(err) {
		backoff := retryBackoff(doc.ExtractionAttempts)
		s.log.Warn("extraction failed transiently, requeueing",
			"document_id", doc.ID, "attempt", doc.ExtractionAttempts, "backoff", backoff.String(), "error", err)
		if rerr := s.docs.ReturnForRetry(ctx, doc.ID, backoff); rerr != nil {
			s.log.Error("requeue for retry", "document_id", doc.ID, "error", rerr)
		}
		return
	}
	s.log.Error("extraction failed", "document_id", doc.ID, "error", err)
	if _, ferr := s.docs.MarkFailed(ctx, doc.ID, store.StatusExtracting, "extraction", err.Error()); ferr != nil {
		s.log.Error("mark failed", "document_id", doc.ID, "error", ferr)
	}
}

func (s *Service) extract(ctx context.Context, doc store.Document) error {
	results, err := s.ocr.ResultsByDocument(ctx, doc.ID)
	if err != nil {
		return llm.Transient(fmt.Errorf("load ocr results: %w", err))
	}
	if len(results) == 0 {
		return fmt.Errorf("document is ocr_done but has no ocr_results rows")
	}

	var b strings.Builder
	for _, r := range results {
		fmt.Fprintf(&b, "--- Página %d ---\n%s\n", r.PageNumber, r.Text)
	}

	extracted, raw, usage, err := s.extractor.ExtractMatricula(ctx, b.String())
	if err != nil {
		return err
	}

	Normalize(&extracted)

	if _, err := s.matriculas.SaveAggregate(ctx, doc.ID, extracted, store.ExtractionMeta{
		Model:         s.extractor.Model(),
		PromptVersion: llm.PromptVersion,
		RawResponse:   raw,
		InputTokens:   usage.InputTokens,
		OutputTokens:  usage.OutputTokens,
	}); err != nil {
		return llm.Transient(fmt.Errorf("persist aggregate: %w", err))
	}

	moved, err := s.docs.UpdateStatus(ctx, doc.ID, store.StatusExtracting, store.StatusExtracted)
	if err != nil {
		return llm.Transient(err)
	}
	if !moved {
		s.log.Warn("document left extracting while being processed", "document_id", doc.ID)
		return nil
	}
	s.log.Info("document extracted", "document_id", doc.ID,
		"matricula", extracted.Numero, "atos", len(extracted.Atos),
		"input_tokens", usage.InputTokens, "output_tokens", usage.OutputTokens)
	return nil
}

// retryBackoff grows 30s, 1m, 2m, ... capped at 10m. attempts is the count
// already consumed (>= 1 when called).
func retryBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := 30 * time.Second << (attempts - 1)
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}
