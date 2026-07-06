package extraction

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"mortgage/internal/clients/llm"
	"mortgage/internal/dto"
	"mortgage/internal/store"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedOCRDoneDocument creates a document with OCR results, ready for extraction.
// Pre-existing eligible documents (from earlier manual runs against the same dev
// DB) are pushed out of the claim window so ClaimForExtraction picks ours.
func seedOCRDoneDocument(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		UPDATE documents SET next_extraction_at = now() + interval '1 hour'
		WHERE status = 'ocr_done' AND (next_extraction_at IS NULL OR next_extraction_at <= now())`); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if _, err := store.NewDocuments(pool).Create(ctx, id, "seed.pdf", "b", "documents/x/original.pdf"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM documents WHERE id = $1::uuid`, id) })
	if _, err := pool.Exec(ctx, `
		INSERT INTO ocr_results (document_id, page_number, text) VALUES ($1::uuid, 1, 'MATRICULA 45.678 ...')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE documents SET status = 'ocr_done' WHERE id = $1::uuid`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

type failingExtractor struct{ err error }

func (f failingExtractor) Model() string { return "failing" }
func (f failingExtractor) ExtractMatricula(ctx context.Context, _ string) (dto.ExtractedMatricula, []byte, llm.Usage, error) {
	return dto.ExtractedMatricula{}, nil, llm.Usage{}, f.err
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestService(t *testing.T, pool *pgxpool.Pool, ex llm.Extractor) (*Service, *store.Documents) {
	t.Helper()
	docs := store.NewDocuments(pool)
	return NewService(discardLog(), docs, store.NewOCR(pool), store.NewMatriculas(pool), ex), docs
}

func claim(t *testing.T, docs *store.Documents) store.Document {
	t.Helper()
	doc, ok, err := docs.ClaimForExtraction(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("nothing claimable")
	}
	return doc
}

func TestExtractDocumentHappyPath(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	id := seedOCRDoneDocument(t, pool)

	stubEx, err := llm.New("stub", "")
	if err != nil {
		t.Fatal(err)
	}
	svc, docs := newTestService(t, pool, stubEx)

	doc := claim(t, docs)
	if doc.ID != id {
		t.Fatalf("claimed %s, want %s", doc.ID, id)
	}
	if doc.Status != store.StatusExtracting || doc.ExtractionAttempts != 1 {
		t.Fatalf("claim state: %s/%d", doc.Status, doc.ExtractionAttempts)
	}

	svc.ExtractDocument(ctx, doc)

	got, err := docs.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusExtracted {
		t.Fatalf("status = %s, want extracted (error: %v)", got.Status, got.ErrorMessage)
	}
	mat, err := store.NewMatriculas(pool).GetByDocumentID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if mat.Numero != "45.678" || len(mat.Atos) != 6 {
		t.Errorf("aggregate: numero=%s atos=%d", mat.Numero, len(mat.Atos))
	}
	// Normalization ran: CPF digits only + checksum flag.
	if p := mat.Proprietarios[0]; p.CpfCnpj != "11122233396" || p.CpfCnpjValido == nil || !*p.CpfCnpjValido {
		t.Errorf("proprietario not normalized: %+v", p)
	}
	// Onus lifecycle: hipoteca cancelada, penhora ativa.
	statuses := map[string]string{}
	for _, o := range mat.Onus {
		statuses[o.Tipo] = o.Status
	}
	if statuses["hipoteca"] != "cancelado" || statuses["penhora"] != "ativo" {
		t.Errorf("onus statuses: %v", statuses)
	}
}

func TestExtractDocumentTransientFailureRequeues(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	id := seedOCRDoneDocument(t, pool)

	svc, docs := newTestService(t, pool, failingExtractor{err: llm.Transient(errors.New("rate limited"))})
	svc.ExtractDocument(ctx, claim(t, docs))

	got, err := docs.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusOCRDone {
		t.Fatalf("status = %s, want ocr_done", got.Status)
	}
	if got.NextExtractionAt == nil {
		t.Fatal("next_extraction_at not set")
	}
	if got.ExtractionAttempts != 1 {
		t.Fatalf("attempts = %d, want 1", got.ExtractionAttempts)
	}

	// Not yet eligible again (backoff in the future).
	if _, ok, err := docs.ClaimForExtraction(ctx, 3); err != nil || ok {
		t.Fatalf("claim during backoff: ok=%v err=%v", ok, err)
	}
}

func TestExtractDocumentTerminalFailureFails(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	id := seedOCRDoneDocument(t, pool)

	svc, docs := newTestService(t, pool, failingExtractor{err: errors.New("invalid_request: prompt too long")})
	svc.ExtractDocument(ctx, claim(t, docs))

	got, err := docs.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	if got.FailedStage == nil || *got.FailedStage != "extraction" {
		t.Fatalf("failed_stage = %v", got.FailedStage)
	}
}

func TestStuckDocumentsFindsStaleExtracting(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	id := seedOCRDoneDocument(t, pool)
	docs := store.NewDocuments(pool)

	if _, err := pool.Exec(ctx, `
		UPDATE documents SET status = 'extracting', updated_at = now() - interval '1 hour'
		WHERE id = $1::uuid`, id); err != nil {
		t.Fatal(err)
	}

	stuck, err := docs.StuckDocuments(ctx, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range stuck {
		if d.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("seeded stuck document not returned (got %d docs)", len(stuck))
	}

	// A fresh extracting row must not be reported.
	if _, err := pool.Exec(ctx, `UPDATE documents SET updated_at = now() WHERE id = $1::uuid`, id); err != nil {
		t.Fatal(err)
	}
	stuck, err = docs.StuckDocuments(ctx, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range stuck {
		if d.ID == id {
			t.Fatal("fresh document reported as stuck")
		}
	}
}

func TestFailExhausted(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	id := seedOCRDoneDocument(t, pool)
	docs := store.NewDocuments(pool)

	if _, err := pool.Exec(ctx, `UPDATE documents SET extraction_attempts = 3 WHERE id = $1::uuid`, id); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := docs.ClaimForExtraction(ctx, 3); err != nil || ok {
		t.Fatalf("exhausted doc claimed: ok=%v err=%v", ok, err)
	}
	if _, err := docs.FailExhausted(ctx, 3); err != nil {
		t.Fatal(err)
	}
	got, err := docs.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
}
