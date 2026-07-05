# Sub-plan 5 — LLM extraction pipeline (LangChain strategy + poller)

**Feature:** OCR text becomes structured registry data automatically.

**Scope**
- `internal/clients/llm/extractor.go` — **the strategy interface (user requirement)**:
  `type Extractor interface { ExtractMatricula(ctx context.Context, ocrText string) (dto.ExtractedMatricula, Usage, error) }`
- `internal/clients/llm/langchain.go` — first strategy on `tmc/langchaingo` (Anthropic provider): force JSON via tool/function-calling or JSON mode where the provider supports it, else prompt-enforced JSON; always strict unmarshal (`DisallowUnknownFields`) + one in-call repair retry
- `internal/clients/llm/schema.go`, `prompt.go` (versioned `PromptV1`) — implementation-agnostic, reused by any future strategy
- `internal/services/extraction/extraction.go` — fetch OCR pages in order → Extractor → validate/normalize → persist aggregate in one tx → status `extracted`
- `internal/services/extraction/poller.go` — ticker (`POLL_INTERVAL`) claims one eligible doc (`status='ocr_done' AND (next_extraction_at IS NULL OR next_extraction_at <= now())`) via `FOR UPDATE SKIP LOCKED` → `extracting`; **janitor** re-queues rows stuck in `processing`/`extracting` past `STUCK_TIMEOUT` using `ocr_dispatched_at` to pick re-ingest vs re-dispatch vs back-to-`ocr_done`

**Design notes:**
- Extraction service depends only on the `Extractor` interface, constructed in `cmd/api` from config (`LLM_PROVIDER`/`LLM_MODEL`) — a future direct-SDK strategy is one new file + wiring.
- Poller (not check-on-read, not a Python callback): restart-safe, no queue, multi-replica-safe via SKIP LOCKED. Goroutine in `cmd/api`; isolated for a later `cmd/worker` split.
- Retry semantics per the shared failure-modes section (transient → back to `ocr_done` with backoff; terminal → `failed`; cap by `MAX_EXTRACTION_ATTEMPTS`). Context-window overflow on very long matrículas fails with a clear `error_message` (chunking is out of scope v1).
- Tests: fake `Extractor` returning golden JSON; real-API test behind `//go:build llm_integration` asserting key fixture fields (matrícula number, ≥1 proprietário, penhora present).

**Acceptance criteria**
- Upload → status reaches `extracted`; `atos`/`onus`/`proprietarios` correct in psql for the fixture
- Kill the API mid-extraction → janitor returns the stuck row and it completes on the next cycle
- Forced transient failure retries with backoff then succeeds; forced terminal failure → `failed` with message

---

