# Matrícula Analysis Orchestrator — Master Plan + Feature Sub-Plans

## Context

Build a product that analyses Brazilian real estate registry documents (matrícula de imóvel). This repo (`tax-deeds`, empty Go 1.26 module) is the **Go orchestrator**: it accepts a matrícula PDF upload, converts pages to images, stores them in Google Cloud Storage, persists metadata to Postgres, triggers a separate Python OCR service over HTTP (out of scope for this repo, contract defined here), and — once OCR text lands in Postgres — calls an LLM to extract structured registry entities (proprietários, registros, averbações, ônus) and exposes them via a query API.

User-confirmed decisions:
- **No message queue** — Go fires an async HTTP POST to the Python OCR service and returns `202`.
- **GCS** for object storage (fake-gcs-server locally); **Go converts PDF→images** (poppler `pdftoppm` via `os/exec`).
- **Python writes OCR results + status directly to the shared Postgres** (per user's diagram); Go reads status from DB.
- **LLM via LangChain (`tmc/langchaingo`) behind a strategy-pattern interface** so the library/provider is swappable later.

Layout must follow `.ai/GO_PROJECT_STRUCTURE.md`: `cmd/<entrypoint>/`, `internal/{handlers,routes,services,store,clients,dto,config}`, `pkg/{logger,middleware,response}`; dependency direction `handlers → services → store`, never reverse.

**Sub-plan materialization:** the first implementation step writes each sub-plan below into the repo as `docs/plans/<nn>-<feature>.md` (this master context → `docs/plans/00-overview.md`). Sub-plans are iterated and implemented independently, in order; the repo stays shippable after every one.

## Shared design (referenced by all sub-plans)

### Libraries

| Concern | Choice |
|---|---|
| Routing | stdlib `net/http` ServeMux (Go 1.22+ method/wildcard patterns) |
| Postgres | `jackc/pgx/v5` + `pgxpool` |
| Migrations | `pressly/goose/v3`, embedded SQL, run via `cmd/migrate` |
| GCS | `cloud.google.com/go/storage` (honors `STORAGE_EMULATOR_HOST`) |
| PDF→PNG | `pdftoppm -png -r 200` behind a `Converter` interface, exec timeout + page cap |
| LLM | `tmc/langchaingo` as first strategy behind an `Extractor` interface |
| Logging/config | `log/slog` in `pkg/logger`; env vars parsed in `internal/config` |

### Config (env)

`PORT`, `DATABASE_URL`, `GCS_BUCKET`, `STORAGE_EMULATOR_HOST` (dev), `GOOGLE_APPLICATION_CREDENTIALS` (prod), `OCR_SERVICE_URL`, `OCR_DISPATCH_TIMEOUT`, `MAX_UPLOAD_MB`, `MAX_PAGES`, `LLM_PROVIDER`, `LLM_MODEL`, `ANTHROPIC_API_KEY`, `POLL_INTERVAL`, `STUCK_TIMEOUT`, `MAX_EXTRACTION_ATTEMPTS`, `LOG_LEVEL`.

### Status state machine (`documents.status`)

```
uploaded → processing → ocr_done → extracting → extracted
     └──────────┴───────────┴───────────┴──→ failed (failed_stage, error_message)
                                ▲───────────┘ transient LLM failure: extracting → ocr_done
                                              (extraction_attempts++, next_extraction_at backoff)
```

- `ocr_done` / OCR-failure written **by Python**, guarded `WHERE status='processing'`.
- Every Go transition is a guarded update (`... WHERE id=$1 AND status=$2`) so crashed/duplicate workers can't corrupt state. Partial index on `status` for the poller.

### Failure modes & recovery (cross-cutting)

- **Background work never inherits the HTTP request context** — `ingest.Process` runs with `context.WithoutCancel` (else processing dies when the upload request closes). Graceful shutdown waits on a WaitGroup with a deadline; anything unfinished is recovered by the janitor.
- **`ocr_dispatched_at timestamptz` on `documents`** disambiguates a stuck `processing` row: NULL → Go died mid-ingest, janitor re-runs ingest (idempotent: same GCS object names, re-dispatch allowed); set long ago → Python lost the job, janitor re-dispatches up to N times then `failed`.
- **Idempotency is part of the OCR contract**: Python upserts `ocr_results` (`ON CONFLICT (document_id, page_number) DO UPDATE`) and guards its status update, so duplicate dispatches are harmless.
- **LLM retries**: transient (rate-limit/5xx/timeout) → guarded `extracting → ocr_done`, `extraction_attempts++`, `next_extraction_at = now() + backoff`; poller claims only rows with `next_extraction_at <= now()`; after `MAX_EXTRACTION_ATTEMPTS` → `failed`. Terminal (4xx, schema-invalid output after 1 in-call retry) → `failed`.
- **Abuse guards**: upload size limit, `%PDF-` content sniff, `MAX_PAGES` cap after conversion, `pdftoppm` wrapped in a context timeout.
- **Shared-DB hardening**: Python gets a dedicated DB role granted only INSERT on `ocr_results` and UPDATE of `documents.status/error_message` — the contract is enforced by grants, not just documentation (role + grants created in the ocr_results migration).

### API surface (final, built up across sub-plans)

- `GET /healthz`, `GET /readyz`
- `POST /api/v1/documents` (multipart PDF) → `202 {id, status}`
- `GET /api/v1/documents/{id}` — status DTO
- `GET /api/v1/documents/{id}/ocr` — raw OCR text
- `GET /api/v1/documents/{id}/matricula` — full nested aggregate
- `GET /api/v1/matriculas/{id}/proprietarios` | `/atos?kind=` | `/onus?status=`
- Error envelope `{"error":{"code","message"}}` via `pkg/response`; `409 not_ready` when a document exists but isn't extracted yet

---

## Execution order

1 → 2 → 3 → 4 → 5 → 6 (→ 7 as desired). Verify each sub-plan's acceptance criteria before starting the next. First step overall: materialize these sub-plans into `docs/plans/` in the repo.
