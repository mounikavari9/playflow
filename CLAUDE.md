# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

PlayFlow is a payment processing platform. Target stack: Go + Gin, PostgreSQL, Redis, Kafka, Stripe (test mode), React, Docker, Kubernetes, AWS, GitHub Actions.

Monorepo layout:

| Path | Contents | Status |
|---|---|---|
| `backend/` | Go API service (module `github.com/mounikavari9/playflow/backend`, Go 1.25.1) | Steps 1–6 done |
| `web/` | React frontend | not yet created |
| `deploy/` | Kubernetes manifests, AWS infrastructure | not yet created |
| `docker-compose.yml`, `.env.example` | local stack (PostgreSQL, Redis and the API), dev-only env template | Steps 2, 5 and 6 done |
| `Makefile`, `scripts/migrate.sh` | developer commands, verified local migrations | Step 7 done |
| `.github/workflows/` | CI | not yet created |

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
- **Step 4 (done):** `GET /readyz` readiness probe.
  - `internal/health`: `Pinger` interface and `Readiness()` handler, which pings PostgreSQL with a 2s timeout
  - `internal/server`: `New` and `NewRouter` take a `health.Pinger`; `cmd/api/main.go` passes the pool
- **Step 5 (done):** Redis connection (no Redis features yet).
  - `go.mod`: `github.com/redis/go-redis/v9` pinned to **v9.22.0**, the newest release that builds on Go 1.25.x; v9.23.0+ need Go 1.26. Add or upgrade with `GOTOOLCHAIN=local go get`.
  - `internal/config`: `REDIS_URL` (required), `REDIS_POOL_SIZE`, `REDIS_CONNECT_TIMEOUT`
  - `internal/platform/redis`: `New()` builds a go-redis client and pings it; unit tests plus gated integration tests
  - `cmd/api/main.go`: connects after PostgreSQL (fails fast if unreachable) and closes the client after HTTP shutdown
  - `docker-compose.yml`: `redis:8-alpine` on `127.0.0.1:6379`, password required, no data volume
  - Redis is deliberately **not** checked by `/readyz`.
- **Step 6 (done):** containerized API.
  - `backend/Dockerfile`: multi-stage build on `golang:1.25.14-bookworm`, runtime on `gcr.io/distroless/static-debian12:nonroot`, both pinned by digest
  - `backend/.dockerignore`: allowlist for the build context, with explicit exclusions for env files, private keys and credentials
  - `cmd/healthcheck`: standard-library probe binary, because the runtime image has no shell, curl or wget
  - `docker-compose.yml`: `api` service on `127.0.0.1:${API_PORT:-8080}`
  - `docker-compose.yml`: the `postgres` healthcheck now uses `pg_isready -h 127.0.0.1`, so it tests TCP
- **Step 7 (done):** developer commands.
  - `Makefile`: Compose, Go quality and migration targets, compatible with macOS Make 3.81
  - `scripts/migrate.sh`: verifies the local database, then runs Goose
- **Planned, not implemented yet:** domain tables, Redis features (caching, rate limiting), automated migrations in Compose, golangci-lint, GitHub Actions. Don't assume these exist.

## Commands

Everyday commands, from the repo root. Run `make` to list them:

```sh
make up                # build and start PostgreSQL, Redis and the API
make down              # stop the stack and keep the database volume
make logs              # follow API logs
make ps                # show container status
make build             # build backend/bin/api and backend/bin/healthcheck
make test              # unit tests
make test-race         # unit tests with the race detector
make fmt               # format project Go files
make fmt-check         # check formatting without changing files
make vet               # go vet
make check             # fmt-check, vet, test-race, build and migration validation
make migrate-status    # show applied and pending migrations (first use creates goose_db_version)
make migrate-up        # apply pending migrations
make migrate-down      # roll back one migration after typing its version number
```

Prerequisites: Docker Desktop for Mac, Go 1.25 and Make. The first `make check` or migration command downloads Goose v3.27.0, so it needs internet access.

Run migration and rollback experiments only in the disposable `playflow-test` project:

```sh
make_test() { COMPOSE_PROJECT_NAME=playflow-test POSTGRES_PORT=55432 REDIS_PORT=56379 API_PORT=18080 make "$@"; }   # works in zsh and bash
make_test up
make_test migrate-up
make_test down
docker compose -p playflow-test down -v           # deletes only the test project's volume
```

After moving the repository to another folder, run `make down` and then `make up` from the new folder. Migration commands refuse containers started from a different folder. The database volume is kept.

The raw commands below remain available for cases the Makefile does not cover.

Local PostgreSQL and Redis, from the repo root (credentials are dev-only defaults; override in a gitignored `.env`, see `.env.example`):

```sh
docker compose up -d --wait postgres redis        # start and wait until healthy
docker compose down                               # stop (add -v to wipe the data volume)
```

Containerized API, from the repo root:

```sh
docker compose up -d --build --wait               # build the API and start the full stack
docker compose logs -f api                        # follow API logs
docker compose stop api                           # graceful stop (exit code 0)
API_PORT=18080 docker compose up -d --wait api    # use another host port if 8080 is taken
```

Run failure tests in a separate Compose project so the development volume is never touched. This helper uses `.env.example` values and separate ports. The ports apply to each call only and are not exported to your shell:

```sh
compose_test() { POSTGRES_PORT=55432 REDIS_PORT=56379 API_PORT=18080 docker compose -p playflow-test --env-file .env.example "$@"; }   # works in zsh and bash
compose_test up -d --build --wait                 # start the isolated stack; the API is on 127.0.0.1:18080
compose_test down -v                              # removes only this project's containers and volumes
```

Run from `backend/`:

```sh
export DATABASE_URL='postgres://playflow:playflow@localhost:5432/playflow?sslmode=disable'
export REDIS_URL='redis://:playflow@localhost:6379/0'
go run ./cmd/api                                   # start API on :8080 (env: PORT, APP_ENV, SHUTDOWN_TIMEOUT, DATABASE_URL, DB_MAX_CONNS, DB_CONNECT_TIMEOUT, REDIS_URL, REDIS_POOL_SIZE, REDIS_CONNECT_TIMEOUT)
go test -race ./...                                # all unit tests (integration tests are skipped)
TEST_DATABASE_URL="$DATABASE_URL" go test -race ./internal/platform/postgres -v   # integration test against Compose Postgres
TEST_REDIS_URL="$REDIS_URL" go test -race ./internal/platform/redis -v            # integration tests against Compose Redis
go test -race ./internal/health -run TestLiveness -v   # single test
go vet ./...
gofmt -l .                                         # must print nothing
```

Checks:
- `curl -i localhost:8080/healthz` → `200 {"status":"ok"}`, whatever state the database is in.
- `curl -i localhost:8080/readyz` → `200 {"status":"ok"}` while the database is up. It returns `503 {"status":"unavailable"}` after `docker compose stop postgres` (immediately) or `docker compose pause postgres` (after the 2s timeout).
- After `docker compose start` or `unpause`, `/readyz` returns 200 again without restarting the API, because pgxpool reconnects on its own.
- `docker compose stop redis` (or `pause`) while the API runs: `/healthz` and `/readyz` both stay 200 and the API keeps running.
- Starting the API while Redis is stopped exits 1 within about 1–2s (`redis: ping: … connection refused`). While Redis is paused it exits after `REDIS_CONNECT_TIMEOUT` (`context deadline exceeded`). A wrong password exits with `WRONGPASS`. Port 8080 never opens in any of these cases.
- Redis is used by nothing after startup yet, so a Redis restart has no visible effect. go-redis dials new connections on demand.

Container checks, using the `compose_test` helper above and the API on `127.0.0.1:18080`:
- `compose_test up -d --build --wait` exits 0 on empty volumes, and `compose_test ps api` shows the API as healthy.
- `docker image inspect -f '{{.Config.User}}' playflow-api:local` → `65532:65532`.
- `docker top "$(compose_test ps -q api)" -eo pid,uid,args` shows the API running as UID `65532`.
- `docker inspect -f '{{.HostConfig.ReadonlyRootfs}} {{.HostConfig.CapDrop}}' "$(compose_test ps -q api)"` → `true [ALL]`.
- `docker run --rm --entrypoint /bin/sh playflow-api:local` fails, because the image has no shell.
- `docker exec "$(compose_test ps -q api)" /usr/local/bin/healthcheck /readyz` exits 0, and the same command with `/missing` exits 1.
- `compose_test stop postgres`: `/readyz` returns 503, `/healthz` stays 200, and the container turns unhealthy but keeps running. After `compose_test start postgres`, `/readyz` returns 200 and the container is healthy again without an API restart.
- `compose_test stop redis`: `/healthz` and `/readyz` both stay 200.
- `compose_test stop api`: `docker inspect -f '{{.State.ExitCode}}' "$(compose_test ps -aq api)"` → `0`, and `compose_test logs api` contains `api stopped gracefully`.
- With Redis stopped, `compose_test up -d --no-deps api` exits the API with code 1 and port 18080 stays closed.

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

- `cmd/api/main.go` is the only place dependencies are wired. `run()` loads config, connects to PostgreSQL and then Redis (exits non-zero if either fails), builds the server, and handles SIGINT/SIGTERM with a graceful `Shutdown` bounded by `ShutdownTimeout`; the Redis client and then the DB pool close after HTTP shutdown. Global side effects such as `gin.SetMode` happen here and nowhere else.
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
  - `/readyz` (readiness) is built by `health.Readiness(db Pinger, timeout, logger)`. It pings the database with a deadline taken from the request context, and the timeout is the `readinessTimeout` constant (2s) in `internal/server`.
  - **Kubernetes:** the readiness probe's `timeoutSeconds` must be greater than 2. The Kubernetes default is 1.
  - Responses carry only `{"status":"ok"}` (200) or `{"status":"unavailable"}` (503), never error details.
  - A failed check logs only `dependency` and a `reason` of `timeout`, `canceled` or `unavailable`. The reason comes from the context's state, never from the raw driver error, which can name the host and user.
  - `health.Pinger` keeps the package free of pgx; `*pgxpool.Pool` satisfies it.
- Infrastructure clients go in `internal/platform/<name>`. Domain packages should depend on interfaces, not on drivers.
- `internal/platform/postgres`: `New(ctx, config.DatabaseConfig)` returns a pinged `*pgxpool.Pool`; the caller closes it. Integration tests run only when `TEST_DATABASE_URL` is set.
- `DATABASE_URL` contains credentials: never log it or put it in error messages. Config and postgres errors deliberately avoid wrapping URL-parse errors because they echo the input.
- `internal/platform/redis`: `New(ctx, config.RedisConfig)` returns a pinged `*goredis.Client` (go-redis is imported as `goredis`); the caller closes it. Integration tests run only when `TEST_REDIS_URL` is set.
  - `ContextTimeoutEnabled` is on, so context deadlines apply to every Redis command.
  - The ping error is wrapped, but replaced by an opaque error if its text ever contains the password. go-redis dial errors name only `host:port`, and AUTH failures are the server's `WRONGPASS` reply.
  - `options()` refuses `InsecureSkipVerify` as a second line of defense.
- Redis readiness and startup policy:
  - Redis is a **soft dependency** and is **not** in `/readyz`. Every pod shares one Redis, so gating readiness on it would mark every pod unready at once and turn a Redis outage into a full API outage. Data that must survive (payments, ledgers, idempotency keys) lives in PostgreSQL. Future Redis features must degrade on their own: a cache miss falls through to the DB, and rate limiting fails open or closed as an explicit choice.
  - Startup still **fails fast** if Redis is unreachable, so misconfiguration surfaces at deploy time. **Tradeoff:** while Redis is down, new pods (scale-up, rollouts) crash-loop even though running pods keep serving. Revisit, and consider starting degraded with a warning, once Redis has real consumers.
- `REDIS_URL` rules (`redis[s]://[user]:password@host[:port][/db]`):
  - It contains credentials: never log it or put it in error messages. `RedisConfig.LogValue` redacts it.
  - Only `redis` and `rediss` schemes; host required; the path may only be a non-negative DB number.
  - **Query parameters are rejected.** go-redis maps them onto client options, including `skip_verify`, which disables certificate verification.
  - **TLS:** in `staging` and `production`, `rediss://` is required unless the host is loopback (`localhost`, `127.0.0.0/8`, `::1`). The loopback exception supports a TLS-terminating sidecar proxy in the pod. A transparent service mesh where the app dials a cluster hostname over plaintext is **not** covered and would need a deliberate change. TLS always verifies certificates against the system CA store, with TLS 1.2 minimum. Supply a private CA through Go's standard `SSL_CERT_FILE`/`SSL_CERT_DIR`, not a new option.
  - Known gap (out of scope for now): go-redis writes some internal messages as plain text to stderr (e.g. `pool.go: failed to dial`), outside slog. They name only `host:port`.
- Container rules:
  - The image runs as numeric UID `65532`, so Kubernetes `runAsNonRoot` can verify it. Compose also sets a read-only root filesystem, drops all capabilities and enables `no-new-privileges`.
  - `ENTRYPOINT` stays in exec form so the API is PID 1 and receives SIGTERM directly.
  - `stop_grace_period` (15s) must stay longer than `SHUTDOWN_TIMEOUT` (10s).
  - The health check runs `healthcheck /readyz` against the container's internal `PORT`. `API_PORT` changes only the host side. The probe's 3s limit must stay between the 2s `/readyz` timeout and Compose's 5s `timeout`.
  - A database outage marks the container unhealthy, but Compose does not restart it. There is deliberately no restart policy, so startup failures stay visible.
  - Compose builds `DATABASE_URL` and `REDIS_URL` itself with the `postgres` and `redis` hostnames. Do not use `env_file: .env`, because its `localhost` URLs are for host-run tools. Passwords are inserted unencoded, so they must be URL-safe.
  - Keep `-h 127.0.0.1` in the PostgreSQL healthcheck. On first boot the image runs a temporary server on the Unix socket only, and a socket check could report healthy before the real server starts. The API fails fast and has no restart policy, so that race would stop it.
  - The image contains no migrations. Run Goose from the host against the published PostgreSQL port.
  - Bumping the Go or distroless image means updating both the tag and its digest. Go 1.25 no longer receives upstream patches, because Go 1.27 is current. Upgrading Go is separate work.
- Makefile rules:
  - Stay compatible with GNU Make 3.81 and Bash 3.2, the macOS versions. Avoid `.ONESHELL`, `.SHELLFLAGS`, `!=` and `$(file ...)`.
  - Never add `down -v`, `volume rm`, `prune`, a database reset or a Goose `reset` or `down-to` target.
  - Never `include .env` or put credentials in a Make variable, because `make -n` and `make -p` print them.
  - `COMPOSE_PROJECT_NAME` may only be `playflow` or `playflow-test`. Run failure and rollback tests with `playflow-test`, on separate ports.
  - `COMPOSE_PROJECT_NAME` is read with `$(value ...)`, so Make never expands text inside it.
  - Go targets remove inherited `DATABASE_URL`, `REDIS_URL`, `TEST_DATABASE_URL` and `TEST_REDIS_URL`, so `make test` skips integration tests. They also remove the nine PostgreSQL settings named in the `GO` variable: `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE`, `PGSERVICE`, `PGSERVICEFILE`, `PGPASSFILE` and `PGSSLMODE`. Other `PG*` variables are not removed.
  - Recipes enter the backend with `cd ./backend`, because a plain `cd backend` follows `CDPATH` and could enter another project. `GOWORK=off` keeps other modules out of the build.
  - `make fmt` and `make fmt-check` skip only `backend/bin`, `backend/vendor`, `testdata` and hidden folders.
  - `make check` must not change source files or database state.
- `scripts/migrate.sh` rules:
  - Run it as `bash scripts/migrate.sh validate|status|up|down`, normally through the Makefile.
  - It restarts itself in a clean shell with an allowlisted environment, so inherited tracing, `BASH_ENV`, `CDPATH`, exported functions and `GOOSE_*`, `PG*` or `*_URL` settings never reach it.
  - It never reads `DATABASE_URL`. It takes the credentials and port from the running PostgreSQL container and always connects to `127.0.0.1`.
  - Before Goose runs it verifies a local Docker endpoint, the Compose project, service, repository folder, image, health and data volume, and a single loopback port. It then proves the port reaches that container by sending a credential-free PostgreSQL startup message with a random user name and finding that name in the container's process list. Credentials are read only after this proof, and any failed check stops the command.
  - The connection string goes to Goose through `GOOSE_DBSTRING`, never command arguments, and Goose output is redacted.
  - `make migrate-down` requires an interactive terminal and the exact version number. There is deliberately no bypass.
  - `make migrate-up` and `make migrate-down` hold a per-project lock in `$TMPDIR`, so they can't overlap on this machine. **Residual risk:** raw Goose commands and other machines don't take this lock, so never run them during a migration.
  - **Goose v3.27.0 writes on first use:** `status` and `version` create `goose_db_version` and insert a version 0 row if the table is missing, so `make migrate-status` is read-only only after that.

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
