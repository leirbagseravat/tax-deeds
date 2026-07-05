# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

This is a new, empty Go module (`module mortgage`, Go 1.26.4) — no source code exists yet. There is no Makefile, README, or CI config. When adding the first code, follow the layout below rather than inventing an alternative structure.

## Commands

No Makefile exists yet; use plain `go` tooling until one is added:

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
