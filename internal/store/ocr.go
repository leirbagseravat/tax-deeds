package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OCRResult is a row of ocr_results. Go only ever reads this table; the OCR
// service writes it (see docs/ocr-contract.md).
type OCRResult struct {
	PageNumber int
	Text       string
	Confidence *float32
	Engine     *string
}

// OCR reads OCR output produced by the Python service.
type OCR struct {
	pool *pgxpool.Pool
}

func NewOCR(pool *pgxpool.Pool) *OCR {
	return &OCR{pool: pool}
}

// ResultsByDocument returns a document's OCR pages ordered by page number.
func (s *OCR) ResultsByDocument(ctx context.Context, documentID string) ([]OCRResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT page_number, text, confidence, engine
		FROM ocr_results WHERE document_id = $1::uuid ORDER BY page_number`,
		documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []OCRResult
	for rows.Next() {
		var r OCRResult
		if err := rows.Scan(&r.PageNumber, &r.Text, &r.Confidence, &r.Engine); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
