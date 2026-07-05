-- +goose Up
CREATE TABLE ocr_results (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    page_number int NOT NULL,
    text        text NOT NULL,
    confidence  real,
    engine      text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (document_id, page_number)
);

-- The Python OCR service connects with this role. The grants below are the
-- entire write surface it gets: upsert OCR rows and flip document status.
-- Production must override the password (ALTER ROLE ... PASSWORD).
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'ocr_service') THEN
        CREATE ROLE ocr_service LOGIN PASSWORD 'ocr_service';
    END IF;
END
$$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA public TO ocr_service;
GRANT SELECT, INSERT, UPDATE ON ocr_results TO ocr_service;
GRANT SELECT (id, status), UPDATE (status, failed_stage, error_message, updated_at)
    ON documents TO ocr_service;

-- +goose Down
REVOKE ALL ON documents FROM ocr_service;
REVOKE ALL ON ocr_results FROM ocr_service;
REVOKE ALL ON SCHEMA public FROM ocr_service;
DROP TABLE ocr_results;
DROP ROLE IF EXISTS ocr_service;
