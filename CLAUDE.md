# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

PlayFlow is a payment processing platform. Target stack: Go + Gin, PostgreSQL, Redis, Kafka, Stripe (test mode), React, Docker, Kubernetes, AWS, GitHub Actions.

Monorepo layout:

| Path | Contents | Status |
|---|---|---|
| `backend/` | Go API service (module `github.com/mounikavari9/playflow/backend`, Go 1.25.1) | Steps 1–3 done |
| `web/` | React frontend | not yet created |
| `deploy/` | Kubernetes manifests, AWS infrastructure | not yet created |
| `docker-compose.yml`, `.env.example` | local infra (PostgreSQL only so far), dev-only env template | Step 2 done |
| `Makefile`, `.github/workflows/` | dev commands, CI | not yet created |

PAY-001 builds the foundation one step at a time. Status:

- **Step 1 (done):** these files exist in `backend/`:
  - `go.mod` and `go.sum` (Gin, testify)
  - `cmd/api/main.go`: entrypoint with graceful shutdown
  - `internal/config`: env config and its tests
  - `internal/server`: Gin router, request-logging middleware, `http.Server` timeouts, and tests
  - `internal/health`: `GET /healthz` liveness handler and its tests
- **Step 2 (done):** PostgreSQL.
  - `internal/config`: `DATABASE_URL` (required), `DB_MAX_CONNS`, `DB_CONNECT_TIMEOUT`
  - `internal/platform/postgres`: `New()` builds a `pgxpool` (pgx v5) and pings it; unit tests plus a gated integration test
  - `cmd/api/main.go`: connects at startup (fails fast if unreachable) and closes the pool after HTTP shutdown
  - `docker-compose.yml`: `postgres:17-alpine` on `127.0.0.1:5432`, dev-only credentials
- **Step 3 (done):** Goose SQL migrations.
  - `backend/migrations/00001_create_set_updated_at_function.sql`: shared `set_updated_at()` trigger function, reversible. There are no tables yet.
  - Goose is a pinned CLI run with `go run`, not an application dependency. The API does **not** run migrations at startup.
- **Planned, not implemented yet:** domain tables, Redis, app Dockerfile and app service in Compose, Makefile, `/readyz` readiness checks, golangci-lint, GitHub Actions. Don't assume these exist.

## Commands

Local PostgreSQL, from the repo root (credentials are dev-only defaults; override in a gitignored `.env`, see `.env.example`):

```sh
docker compose up -d --wait postgres              # start and wait until healthy
docker compose down                               # stop (add -v to wipe the data volume)
```

Run from `backend/`:

```sh
export DATABASE_URL='postgres://playflow:playflow@localhost:5432/playflow?sslmode=disable'
go run ./cmd/api                                   # start API on :8080 (env: PORT, APP_ENV, SHUTDOWN_TIMEOUT, DATABASE_URL, DB_MAX_CONNS, DB_CONNECT_TIMEOUT)
go test -race ./...                                # all unit tests (DB integration test is skipped)
TEST_DATABASE_URL="$DATABASE_URL" go test -race ./internal/platform/postgres -v   # integration test against Compose Postgres
go test -race ./internal/health -run TestLiveness -v   # single test
go vet ./...
gofmt -l .                                         # must print nothing
```

Check: `curl -i localhost:8080/healthz` → `200 {"status":"ok"}`.

Migrations, from `backend/` with `DATABASE_URL` exported. Goose is pinned to **v3.27.0** because it's the newest release that builds on Go 1.25.x; v3.28.0+ need Go 1.26. `GOTOOLCHAIN=local` makes a version bump fail loudly instead of silently downloading a newer toolchain.

```sh
goose() { GOTOOLCHAIN=local go run github.com/pressly/goose/v3/cmd/goose@v3.27.0 "$@"; }   # works in zsh and bash
goose -dir migrations validate                                # check files only, no DB
goose -dir migrations postgres "$DATABASE_URL" status         # Pending / Applied per file
goose -dir migrations postgres "$DATABASE_URL" up             # apply all pending
goose -dir migrations postgres "$DATABASE_URL" down           # roll back the latest one
goose -dir migrations postgres "$DATABASE_URL" version        # current DB version
goose -dir migrations -s create <name> sql                    # new sequential file (00002_<name>.sql)
```

Migration rules:
- Files are sequential (`NNNNN_name.sql`, made with `-s`). Every file needs a working `-- +goose Down` section.
- Wrap function bodies or other statements containing `;` in `-- +goose StatementBegin` / `-- +goose StatementEnd`.
- Each file runs in a transaction by default.
- Never edit a migration once it has been applied anywhere shared. Add a new one instead.
- Before committing a new migration, test the round trip: `up` → `down` → `up`.
- Goose tracks applied versions in the `goose_db_version` table.
- Goose auto-loads `.env` from the current directory (`-env` flag). The project's `.env` lives at the repo root, so keep `GOOSE_*` variables out of any `backend/.env`.

## Architecture

- `cmd/api/main.go` is the only place dependencies are wired. `run()` loads config, connects to PostgreSQL (exits non-zero if it can't), builds the server, and handles SIGINT/SIGTERM with a graceful `Shutdown` bounded by `ShutdownTimeout`; the DB pool closes after HTTP shutdown. Global side effects such as `gin.SetMode` happen here and nowhere else.
- `internal/config`:
  - Config comes **only** from environment variables.
  - `Load()` wraps an injectable `lookupFunc`, so tests never touch the real env.
  - Validation errors are collected with `errors.Join`.
  - `APP_ENV` must be one of `local|test|staging|production`. Gin runs in release mode for anything other than `local`.
- `internal/server`:
  - `New()` returns an `*http.Server` with read, write and idle timeouts set.
  - `NewRouter()` returns the Gin engine, so tests use `router.ServeHTTP` directly.
  - It uses `gin.New()` with `Recovery` and a slog request logger. The logger records the path only, never the query string.
  - `HandleMethodNotAllowed` is on, so a wrong method returns 405.
- `internal/health` holds the probe handlers.
  - `/healthz` (liveness) must **never** check external dependencies, because a database or cache outage must not trigger pod restarts.
  - Dependency checks belong in the planned `/readyz`.
- Infrastructure clients go in `internal/platform/<name>`. Domain packages should depend on interfaces, not on drivers.
- `internal/platform/postgres`: `New(ctx, config.DatabaseConfig)` returns a pinged `*pgxpool.Pool`; the caller closes it. Integration tests run only when `TEST_DATABASE_URL` is set.
- `DATABASE_URL` contains credentials: never log it or put it in error messages. Config and postgres errors deliberately avoid wrapping URL-parse errors because they echo the input.

## Go standards used here

- Dependencies go in through constructors. No package-level mutable state.
- Wrap errors with `%w`. Use `log/slog` (JSON) for all logging. No `panic` outside `main`.
- Write doc comments on exported identifiers.
- Tests:
  - table-driven, `t.Parallel()`, testify `assert`/`require`
  - Gin test mode is set in each package's `TestMain`

## Payment-domain rules

- Money is stored as `int64` minor units plus an ISO 4217 currency code. Never use floats.
- Stripe runs in test mode only: keys must start with `sk_test_`. Reject `sk_live_` in non-production config.
- Every payment mutation takes an idempotency key.

## Workflow conventions

- Branches: `<type>/PAY-<n>-<slug>` (e.g. `chore/PAY-001-project-setup`). Commits use Conventional Commits. PRs target `main`.
- **Claude does not commit, push, open PRs or merge.** The user handles all git operations.
- Implement in small steps. After each step, run `gofmt -l .`, `go vet ./...` and `go test -race ./...`, report the results, and wait for approval before the next step.
