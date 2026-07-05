# Sub-plan 2 — Document ingestion (upload → PDF-to-images → GCS → metadata)

**Feature:** clients upload a matrícula PDF; pages become PNGs in GCS; metadata tracked in Postgres.

**Scope**
- Migration `00002_documents_pages.sql`:
  - `documents` (id uuid PK, filename, status + CHECK on enum, failed_stage, error_message, page_count, gcs_bucket, gcs_pdf_object, ocr_dispatched_at timestamptz NULL, created_at/updated_at; partial index on status)
  - `pages` (id, document_id FK CASCADE, page_number, gcs_object, UNIQUE(document_id, page_number))
- `internal/store/documents.go` — Create, GetByID, guarded UpdateStatus, AddPages
- `internal/clients/gcs/gcs.go` — thin `Upload(ctx, object, io.Reader)` wrapper
- `internal/services/pdfconvert/` — `Converter` interface; real impl shells to `pdftoppm` (temp dir → ordered page PNG paths), context timeout, `MAX_PAGES` enforcement; integration test behind `//go:build poppler`
- `internal/services/ingest/ingest.go` — orchestration + background `Process(id)` on `context.WithoutCancel`, WaitGroup for shutdown
- `internal/handlers/documents.go`, `internal/dto/documents.go` — upload (202) + status endpoints

**Design notes:** handler stays thin — stream multipart to temp file, validate (`%PDF-` sniff, size limit), upload raw PDF, insert row (`uploaded`), spawn background Process, return 202. GCS layout `documents/{id}/original.pdf`, `documents/{id}/pages/0001.png` (zero-padded ⇒ lexical order). Process is idempotent (deterministic object names) so the janitor can re-run it.

**Acceptance criteria**
- `curl -F file=@testdata/matricula.pdf :8080/api/v1/documents` → 202 with id
- Page objects visible via fake-gcs REST (`/storage/v1/b/<bucket>/o`); rows in psql
- `GET /documents/{id}` shows `processing` + correct `page_count` (parks there until sub-plan 3)
- Non-PDF, oversize, and over-page-cap uploads rejected/failed cleanly; store tests pass against dockerized Postgres

---

