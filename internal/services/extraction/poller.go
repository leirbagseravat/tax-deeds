package extraction

import (
	"context"
	"log/slog"
	"time"

	"tax-deeds/internal/services/ingest"
	"tax-deeds/internal/store"
)

// Poller drives extraction: it claims ocr_done documents for the LLM and, as
// janitor, recovers documents that stopped making progress (crashed worker,
// lost OCR job). Safe to run in multiple replicas thanks to SKIP LOCKED.
type Poller struct {
	Log          *slog.Logger
	Docs         *store.Documents
	Ingest       *ingest.Service
	Extraction   *Service
	Interval     time.Duration
	StuckTimeout time.Duration
	MaxAttempts  int
}

// Run blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()
	p.Log.Info("extraction poller running", "interval", p.Interval.String(), "stuck_timeout", p.StuckTimeout.String())
	for {
		select {
		case <-ctx.Done():
			p.Log.Info("extraction poller stopped")
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Poller) tick(ctx context.Context) {
	// Drain everything currently eligible, one claim at a time.
	for {
		doc, ok, err := p.Docs.ClaimForExtraction(ctx, p.MaxAttempts)
		if err != nil {
			p.Log.Error("claim for extraction", "error", err)
			break
		}
		if !ok {
			break
		}
		p.Extraction.ExtractDocument(ctx, doc)
	}

	if n, err := p.Docs.FailExhausted(ctx, p.MaxAttempts); err != nil {
		p.Log.Error("fail exhausted", "error", err)
	} else if n > 0 {
		p.Log.Warn("documents failed after exhausting extraction attempts", "count", n)
	}

	p.janitor(ctx)
}

// janitor recovers stuck documents. "processing" without a dispatch stamp
// means our ingest died (re-run it: it is idempotent); with a stale stamp the
// OCR job got lost (re-dispatch). "extracting" means an extraction worker
// died mid-flight (make it claimable again).
func (p *Poller) janitor(ctx context.Context) {
	stuck, err := p.Docs.StuckDocuments(ctx, p.StuckTimeout)
	if err != nil {
		p.Log.Error("list stuck documents", "error", err)
		return
	}
	for _, doc := range stuck {
		switch doc.Status {
		case store.StatusProcessing:
			if doc.OCRDispatchedAt == nil {
				p.Log.Warn("re-running ingest for stuck document", "document_id", doc.ID)
				p.Ingest.Process(ctx, doc.ID)
			} else {
				p.Log.Warn("re-dispatching lost OCR job", "document_id", doc.ID)
				p.Ingest.Redispatch(ctx, doc.ID)
			}
		case store.StatusExtracting:
			p.Log.Warn("requeueing stuck extraction", "document_id", doc.ID)
			if err := p.Docs.ReturnForRetry(ctx, doc.ID, 0); err != nil {
				p.Log.Error("requeue stuck extraction", "document_id", doc.ID, "error", err)
			}
		}
	}
}
