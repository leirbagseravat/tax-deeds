# Go Project Structure Guidelines

Based on ["Production-Ready Go Folder Structure"](https://medium.com/@gitesky14/production-ready-go-folder-structure-88c1bd0f5a07).

## Layout

```
project/
├── cmd/              # Application entry points
│   ├── api/          # e.g. HTTP server main.go
│   ├── worker/        # e.g. background worker main.go
│   └── migrate/       # e.g. DB migration tool main.go
├── internal/         # Private implementation (not importable by other modules)
│   ├── handlers/      # HTTP request handlers
│   ├── routes/        # Endpoint registration
│   ├── services/      # Business logic
│   ├── store/         # Data persistence layer
│   ├── clients/       # External API integrations
│   ├── dto/           # Request/response objects
│   └── config/        # Configuration helpers
├── pkg/              # Public, reusable utilities (no business logic)
│   ├── logger/
│   ├── middleware/
│   └── response/
├── .air.toml         # Live reload config
├── .env              # Environment variables
├── go.mod
└── Makefile          # Build automation
```

## Core Principles

- **Separation of concerns**: handlers only deal with HTTP + calling services; they must not contain database logic.
- **Dependency direction**: `handlers → services → store`. Never the reverse.
- **`internal/` is enforced by the Go compiler** — other modules cannot import it. Use it for anything that shouldn't be part of your public API.
- **`pkg/` is for genuinely reusable, business-logic-free code** (logging, config loading, middleware, response helpers). If it's specific to this app's domain, it belongs in `internal/`, not `pkg/`.
- Split `cmd/` into subfolders (`cmd/api`, `cmd/worker`, `cmd/migrate`, ...) once the project has more than one entry point.

## Supporting Files

- **Makefile**: standardize common tasks — `make run`, `make test`, `make build`, `make migrate`.
- **.air.toml**: hot-reload during development (used alongside, not instead of, the Makefile).

## Alternatives (for larger/more complex projects)

| Architecture | When to use |
|---|---|
| **Clean Architecture** (domain / usecase / interface / infrastructure layers) | Large teams, strict layering needed |
| **Hexagonal Architecture** (ports & adapters) | Need framework independence and high testability |
| **Repository Pattern** | Simpler projects — just isolate persistence behind interfaces |

For a single-service portfolio project (e.g. a log aggregator + log producer), the base `cmd/ + internal/ + pkg/` layout above is usually sufficient — reach for Clean/Hexagonal only if the domain logic grows complex enough to need strict layer isolation.
