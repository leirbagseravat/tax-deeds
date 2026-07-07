package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pgStore issues the three contract SQL statements (docs/ocr-contract.md)
// under the restricted ocr_service role — its grants are the entire allowed
// write surface.
type pgStore struct {
	pool *pgxpool.Pool
}

// UpsertPage writes one page's text; duplicate dispatches are expected, so
// conflicts overwrite.
func (s *pgStore) UpsertPage(ctx context.Context, documentID string, page int, text string, confidence *float64, engine string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ocr_results (document_id, page_number, text, confidence, engine)
		VALUES ($1::uuid, $2, $3, $4, $5)
		ON CONFLICT (document_id, page_number)
		DO UPDATE SET text = EXCLUDED.text, confidence = EXCLUDED.confidence, engine = EXCLUDED.engine`,
		documentID, page, text, confidence, engine)
	return err
}

// MarkOCRDone flips processing → ocr_done; the guard keeps races with the
// orchestrator's state machine harmless. Reports whether a row flipped.
func (s *pgStore) MarkOCRDone(ctx context.Context, documentID string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE documents SET status = 'ocr_done', updated_at = now()
		WHERE id = $1::uuid AND status = 'processing'`,
		documentID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// MarkFailed records an unrecoverable OCR failure.
func (s *pgStore) MarkFailed(ctx context.Context, documentID, message string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE documents
		SET status = 'failed', failed_stage = 'ocr', error_message = $2, updated_at = now()
		WHERE id = $1::uuid AND status = 'processing'`,
		documentID, message)
	return err
}
