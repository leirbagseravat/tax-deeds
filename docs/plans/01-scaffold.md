# Sub-plan 1 — Project scaffold & local infrastructure

**Feature:** a runnable, healthy service with local dev environment and migration tooling.

**Scope**
- `Makefile` (run, build, test, vet, fmt, up, down, migrate, migrate-down), `docker-compose.yml` (postgres:17, fsouza/fake-gcs-server with `-scheme http`), `.env.example`
- `cmd/api/main.go` — composition root: config → logger → pgxpool → routes → `http.Server`, graceful shutdown (`signal.NotifyContext`)
- `cmd/migrate/main.go` — goose up/down/status via pgx stdlib adapter; migrations embedded (`embed.FS`); only place using `database/sql`
- `internal/config/config.go`, `internal/routes/routes.go`, `internal/handlers/health.go`, `internal/store/db.go`
- `internal/store/migrations/00001_init.sql` (enable pgcrypto for `gen_random_uuid()`)
- `pkg/logger` (slog JSON, level from env), `pkg/middleware` (request-ID, logging, panic recovery), `pkg/response`

**Design notes:** handlers receive dependencies via small structs — no globals.

**Acceptance criteria**
- `make up && make migrate && make run` gives a healthy server
- `curl /healthz` → 200; `/readyz` → 200, and non-200 when Postgres is stopped
- `go vet ./... && go test ./...` green

---

