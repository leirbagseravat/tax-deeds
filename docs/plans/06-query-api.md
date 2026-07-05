# Sub-plan 6 — Structured query/read API

**Feature:** the consumable read surface over extracted registry data.

**Scope**
- `internal/handlers/matriculas.go` — `GET /documents/{id}/matricula`, `/matriculas/{id}/proprietarios`, `/atos?kind=`, `/onus?status=`
- `internal/services/matriculas.go` — nested-DTO assembly, ownership-chain derivation (atos ordered by seq, filtered to transfer types)
- Extended `internal/store/matriculas.go` queries + DTOs + route registration
- `scripts/demo.sh` — whole flow end-to-end against docker-compose

**Design notes:** full-matricula endpoint returns the complete nested aggregate in one payload (matrículas have tens of atos, not thousands — no pagination); `409 not_ready` vs `404` so clients distinguish "keep polling" from "wrong id".

**Acceptance criteria**
- `scripts/demo.sh` runs upload→extracted→queries green
- Handler tests with seeded DB; `go test ./...` green

---

