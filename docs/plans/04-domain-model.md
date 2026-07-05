# Sub-plan 4 — Matrícula domain model & persistence

**Feature:** the Brazilian registry domain schema and its store layer, independent of any LLM.

**Scope**
- Migration `00004_matricula_domain.sql`:
  - `extractions` — provenance: model, prompt_version, raw_response JSONB, input/output tokens, created_at (enables re-extraction & audit)
  - `matriculas` — numero, cartório (nome/comarca/UF), data_abertura, imóvel (descricao, endereco, area_m2, tipo urbano/rural/...); UNIQUE document_id; FK extraction_id
  - `atos` — unified registros (R-n) + averbações (Av-n): numero as printed, kind CHECK ('registro','averbacao'), tipo (compra_venda, doacao, partilha, hipoteca, penhora, alienacao_fiduciaria, cancelamento, construcao, outro…), data_ato, valor numeric, **moeda text** (older matrículas carry Cr$, CZ$, NCz$ — never assume BRL), descricao, seq (timeline order)
  - `ato_partes` — papel (transmitente/adquirente/credor/devedor/interessado), nome, cpf_cnpj (digits-only **text**, never numeric — leading zeros), tipo_pessoa
  - `proprietarios` — current-owner snapshot (LLM-derived): nome, cpf_cnpj, fracao, ato_aquisicao_id
  - `onus` — tipo (hipoteca, penhora, alienacao_fiduciaria, usufruto, servidao, indisponibilidade, outro), status ativo/cancelado, ato_constituicao_id, ato_cancelamento_id, credor, valor, moeda
  - `documents` gains `extraction_attempts int DEFAULT 0`, `next_extraction_at timestamptz NULL`
- `internal/dto/matricula.go` — `ExtractedMatricula` + nested types (single source of truth: reused by the LLM JSON schema and the API DTOs)
- `internal/store/matriculas.go` — transactional insert of the full aggregate (extractions → matriculas → atos → ato_partes → proprietarios → onus); aggregate fetch; delete-and-replace for re-extraction
- Normalization helpers in the service layer: CPF/CNPJ digit-strip + checksum validation (flag, don't reject), `dd/mm/yyyy` → date, valor string → numeric + moeda

**Acceptance criteria**
- Round-trip test: insert a hand-built `ExtractedMatricula` fixture aggregate, fetch it back, deep-equal
- CPF/CNPJ and date normalization unit tests green
- Migration up/down clean

---

