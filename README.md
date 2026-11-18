# PlayFlow

## Prerequisites

- Docker Desktop for Mac, running
- Go 1.25
- Make, included with the Xcode Command Line Tools (`xcode-select --install`)
- golangci-lint 2.12.2, for `make lint` and `make check` (see [Install golangci-lint](#install-golangci-lint))
- Internet access the first time you run `make check` or a migration command. Go downloads Goose v3.27.0 once, which can take a minute, and reuses it after that.

## Run locally

Start PostgreSQL, Redis and the API, then apply the database migrations:

```sh
make up
make migrate-up
curl -i localhost:8080/healthz
curl -i localhost:8080/readyz
```

Stop the stack with `make down`. Your database volume is kept.

To change local settings, copy `.env.example` to `.env`. Set `API_PORT` if port 8080 is already in use.

## Commands

Run `make` to see this list.

| Command | What it does | Needs |
|---|---|---|
| `make up` | Build and start PostgreSQL, Redis and the API | Docker |
| `make down` | Stop the stack and keep the database volume | Docker |
| `make logs` | Follow API logs | Docker |
| `make ps` | Show container status | Docker |
| `make build` | Build the Go binaries into `backend/bin` | Go |
| `make test` | Run unit tests | Go |
| `make test-race` | Run unit tests with the race detector | Go |
| `make fmt` | Format project Go files | Go |
| `make fmt-check` | Check formatting without changing files | Go |
| `make vet` | Run `go vet` | Go |
| `make lint` | Run golangci-lint without changing files | Go, golangci-lint |
| `make check` | Run all checks without changing files or the database | Go, golangci-lint |
| `make migrate-status` | Show migrations. First use creates Goose's tracking table | Go, Docker, running PostgreSQL |
| `make migrate-up` | Apply pending migrations | Go, Docker, running PostgreSQL |
| `make migrate-down` | Roll back one migration after confirmation | Go, Docker, running PostgreSQL |

## Install golangci-lint

`make lint` requires exactly version 2.12.2, the newest release that declares support for building on Go 1.25. These steps download the official release for your Mac, stop if its SHA-256 checksum doesn't match, and install the binary into `~/.local/bin`:

```sh
(
  set -eu
  version=2.12.2
  case "$(uname -m)" in
    arm64)  arch=arm64; sha256=a9c54498731b3128f79e090be6110f3e5fffccc617b08142ed244d4126c73f29 ;;
    x86_64) arch=amd64; sha256=f6f06d94b6241521c53d15450c5209b028270bf966f842afb11c030c79f5bc16 ;;
    *) echo "Unsupported Mac architecture"; exit 1 ;;
  esac
  name="golangci-lint-$version-darwin-$arch"
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  cd "$tmp"
  curl -fsSLO "https://github.com/golangci/golangci-lint/releases/download/v$version/$name.tar.gz"
  echo "$sha256  $name.tar.gz" | shasum -a 256 -c -
  tar -xzf "$name.tar.gz" "$name/golangci-lint"
  mkdir -p "$HOME/.local/bin"
  install -m 0755 "$name/golangci-lint" "$HOME/.local/bin/golangci-lint"
)
golangci-lint version --short   # must print 2.12.2
```

If the command isn't found, add `~/.local/bin` to your `PATH`.

## Database migrations

Migration commands only work on the local PostgreSQL container started by `make up`. They ignore any `DATABASE_URL` in your shell, and they stop if the target can't be verified. They're tested with Docker Desktop on macOS and refuse remote Docker engines.

**`make migrate-status` can write to the database once.** If the database has no `goose_db_version` table yet, Goose creates it and adds a version 0 row. This applies no migrations and doesn't touch your data. After that, the command only reads.

**Rolling back:** `make migrate-down` shows the migration it will roll back and asks you to type its version number. A rollback can permanently delete data, so run `make migrate-status` first.

**Run one migration command at a time.** `make migrate-up` and `make migrate-down` block each other on this machine. That protection doesn't cover Goose commands you run by hand, so don't run those at the same time.

**After moving this repository to another folder,** migration commands stop because the running containers belong to the old location. Run `make down`, then `make up` from the new folder. Your database volume is kept.

## Disposable test stack

Use the separate `playflow-test` project to try migrations and rollbacks without touching your development database:

```sh
make_test() { COMPOSE_PROJECT_NAME=playflow-test POSTGRES_PORT=55432 REDIS_PORT=56379 API_PORT=18080 make "$@"; }

make_test up
make_test migrate-up
make_test migrate-status
make_test down
docker compose -p playflow-test down -v   # deletes only the test project's volume
```
