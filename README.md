# tax-deeds

**Distressed real estate is a large market in Brazil.** Around half of the adult
population carries overdue debt, much of it tied to property bought with
financing. When a borrower defaults, the lender repossesses the property and
needs to sell it fast — so repossessed units routinely list well below market
price. The catch for a buyer is due diligence: every lien, prior owner, and
registered act lives in the property's **matrícula**, a dense registry document
that is slow and error-prone to read by hand.

`tax-deeds` turns that document into structured, queryable data. You upload a
matrícula PDF; the service runs OCR over its pages, extracts the registry
entities — owners, acts (registros/averbações), and liens (ônus) — with an LLM,
and exposes a read/query API over the result, so a repossessed property can be
vetted in seconds instead of hours.

```
upload → ingest → OCR → LLM extraction → read API
```

## Architecture

Three binaries plus local infra:

| Component | Path | Port | Role |
|-----------|------|------|------|
| API / orchestrator | `cmd/api` | `:8080` | Upload + query endpoints; runs the extraction poller |
| OCR worker | `cmd/ocr` | `:9090` | Transcribes page images (runs inside docker compose) |
| Migrations | `cmd/migrate` | — | Applies goose SQL migrations |

Local infra (via `docker-compose.yml`): **Postgres 17** and
**`fake-gcs-server`** (a Google Cloud Storage emulator for uploaded files).

```mermaid
flowchart LR
    client([Client])
    subgraph api["cmd/api :8080"]
        ingest["ingest"]
        poller["extraction poller"]
    end
    worker["cmd/ocr :9090"]
    pg[("Postgres")]
    gcs[("GCS / fake-gcs")]
    engines["OCR engine<br/>stub · glm · glm-maas"]
    llm["LLM provider<br/>anthropic · ollama · stub"]

    client -->|"POST /documents (PDF)"| ingest
    client -->|"GET status · matricula"| api
    ingest -->|"PDF + page PNGs"| gcs
    ingest -->|"documents · pages"| pg
    ingest -->|"POST /v1/ocr-jobs"| worker
    worker -->|"page PNGs"| gcs
    worker --> engines
    worker -->|"ocr_results"| pg
    poller -->|"claim ocr_done"| pg
    poller --> llm
    poller -->|"matrícula aggregate"| pg
```

For the full picture — service-interaction, sequence, and state-machine
diagrams plus a stage-by-stage walkthrough of an analysis — see
**[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)**.

## Running locally

Prerequisites, an offline stub run (no API keys), configuration, and how to
wire up real OCR engines and LLM providers all live in one guide:
**[How to boot the app locally →](docs/RUNNING_LOCALLY.md)**.

The fastest path — offline, no keys, using the `stub` OCR engine and LLM
provider:

```bash
cp .env.example .env
make up                     # postgres + fake-gcs + ocr worker (stub engine)
make migrate                # apply DB migrations
make run LLM_PROVIDER=stub  # start the API on :8080
```

## API endpoints

| Method & path | Description |
|---------------|-------------|
| `GET /healthz` | Liveness check |
| `GET /readyz` | Readiness check |
| `POST /api/v1/documents` | Upload a matrícula PDF (multipart `file`) |
| `GET /api/v1/documents/{id}` | Document status |
| `GET /api/v1/documents/{id}/ocr` | OCR result for the document |
| `GET /api/v1/documents/{id}/matricula` | Full extracted matrícula aggregate |
| `GET /api/v1/matriculas/{id}/proprietarios` | Owners + cadeia dominial |
| `GET /api/v1/matriculas/{id}/atos?kind=averbacao` | Acts (filter by `kind`) |
| `GET /api/v1/matriculas/{id}/onus?status=ativo` | Liens (filter by `status`) |

## Project layout

- `cmd/` — entrypoints (`api`, `ocr`, `migrate`).
- `internal/` — private implementation: `handlers`, `routes`, `services`,
  `store`, `clients` (`gcs`, `ocr`, `llm`), `ocrengine`, `config`, `dto`.
- `pkg/` — reusable, domain-free utilities (`logger`, `middleware`, `response`).
- `docs/plans/` — the design docs the implementation followed.

## Next steps

### RAG-backed extraction

Today the extraction step (`internal/services/extraction/`) ships the full OCR
text plus a fixed system prompt to the LLM provider. That prompt (`prompt.go`,
`PromptVersion "v1"`) carries every rule, enum vocabulary, and example inline —
so the token count grows on every call, and it still can't capture every
cartório's quirks.

The natural improvement is to move that domain knowledge behind a **retrieval
step (RAG)**. Build a knowledge base of registry conventions — enum definitions,
cartório-specific layouts, legal terminology (registros vs. averbações, tipos de
ônus), and curated few-shot examples of hard cases — embedded into a vector
store. At extraction time, embed the document's OCR text, retrieve only the
handful of most relevant snippets, and inject *those* into the prompt instead of
shipping the entire rulebook every time.

```mermaid
flowchart LR
    ocr["OCR text for a document"]
    kb[("Knowledge base<br/>registry conventions ·<br/>cartório templates ·<br/>few-shot examples")]

    subgraph retrieval["Retrieval (new)"]
        embed["Embed OCR text"]
        search["Similarity search<br/>top-k relevant snippets"]
    end

    prompt["Prompt = OCR text +<br/>retrieved context only"]
    llm["LLM provider<br/>internal/clients/llm"]
    out["ExtractedMatricula JSON"]

    ocr --> embed --> search
    kb --> search
    search --> prompt
    ocr --> prompt
    prompt --> llm --> out
    llm -.->|"reviewed extractions<br/>feed back as examples"| kb
```

Why it helps:

- **More precise.** Extraction is grounded in real, matching examples and the
  exact conventions of the cartório that issued the document, rather than the
  model's priors.
- **Fewer tokens.** The static prompt shrinks to a small, retrieved context —
  you pay for the few examples that matter, not all of them — lowering cost and
  latency per document.
- **Improves over time.** The append-only `extractions` table already stores
  every raw LLM response; reviewed extractions can feed curated examples back
  into the knowledge base.

Where it slots in: a retriever sits between the OCR text and the `Extractor`
(`internal/clients/llm/`), so the strategy interface and the rest of the
pipeline stay unchanged.
