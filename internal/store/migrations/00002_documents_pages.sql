-- +goose Up
CREATE TABLE documents (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    filename          text NOT NULL,
    status            text NOT NULL DEFAULT 'uploaded'
                      CHECK (status IN ('uploaded', 'processing', 'ocr_done', 'extracting', 'extracted', 'failed')),
    failed_stage      text,
    error_message     text,
    page_count        int,
    gcs_bucket        text NOT NULL,
    gcs_pdf_object    text NOT NULL,
    ocr_dispatched_at timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- The poller and janitor scan for non-terminal statuses only.
CREATE INDEX documents_status_idx ON documents (status, updated_at)
    WHERE status NOT IN ('extracted', 'failed');

CREATE TABLE pages (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    page_number int NOT NULL,
    gcs_object  text NOT NULL,
    UNIQUE (document_id, page_number)
);

-- +goose Down
DROP TABLE pages;
DROP TABLE documents;
