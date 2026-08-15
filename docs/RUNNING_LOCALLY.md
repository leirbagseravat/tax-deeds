# How to boot the app locally

Everything you need to get `tax-deeds` running on your machine — from an
offline stub run (no API keys) to real OCR and LLM providers. For what the
service does and how it fits together, see the [README](../README.md) and
[`ARCHITECTURE.md`](ARCHITECTURE.md).

## Prerequisites

- Go 1.26
- Docker + Docker Compose
- `python3` (only used by `scripts/demo.sh` to pretty-print JSON)

## Quick start (offline, no API keys)

The fastest end-to-end run uses the `stub` OCR engine and `stub` LLM provider —
both return fixtures, so no external services or keys are needed.

```bash
cp .env.example .env

make up                     # postgres + fake-gcs + ocr worker (stub engine)
make migrate                # apply DB migrations

make run LLM_PROVIDER=stub  # start the API on :8080
```

Then, in another shell, drive the full pipeline against the bundled sample PDF:

```bash
scripts/demo.sh             # uploads testdata/matricula.pdf, polls, walks the read API
```

The demo uploads a document, waits until its status reaches `extracted`, and
prints the matrícula aggregate plus owners, acts, and liens.

## Configuration

Copy `.env.example` to `.env` and adjust as needed. The Makefile auto-loads
`.env`, and those values **override** prefix-style shell variables
(`LLM_PROVIDER=stub make run` loses to `.env`); to override a `.env` value for
one run, pass it as a make argument: `make run LLM_PROVIDER=stub`. Key
variables:

| Variable | Default | Read by | Purpose |
|----------|---------|---------|---------|
| `PORT` | `8080` | `cmd/api` | HTTP listen port |
| `LOG_LEVEL` | `info` | `cmd/api` | Log verbosity |
| `DATABASE_URL` | *(required)* | api, ocr, migrate | Postgres connection string |
| `GCS_BUCKET` | `matriculas` | `cmd/api` | Object-storage bucket |
| `STORAGE_EMULATOR_HOST` | `localhost:4443` | api, ocr | Point GCS client at fake-gcs (unset in prod) |
| `OCR_SERVICE_URL` | `http://localhost:9090` | `cmd/api` | Where the API dispatches OCR jobs |
| `MAX_UPLOAD_MB` | `25` | `cmd/api` | Upload size limit |
| `MAX_PAGES` | `100` | `cmd/api` | Max pages per document |
| `POLL_INTERVAL` | `5s` | `cmd/api` | Extraction poller interval |
| `STUCK_TIMEOUT` | `10m` | `cmd/api` | Reclaim documents stuck mid-extraction |
| `MAX_EXTRACTION_ATTEMPTS` | `3` | `cmd/api` | Retries before a document is marked failed |

See `.env.example` for the full annotated list.

## Choosing an OCR engine

Set `OCR_ENGINE` (read by `cmd/ocr`). Because the worker runs inside docker
compose, pass the engine when (re)building that service.

- **`stub`** (default) — offline fixture. No setup.
- **`glm-maas`** — GLM-OCR hosted on Zhipu's `layout_parsing` API. Fastest real
  engine: only needs a key.
  - Required: `ZHIPU_API_KEY` (from <https://open.bigmodel.cn>).
  - Optional: `ZHIPU_BASE_URL` (defaults to `https://open.bigmodel.cn/api/paas/v4`).
  - Note: each page image is capped at **10 MB** as a base64 data URI.
  ```bash
  OCR_ENGINE=glm-maas ZHIPU_API_KEY=your-key \
    docker compose up -d --build ocr
  ```
- **`glm`** — any OpenAI-compatible endpoint serving GLM-OCR (Ollama, vLLM,
  SGLang). You host the model yourself.
  - Required: `GLM_BASE_URL`.
  - Optional: `GLM_MODEL` (default `glm-ocr`), `GLM_API_KEY`.

## Choosing an LLM provider

Set `LLM_PROVIDER` / `LLM_MODEL` (read by `cmd/api`). Three providers are
wired in (`internal/clients/llm/extractor.go`):

- **`anthropic`** (default) — requires `ANTHROPIC_API_KEY`;
  `LLM_MODEL=claude-sonnet-5`.
- **`ollama`** — any model served by a local [Ollama](https://ollama.com)
  (no API key). `LLM_MODEL` must match a pulled model exactly as shown by
  `ollama list`, tag included. Optional: `LLM_BASE_URL` (default
  `http://localhost:11434`).
  ```bash
  ollama pull qwen3:8b
  make run LLM_PROVIDER=ollama LLM_MODEL=qwen3:8b
  ```
  Extraction demands strict JSON from Portuguese registry text, so prefer a
  capable instruct model (e.g. `qwen3:8b`, or `qwen3:14b` for better quality).
- **`stub`** — returns fixture extraction output, no key.

Setting `LLM_PROVIDER` to anything else fails at startup. Adding a new provider
means implementing the `Extractor` interface and registering a `case` in the
factory.

## Make targets

| Target | Action |
|--------|--------|
| `make run` | Run the API (`go run ./cmd/api`) |
| `make build` | Build all three binaries into `bin/` |
| `make test` | `go test ./...` |
| `make vet` | `go vet ./...` |
| `make fmt` | `gofmt -w .` |
| `make up` | `docker compose up -d` (postgres + fake-gcs + ocr) |
| `make down` | `docker compose down` |
| `make migrate` | Apply migrations (`goose up`) |
| `make migrate-down` | Roll back the last migration |
| `make migrate-status` | Show migration status |
| `make ocr` | Run the OCR worker on the host |
