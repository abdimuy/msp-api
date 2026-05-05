.PHONY: help setup build run dev test test-unit test-integration lint lint-fix fmt generate migrate-up migrate-down migrate-create migrate-version clean

# ── Config ───────────────────────────────────────────────────────────
APP_NAME      := msp-api
API_BIN       := bin/api
MIGRATOR_BIN  := bin/migrator
SYNC_BIN      := bin/microsip-sync
GO            := go
GOFLAGS       := -trimpath
LDFLAGS       := -s -w -X main.version=$(shell git rev-parse --short HEAD 2>/dev/null || echo dev) -X main.buildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Database (load from .env if present)
ifneq (,$(wildcard .env))
include .env
export
endif

DB_HOST       ?= localhost
DB_PORT       ?= 5432
DB_USER       ?= msp
DB_PASS       ?= msp
DB_NAME       ?= msp_dev
DB_SSLMODE    ?= disable
DATABASE_URL  ?= postgres://$(DB_USER):$(DB_PASS)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=$(DB_SSLMODE)

MIGRATIONS_DIR := migrations

# ── Help ─────────────────────────────────────────────────────────────
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ── Setup ────────────────────────────────────────────────────────────
setup: ## First-time setup (install lefthook hooks, copy .env)
	lefthook install
	@[ -f .env ] || cp .env.example .env && echo "✔ Created .env from .env.example"
	@echo "✔ Setup complete. Edit .env with your local values."

# ── Build ────────────────────────────────────────────────────────────
build: build-api build-migrator build-sync ## Build all binaries

build-api: ## Build API server (current OS)
	$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(API_BIN) ./cmd/api

build-migrator: ## Build migrator
	$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(MIGRATOR_BIN) ./cmd/migrator

build-sync: ## Build microsip sync worker
	$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(SYNC_BIN) ./cmd/microsip-sync

build-windows: ## Cross-compile all binaries to Windows amd64 (.exe)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(API_BIN).exe ./cmd/api
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(MIGRATOR_BIN).exe ./cmd/migrator
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(SYNC_BIN).exe ./cmd/microsip-sync
	@echo "✔ Built Windows binaries in bin/"

# ── Run ──────────────────────────────────────────────────────────────
run: ## Run API server
	$(GO) run ./cmd/api

dev: ## Run with hot reload (air)
	air

# ── Test ─────────────────────────────────────────────────────────────
test: test-unit ## Run unit tests (default)

test-unit: ## Run unit tests with race detector (skips integration)
	$(GO) test ./... -race -count=1 -short -timeout 180s

test-integration: ## Run integration tests (needs Postgres + optional Firebird)
	INTEGRATION=1 $(GO) test ./... -race -count=1 -timeout 600s

test-all: ## Run all tests (unit + integration)
	INTEGRATION=1 $(GO) test ./... -race -count=1 -timeout 600s

coverage: ## Generate coverage report
	$(GO) test ./... -race -count=1 -short -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "✔ Coverage report: coverage.html"

bench: ## Run benchmarks
	$(GO) test -bench=. -benchmem -run=^$$ ./...

# ── Lint / Format ────────────────────────────────────────────────────
lint: ## Run golangci-lint
	golangci-lint run ./...

lint-fix: ## Run golangci-lint with --fix
	golangci-lint run --fix ./...

fmt: ## Format code (gofumpt + goimports)
	gofumpt -l -w .
	goimports -local github.com/abdimuy/msp-api -l -w .

vet: ## Run go vet
	$(GO) vet ./...

tidy: ## Run go mod tidy
	$(GO) mod tidy

# ── Code generation ──────────────────────────────────────────────────
generate: sqlc ## Run all generators

sqlc: ## Generate SQL code from queries/
	sqlc generate

# ── Migrations (golang-migrate) ──────────────────────────────────────
migrate-up: ## Apply all pending migrations
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

migrate-down: ## Rollback last migration
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1

migrate-down-all: ## Rollback ALL migrations (careful!)
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down -all

migrate-create: ## Create a new migration. Usage: make migrate-create NAME=create_clientes_table
	@[ -n "$(NAME)" ] || (echo "❌ NAME required: make migrate-create NAME=create_clientes_table" && exit 1)
	migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq $(NAME)

migrate-version: ## Show current migration version
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" version

migrate-force: ## Force set migration version (dangerous, recovery only). Usage: make migrate-force VERSION=N
	@[ -n "$(VERSION)" ] || (echo "❌ VERSION required: make migrate-force VERSION=N" && exit 1)
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" force $(VERSION)

# ── Clean ────────────────────────────────────────────────────────────
clean: ## Remove build artifacts
	rm -rf bin/ tmp/ dist/ coverage.out coverage.html

# ── CI helpers ───────────────────────────────────────────────────────
ci-check: vet lint test-unit ## Run checks expected to pass in CI

# ── Local Postgres via Docker (dev only) ─────────────────────────────
db-up: ## Start local Postgres (Docker, dev only)
	docker run -d --name msp-postgres \
		-e POSTGRES_USER=$(DB_USER) -e POSTGRES_PASSWORD=$(DB_PASS) -e POSTGRES_DB=$(DB_NAME) \
		-p $(DB_PORT):5432 \
		--restart unless-stopped \
		postgres:17-alpine

db-down: ## Stop and remove local Postgres
	docker rm -f msp-postgres 2>/dev/null || true

db-logs: ## Tail local Postgres logs
	docker logs -f msp-postgres

db-shell: ## psql shell into local Postgres
	docker exec -it msp-postgres psql -U $(DB_USER) -d $(DB_NAME)

db-reset: db-down db-up ## Recreate local Postgres from scratch
	@sleep 2
	@$(MAKE) migrate-up
