# Sub-plan 3 — OCR dispatch, shared-table contract & stub OCR service

**Feature:** the Go↔Python handoff, fully testable locally without the real Python service.

**Scope**
- Migration `00003_ocr_results.sql`: `ocr_results` (document_id FK, page_number, text, confidence, engine, UNIQUE(document_id, page_number)); creates the restricted `ocr_service` DB role + grants (INSERT ocr_results; UPDATE documents.status/error_message)
- `internal/clients/ocr/ocr.go` — `POST {OCR_SERVICE_URL}/v1/ocr-jobs`, 3 retries with backoff, timeout from config; sets `ocr_dispatched_at`
- `internal/store/ocr.go` (read-only for Go), `internal/handlers/ocr.go` — `GET /documents/{id}/ocr` (pages concatenated in order with page markers)
- `cmd/ocr-stub/main.go` — dev-only fake Python service; compose gains this service
- `docs/ocr-contract.md` — the written, versioned spec for the Python team

**Contract** (`POST /v1/ocr-jobs` → `202`):
```json
{"document_id": "uuid", "pages": [{"page_number": 1, "gcs_bucket": "matriculas", "gcs_object": "documents/<id>/pages/0001.png"}]}
```
Python's obligations: **upsert** one `ocr_results` row per page (`ON CONFLICT DO UPDATE` — duplicate dispatches are expected), then `UPDATE documents SET status='ocr_done' WHERE id=$1 AND status='processing'` (or `failed` + `error_message`). Connects with the restricted `ocr_service` role.

**Design notes:** the stub is executable documentation of the contract — accepts the POST, sleeps 2s, upserts realistic fixture matrícula text (embedded `testdata/ocr_fixture.txt`), flips status with the guarded update; `FAIL_RATE` env exercises the failure path. Dispatch failure after retries → `failed`, `failed_stage='ocr_dispatch'`. Janitor (from sub-plan 5) uses `ocr_dispatched_at` to decide re-ingest vs re-dispatch.

**Acceptance criteria**
- Upload → poll `GET /documents/{id}` until `ocr_done` → `GET /documents/{id}/ocr` returns fixture text
- Stub failure mode produces `failed` + `error_message`
- Duplicate dispatch of the same document leaves consistent data (upsert proven by test)

---

