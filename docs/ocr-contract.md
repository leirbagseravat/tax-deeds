# OCR Service Contract (v1)

Contract between the Go orchestrator (this repo) and the OCR service.
The worker at `cmd/ocr` implements it with pluggable engines
(`OCR_ENGINE=stub|glm|glm-maas`; the real engines run
[GLM-OCR](https://github.com/zai-org/GLM-OCR) via an OpenAI-compatible
endpoint or Zhipu's hosted API). Any replacement service — in any language —
must honor this document; when in doubt, do what `cmd/ocr` does.

## Transport: HTTP job dispatch

The orchestrator POSTs one job per document:

```
POST {OCR_SERVICE_URL}/v1/ocr-jobs
Content-Type: application/json

{
  "document_id": "3f6c1a9e-...",
  "pages": [
    {"page_number": 1, "gcs_bucket": "matriculas", "gcs_object": "documents/<id>/pages/0001.png"},
    {"page_number": 2, "gcs_bucket": "matriculas", "gcs_object": "documents/<id>/pages/0002.png"}
  ]
}
```

- Respond `202` immediately and process asynchronously. Any non-2xx makes the
  orchestrator retry (3 attempts, exponential backoff), then mark the document
  `failed` with `failed_stage = 'ocr_dispatch'`.
- **Duplicate jobs for the same document are expected** (retries, crash
  recovery). Processing must be idempotent — see the upsert below.
- Page images are PNGs in the shared GCS bucket; both services use the same
  bucket credentials (`STORAGE_EMULATOR_HOST` points at fake-gcs-server in dev).

## Results: shared Postgres tables

Connect with the dedicated `ocr_service` role (created by migration
`00003_ocr_results.sql`; override its password in production). Its grants are
the entire allowed write surface:

1. **Upsert one row per page** into `ocr_results`:

```sql
INSERT INTO ocr_results (document_id, page_number, text, confidence, engine)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (document_id, page_number)
DO UPDATE SET text = EXCLUDED.text, confidence = EXCLUDED.confidence, engine = EXCLUDED.engine;
```

- `text`: recognized text of that page (UTF-8, preserve line breaks).
- `confidence`: optional 0..1 average confidence.
- `engine`: optional engine name/version (e.g. `tesseract-5.3`).

2. **After all pages are written**, flip the document status with a *guarded*
   update (the `WHERE status` clause is mandatory — it prevents races with the
   orchestrator's state machine):

```sql
UPDATE documents SET status = 'ocr_done', updated_at = now()
WHERE id = $1 AND status = 'processing';
```

3. **On unrecoverable failure**, instead:

```sql
UPDATE documents
SET status = 'failed', failed_stage = 'ocr', error_message = $2, updated_at = now()
WHERE id = $1 AND status = 'processing';
```

## Timeouts

If the orchestrator sees a document stuck in `processing` longer than
`STUCK_TIMEOUT` (default 10m) after dispatch, its janitor re-dispatches the
job. Write results within that window or in an idempotent way. (`cmd/ocr`
bounds each job with `OCR_JOB_TIMEOUT`, default 8m — deliberately inside that
window — and treats expiry as an unrecoverable failure.)

## Failure semantics in cmd/ocr

- A page that still fails after retries (image download or model call) marks
  the document `failed` — terminal by design, so deterministic model errors
  don't re-dispatch forever. Pages already upserted stay (harmless: upserts
  are idempotent).
- A failed **result write** flips nothing: the document stays `processing`
  and the janitor re-dispatches the job.

## Versioning

Breaking changes to this contract bump the URL version (`/v2/ocr-jobs`) and
are coordinated with the Python team via this document.
