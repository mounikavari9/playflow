# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

PlayFlow is a payment processing platform. Target stack: Go + Gin, PostgreSQL, Redis, Kafka, Stripe (test mode), React, Docker, Kubernetes, AWS, GitHub Actions.

Monorepo layout:

| Path | Contents | Status |
|---|---|---|
| `backend/` | Go API service (module `github.com/mounikavari9/playflow/backend`, Go 1.25.1) | Step 1 done |
| `web/` | React frontend | not yet created |
| `deploy/` | Kubernetes manifests, AWS infrastructure | not yet created |
| `docker-compose.yml`, `Makefile`, `.github/workflows/` | local infra, dev commands, CI | not yet created |

PAY-001 builds the foundation one step at a time. Status:

- **Step 1 (done):** these files exist in `backend/`:
  - `go.mod` and `go.sum` (Gin, testify)
  - `cmd/api/main.go`: entrypoint with graceful shutdown
  - `internal/config`: env config and its tests
  - `internal/server`: Gin router, request-logging middleware, `http.Server` timeouts, and tests
  - `internal/health`: `GET /healthz` liveness handler and its tests
- **Planned, not implemented yet:** PostgreSQL, Redis, Dockerfile and Docker Compose, Makefile, `/readyz` readiness checks, golangci-lint, GitHub Actions. Don't assume these exist.

## Commands

Run from `backend/`:

```sh
go run ./cmd/api                                   # start API on :8080 (PORT, APP_ENV, SHUTDOWN_TIMEOUT env vars)
go test -race ./...                                # all unit tests
go test -race ./internal/health -run TestLiveness -v   # single test
go vet ./...
gofmt -l .                                         # must print nothing
```

Check: `curl -i localhost:8080/healthz` → `200 {"status":"ok"}`.

## Architecture

- `cmd/api/main.go` is the only place dependencies are wired. `run()` loads config, builds the server, and handles SIGINT/SIGTERM with a graceful `Shutdown` bounded by `ShutdownTimeout`. Global side effects such as `gin.SetMode` happen here and nowhere else.
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
- Infrastructure clients will go in `internal/platform/<name>`. Domain packages should depend on interfaces, not on drivers.

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
