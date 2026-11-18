# PlayFlow developer commands. Run `make` to list them.
#
# Compatible with GNU Make 3.81, the version that ships with macOS. Recipes run
# in /bin/bash, so they behave the same from zsh or bash.
#
# Safety rules for this file:
# - Never add `down -v`, `volume rm`, `prune` or a database reset command.
# - Never `include .env` or put credentials in a Make variable.
# - Keep migration logic in scripts/migrate.sh, which verifies the target first.

SHELL := /bin/bash
.DEFAULT_GOAL := help
.NOTPARALLEL:

# playflow is the development stack. playflow-test is the disposable test stack.
# $(value ...) reads the name as plain text, so Make never expands anything in it.
COMPOSE_PROJECT_NAME ?= playflow
PROJECT_INPUT := $(strip $(value COMPOSE_PROJECT_NAME))
PROJECT := $(if $(PROJECT_INPUT),$(PROJECT_INPUT),playflow)
ifneq ($(words $(PROJECT)),1)
$(error COMPOSE_PROJECT_NAME must be playflow or playflow-test)
endif
ifeq ($(filter playflow playflow-test,$(PROJECT)),)
$(error COMPOSE_PROJECT_NAME must be playflow or playflow-test)
endif

COMPOSE := docker compose -p $(PROJECT) -f docker-compose.yml
MIGRATE := COMPOSE_PROJECT_NAME=$(PROJECT) /bin/bash scripts/migrate.sh

# Go commands never inherit database or Redis settings from your shell, so tests
# cannot reach a real service. GOFLAGS and GOWORK are fixed so they cannot rewrite
# go.mod or pull in another module. Recipes use `cd ./backend` because a plain
# `cd backend` follows CDPATH and could enter a different project.
GO := env -u DATABASE_URL -u REDIS_URL -u TEST_DATABASE_URL -u TEST_REDIS_URL \
	-u PGHOST -u PGPORT -u PGUSER -u PGPASSWORD -u PGDATABASE -u PGSERVICE \
	-u PGSERVICEFILE -u PGPASSFILE -u PGSSLMODE \
	GOTOOLCHAIN=local GOFLAGS=-mod=readonly GOWORK=off go

# Project Go files only. Skips the build output in backend/bin, vendored code,
# testdata and hidden folders. Packages such as internal/bin are still included.
GO_FILES := find backend -type d \( -path backend/bin -o -path backend/vendor -o -name testdata -o -name '.*' \) -prune -o -type f -name '*.go'

.PHONY: help up down logs ps build test test-race fmt fmt-check vet check \
	migrate-status migrate-up migrate-down \
	build-verify migrate-validate require-go require-docker

help: ## Show available commands
	@echo "PlayFlow commands:"
	@echo
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  make %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

up: require-docker ## Build and start PostgreSQL, Redis and the API
	@$(COMPOSE) up -d --build --wait

down: require-docker ## Stop the stack and keep the database volume
	@$(COMPOSE) down

logs: require-docker ## Follow API logs (press Ctrl-C to stop)
	@$(COMPOSE) logs -f --tail=100 api

ps: require-docker ## Show container status
	@$(COMPOSE) ps

build: require-go ## Build the Go binaries into backend/bin
	@echo "==> Building backend/bin/api and backend/bin/healthcheck"
	@cd ./backend && $(GO) build -o bin/api ./cmd/api
	@cd ./backend && $(GO) build -o bin/healthcheck ./cmd/healthcheck

test: require-go ## Run Go unit tests
	@echo "==> Running unit tests"
	@cd ./backend && $(GO) test ./...

test-race: require-go ## Run Go unit tests with the race detector
	@echo "==> Running unit tests with the race detector"
	@cd ./backend && $(GO) test -race ./...

fmt: require-go ## Format project Go files
	@echo "==> Formatting project Go files"
	@$(GO_FILES) -exec gofmt -w {} +

fmt-check: require-go ## Check Go formatting without changing files
	@echo "==> Checking Go formatting"
	@files=$$($(GO_FILES) -exec gofmt -l {} + 2>&1); \
	if [ -n "$$files" ]; then \
		echo "These files need attention. Run: make fmt"; \
		echo "$$files"; \
		exit 1; \
	fi

vet: require-go ## Run go vet
	@echo "==> Running go vet"
	@cd ./backend && $(GO) vet ./...

check: fmt-check vet test-race build-verify migrate-validate ## Run all checks without changing files or the database
	@echo "==> All checks passed"

migrate-status: require-go require-docker ## Show migrations (first use creates Goose's tracking table)
	@$(MIGRATE) status

migrate-up: require-go require-docker ## Apply pending migrations to the local database
	@$(MIGRATE) up

migrate-down: require-go require-docker ## Roll back one migration after typed confirmation
	@$(MIGRATE) down

# Helpers used by the targets above. They are not listed by `make help`.

build-verify: require-go
	@echo "==> Verifying that the binaries build"
	@cd ./backend && $(GO) build -o /dev/null ./cmd/api
	@cd ./backend && $(GO) build -o /dev/null ./cmd/healthcheck

migrate-validate: require-go
	@echo "==> Validating migration files"
	@$(MIGRATE) validate

require-go:
	@command -v go >/dev/null 2>&1 || { echo "Go is not installed. Install Go 1.25 from https://go.dev/dl/"; exit 1; }

require-docker:
	@command -v docker >/dev/null 2>&1 || { echo "Docker is not installed. Install Docker Desktop first."; exit 1; }
	@docker info >/dev/null 2>&1 || { echo "Docker is not running. Start Docker Desktop and try again."; exit 1; }
	@docker compose version >/dev/null 2>&1 || { echo "Docker Compose is not available. Update Docker Desktop."; exit 1; }
