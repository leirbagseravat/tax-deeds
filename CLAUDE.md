# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

`mortgage` (Go 1.26.4) is a working orchestrator for analysing Brazilian
real-estate registry documents (*matrículas*): `upload → ingest → OCR → LLM
extraction → read API`. It has three binaries (`cmd/api`, `cmd/ocr`,
`cmd/migrate`), a Makefile, `docker-compose.yml`, and a full README. For the
architecture, the analysis flow, and diagrams, read **`docs/ARCHITECTURE.md`**
(and `README.md` for setup/config). Follow the layout below when adding code.

## Commands

Use the Makefile targets (`make build|test|vet|fmt|run|up|migrate`) or plain
`go` tooling:

- Build: `go build ./...`
- Run all tests: `go test ./...`
- Run a single test: `go test ./path/to/package -run TestName`
- Vet: `go vet ./...`
- Format: `gofmt -l .` (or `gofmt -w .` to fix)

## Architecture

`.ai/GO_PROJECT_STRUCTURE.md` defines the intended folder layout for this project — read it before scaffolding new packages. Summary:

- `cmd/<entrypoint>/` — application entry points (e.g. `cmd/api`, `cmd/worker`, `cmd/migrate`), one subfolder per binary once there is more than one.
- `internal/` — private implementation, not importable outside this module: `handlers` (HTTP request handling only, no DB logic), `routes` (endpoint registration), `services` (business logic), `store` (data persistence), `clients` (external API integrations), `dto` (request/response objects), `config`.
- `pkg/` — reusable, domain-free utilities only (`logger`, `middleware`, `response`). Anything specific to this app's domain belongs in `internal/`, not `pkg/`.

Dependency direction is one-way: `handlers → services → store`. Never the reverse, and handlers must not contain database logic directly.

For domain logic complex enough to need strict layer isolation, the guideline allows escalating to Clean Architecture (domain/usecase/interface/infrastructure) or Hexagonal Architecture (ports & adapters) instead of the base layout — see the doc for when to use each.
