.PHONY: help setup build run dev test test-unit test-integration test-all test-mutation test-mutation-domain test-mutation-app test-mutation-ventas test-mutation-ventas-domain test-mutation-ventas-app test-mutation-httpdispatch lint lint-fix fmt generate migrate-up migrate-down migrate-create migrate-version clean db-test-up db-test-down db-test-reset db-test-prune db-test-url test-firebird test-firebird-all coverage-auth coverage-auth-full fb-migrate-up fb-migrate-down fb-migrate-status fb-seed-admin fb-snapshot fb-snapshot-list fb-restore fb-snapshot-delete fb-emu-up fb-emu-down fb-emu-logs

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

test-integration: db-test-up ## Run integration tests against ONE shared Postgres (all packages reuse msp-postgres-test)
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" INTEGRATION=1 $(GO) test ./... -race -count=1 -timeout 600s

test-all: db-test-up ## Run all tests (unit + integration) sharing one Postgres
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" INTEGRATION=1 $(GO) test ./... -race -count=1 -timeout 600s

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

# ── Firebird migrations (DEV) ────────────────────────────────────────
# `golang-migrate` no soporta Firebird, así que aplicamos los .sql via isql
# dentro del container `mueblera-firebird`. Estos targets son para desarrollo
# local — en producción se aplican manualmente con autorización del DBA.

FB_MIGRATIONS_DIR    := migrations-firebird
FB_SEEDS_DIR         := seeds-firebird
FB_CONTAINER         ?= mueblera-firebird
FB_USER              ?= SYSDBA
FB_PASSWORD          ?= masterkey
FB_DATABASE          ?= /firebird/data/MUEBLERA.FDB

# Apply only pending *.up.sql migrations. Idempotent — reads MSP_MIGRATIONS
# first and skips any ID already in there. Bootstraps when the table doesn't
# exist yet (initial install) by treating "table not found" as "nothing
# applied".
fb-migrate-up: ## Apply pending Firebird migrations (idempotent; dev only)
	@applied=$$(docker exec -i $(FB_CONTAINER) /usr/local/firebird/bin/isql \
			-u $(FB_USER) -p $(FB_PASSWORD) -ch UTF8 -q $(FB_DATABASE) \
			<<<'SELECT ID FROM MSP_MIGRATIONS;' 2>/dev/null \
		| awk 'NR>2 && $$1 ~ /^[0-9]+$$/ {print $$1}' | tr '\n' ' '); \
	echo "Applied so far: [$$applied]"; \
	for f in $$(ls $(FB_MIGRATIONS_DIR)/*.up.sql | sort); do \
		id=$$(basename $$f | sed -E 's/^0*([0-9]+)_.*/\1/'); \
		if echo " $$applied " | grep -q " $$id "; then \
			echo "↷ skip $$f (id=$$id already applied)"; \
			continue; \
		fi; \
		echo "▶ $$f (id=$$id)"; \
		docker exec -i $(FB_CONTAINER) /usr/local/firebird/bin/isql \
			-u $(FB_USER) -p $(FB_PASSWORD) -ch UTF8 $(FB_DATABASE) < $$f \
			|| (echo "❌ Failed at $$f" && exit 1); \
	done
	@echo "✔ Firebird migrations applied"

# Rollback the latest migration. Usage: make fb-migrate-down N=000002
fb-migrate-down: ## Rollback one Firebird migration. Usage: make fb-migrate-down N=000002
	@[ -n "$(N)" ] || (echo "❌ N required: make fb-migrate-down N=000002" && exit 1)
	@f=$$(ls $(FB_MIGRATIONS_DIR)/$(N)_*.down.sql 2>/dev/null); \
	[ -n "$$f" ] || (echo "❌ No down migration found for N=$(N)" && exit 1); \
	echo "▶ $$f"; \
	docker exec -i $(FB_CONTAINER) /usr/local/firebird/bin/isql \
		-u $(FB_USER) -p $(FB_PASSWORD) -ch UTF8 $(FB_DATABASE) < $$f

# List applied Firebird migrations
fb-migrate-status: ## Show applied Firebird migrations
	@docker exec -i $(FB_CONTAINER) /usr/local/firebird/bin/isql \
		-u $(FB_USER) -p $(FB_PASSWORD) -ch UTF8 $(FB_DATABASE) <<<"SELECT ID, NAME, APPLIED_AT FROM MSP_MIGRATIONS ORDER BY ID;"

# ── Firebird snapshot / restore ─────────────────────────────────────
# Hot backup via gbak — the canonical Firebird tool. No downtime needed:
# gbak takes a consistent snapshot of the running DB. Output is a .fbk
# logical backup file, portable across Firebird hosts. Restores REPLACE
# the active DB file (destructive — you lose anything written since the
# snapshot).
#
# Snapshots live under data/firebird-snapshots/ (gitignored). The dir is
# created on first snapshot.

FB_SNAPSHOTS_DIR := data/firebird-snapshots

fb-snapshot: ## Take a hot snapshot of the dev DB. NAME=foo optional (defaults to timestamp).
	@mkdir -p $(FB_SNAPSHOTS_DIR)
	@name="$${NAME:-snapshot_$$(date +%Y%m%d_%H%M%S)}"; \
	echo "▶ creating snapshot $$name.fbk (this takes ~30s for 3.6GB)"; \
	docker exec $(FB_CONTAINER) /usr/local/firebird/bin/gbak \
		-b -v -t -user $(FB_USER) -password $(FB_PASSWORD) \
		$(FB_DATABASE) /tmp/$$name.fbk \
		|| (echo "❌ gbak backup failed" && exit 1); \
	docker cp $(FB_CONTAINER):/tmp/$$name.fbk $(FB_SNAPSHOTS_DIR)/$$name.fbk \
		|| (echo "❌ docker cp failed" && exit 1); \
	docker exec $(FB_CONTAINER) rm -f /tmp/$$name.fbk; \
	echo "✔ snapshot saved to $(FB_SNAPSHOTS_DIR)/$$name.fbk"; \
	ls -lh $(FB_SNAPSHOTS_DIR)/$$name.fbk

fb-snapshot-list: ## List available Firebird snapshots
	@if [ ! -d $(FB_SNAPSHOTS_DIR) ] || [ -z "$$(ls -A $(FB_SNAPSHOTS_DIR) 2>/dev/null)" ]; then \
		echo "(no snapshots in $(FB_SNAPSHOTS_DIR))"; \
	else \
		ls -lht $(FB_SNAPSHOTS_DIR)/ | grep -v '^total'; \
	fi

fb-restore: ## Restore from a snapshot. NAME=foo (default: latest). DESTRUCTIVE — replaces current DB.
	@name="$${NAME:-}"; \
	if [ -z "$$name" ]; then \
		name=$$(ls -t $(FB_SNAPSHOTS_DIR)/*.fbk 2>/dev/null | head -1 | xargs -n1 basename | sed 's/\.fbk$$//'); \
		[ -n "$$name" ] || (echo "❌ no snapshots found in $(FB_SNAPSHOTS_DIR)" && exit 1); \
		echo "ℹ no NAME given; using latest: $$name"; \
	fi; \
	src=$(FB_SNAPSHOTS_DIR)/$$name.fbk; \
	[ -f "$$src" ] || (echo "❌ snapshot file not found: $$src" && echo "Available:" && ls $(FB_SNAPSHOTS_DIR)/ 2>/dev/null && exit 1); \
	echo "⚠  RESTORE WILL REPLACE $(FB_DATABASE) WITH $$src"; \
	echo "   Current DB will be wiped. Anything written since the snapshot is gone."; \
	echo ""; \
	read -p "Type 'restore' to confirm: " confirm; \
	[ "$$confirm" = "restore" ] || (echo "aborted" && exit 1); \
	echo "▶ copying $$src into container"; \
	docker cp "$$src" $(FB_CONTAINER):/tmp/restore.fbk \
		|| (echo "❌ docker cp failed" && exit 1); \
	echo "▶ shutting down DB connections (gfix shutdown full -force 30)"; \
	docker exec $(FB_CONTAINER) /usr/local/firebird/bin/gfix \
		-user $(FB_USER) -password $(FB_PASSWORD) \
		-shut full -force 30 $(FB_DATABASE) \
		|| (echo "⚠  gfix shutdown failed — proceeding anyway"; true); \
	echo "▶ restoring (this takes ~1-2 min for 3.6GB)"; \
	docker exec $(FB_CONTAINER) /usr/local/firebird/bin/gbak \
		-c -v -user $(FB_USER) -password $(FB_PASSWORD) \
		-replace_database \
		/tmp/restore.fbk $(FB_DATABASE) \
		|| (echo "❌ gbak restore failed — DB may be in inconsistent state" && exit 1); \
	docker exec $(FB_CONTAINER) /usr/local/firebird/bin/gfix \
		-user $(FB_USER) -password $(FB_PASSWORD) \
		-online normal $(FB_DATABASE) || true; \
	docker exec $(FB_CONTAINER) rm -f /tmp/restore.fbk; \
	echo "✔ restored from $$src"

fb-snapshot-delete: ## Delete a snapshot. NAME=foo (required).
	@[ -n "$(NAME)" ] || (echo "❌ NAME required: make fb-snapshot-delete NAME=snapshot_20260513_120000" && exit 1)
	@f=$(FB_SNAPSHOTS_DIR)/$(NAME).fbk; \
	[ -f "$$f" ] || (echo "❌ not found: $$f" && exit 1); \
	rm -f "$$f"; \
	echo "✔ deleted $$f"

# Apply seed admin (requires seeds-firebird/000001_admin_user.sql to exist — see .example)
fb-seed-admin: ## Apply admin seed file (must be created from the .example template)
	@[ -f $(FB_SEEDS_DIR)/000001_admin_user.sql ] || \
		(echo "❌ Missing $(FB_SEEDS_DIR)/000001_admin_user.sql — copy from .example and fill in values" && exit 1)
	docker exec -i $(FB_CONTAINER) /usr/local/firebird/bin/isql \
		-u $(FB_USER) -p $(FB_PASSWORD) -ch UTF8 $(FB_DATABASE) < $(FB_SEEDS_DIR)/000001_admin_user.sql

# ── Firebird integration tests (against the real Microsip dev DB) ───
# Tests connect to the same Firebird container the API uses in dev
# (`mueblera-firebird`) via the FB_* env vars from `.env`. There is no
# separate test database — the Microsip schema can't be recreated from
# scratch, so test writes MUST live inside a RunInTx that rolls back
# (see fbtestutil.WithTestTransaction). No DDL allowed.

test-firebird: ## Run platform Firebird integration tests against the dev Microsip DB (requires FB_DATABASE)
	@[ -n "$(FB_DATABASE)" ] || (echo "❌ FB_DATABASE not set — start mueblera-firebird and source .env first" && exit 1)
	$(GO) test ./internal/platform/firebird/... ./internal/platform/fbtestutil/... -race -count=1 -timeout 120s

test-firebird-all: ## Run ALL Firebird-backed tests including module repos (auth + future modules)
	@[ -n "$(FB_DATABASE)" ] || (echo "❌ FB_DATABASE not set — start mueblera-firebird and source .env first" && exit 1)
	$(GO) test ./internal/platform/firebird/... ./internal/platform/fbtestutil/... \
	          ./internal/auth/infra/firebird/... \
	          -race -count=1 -timeout 180s

coverage-auth: ## Generate per-package coverage report for the auth module (short mode)
	$(GO) test ./internal/auth/... -count=1 -short -coverprofile=coverage-auth.out -covermode=atomic
	$(GO) tool cover -func=coverage-auth.out | tail -20
	$(GO) tool cover -html=coverage-auth.out -o coverage-auth.html
	@echo "✔ Coverage report: coverage-auth.html"

coverage-auth-full: ## Generate auth coverage INCLUDING Firebird integration tests (requires FB_DATABASE)
	@[ -n "$(FB_DATABASE)" ] || (echo "❌ FB_DATABASE not set — start mueblera-firebird and source .env first" && exit 1)
	$(GO) test ./internal/auth/... -count=1 -coverprofile=coverage-auth.out -covermode=atomic -timeout 180s
	$(GO) tool cover -func=coverage-auth.out | tail -20
	$(GO) tool cover -html=coverage-auth.out -o coverage-auth.html
	@echo "✔ Full coverage report: coverage-auth.html"

# ── Mutation testing (gremlins) ──────────────────────────────────────
# Slow. Run on demand, not on every PR. Targets focus on packages with
# high business value (domain, app) where false negatives in tests are
# most costly. Config in .gremlins.yaml. Install with:
#   go install github.com/go-gremlins/gremlins/cmd/gremlins@latest
test-mutation: test-mutation-domain test-mutation-app test-mutation-ventas-domain test-mutation-ventas-app test-mutation-httpdispatch ## Run mutation testing on critical packages

test-mutation-domain: ## Run mutation testing on auth/domain only
	gremlins unleash ./internal/auth/domain

test-mutation-app: ## Run mutation testing on auth/app only
	gremlins unleash ./internal/auth/app

test-mutation-ventas: test-mutation-ventas-domain test-mutation-ventas-app ## Run mutation testing on the ventas module (domain + app)

test-mutation-ventas-domain: ## Run mutation testing on ventas/domain only
	gremlins unleash ./internal/ventas/domain

test-mutation-ventas-app: ## Run mutation testing on ventas/app only
	gremlins unleash ./internal/ventas/app

test-mutation-httpdispatch: ## Run mutation testing on platform/httpdispatch
	gremlins unleash ./internal/platform/httpdispatch

# ── Clean ────────────────────────────────────────────────────────────
clean: ## Remove build artifacts
	rm -rf bin/ tmp/ dist/ coverage.out coverage.html

# ── CI helpers ───────────────────────────────────────────────────────
ci-check: vet lint test-unit ## Run checks expected to pass in CI

# ── Local Postgres via Docker Compose (dev only) ─────────────────────
# Definition lives in compose.yml. We block on the healthcheck via --wait
# so callers can pipe straight into migrate-up without sleeping.
db-up: ## Start local Postgres (compose, blocks until healthy)
	docker compose up -d --wait postgres

db-down: ## Stop local Postgres (KEEPS data volume)
	docker compose down

db-down-volume: ## Stop local Postgres AND wipe its data volume
	docker compose down --volumes

db-logs: ## Tail local Postgres logs
	docker compose logs -f postgres

db-shell: ## psql shell into local Postgres
	docker compose exec postgres psql -U $(DB_USER) -d $(DB_NAME)

db-reset: db-down-volume db-up ## Recreate local Postgres from scratch (WIPES data)
	@$(MAKE) migrate-up

# ── Postgres for integration tests (separate container, port 5499) ───
# `make test-integration` automatically calls `db-test-up` and exports
# TEST_DATABASE_URL, so every package reuses ONE Postgres instead of each
# spawning its own testcontainers Postgres. Use `db-test-reset` after
# changing migrations to wipe the template DB; `db-test-down` to free the
# port. Running `go test` directly without `make` falls back to
# testcontainers (per-process), so IDE/ad-hoc runs still work.
TEST_DB_PORT     ?= 5499
TEST_DB_USER     ?= test
TEST_DB_PASS     ?= test
TEST_DB_NAME     ?= msp_template
TEST_DATABASE_URL := postgres://$(TEST_DB_USER):$(TEST_DB_PASS)@localhost:$(TEST_DB_PORT)/$(TEST_DB_NAME)?sslmode=disable

db-test-up: ## Ensure integration-test Postgres is running on :5499 (idempotent: starts if down, applies pending migrations, marks template)
	@if ! docker ps --format '{{.Names}}' | grep -q '^msp-postgres-test$$'; then \
		docker rm -f msp-postgres-test >/dev/null 2>&1 || true; \
		docker run -d --name msp-postgres-test \
			-e POSTGRES_USER=$(TEST_DB_USER) -e POSTGRES_PASSWORD=$(TEST_DB_PASS) -e POSTGRES_DB=$(TEST_DB_NAME) \
			-p $(TEST_DB_PORT):5432 \
			--restart unless-stopped \
			postgres:17-alpine >/dev/null; \
		echo "⏳ Waiting for Postgres to accept connections..."; \
		for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
			docker exec msp-postgres-test pg_isready -U $(TEST_DB_USER) -d $(TEST_DB_NAME) >/dev/null 2>&1 && break; \
			sleep 1; \
		done; \
	fi
	@migrate -path $(MIGRATIONS_DIR) -database "$(TEST_DATABASE_URL)" up >/dev/null 2>&1 || \
		migrate -path $(MIGRATIONS_DIR) -database "$(TEST_DATABASE_URL)" up
	@docker exec -e PGPASSWORD=$(TEST_DB_PASS) msp-postgres-test \
		psql -U $(TEST_DB_USER) -d postgres -c "ALTER DATABASE $(TEST_DB_NAME) IS_TEMPLATE true" >/dev/null 2>&1 || true
	@echo "✔ Test Postgres ready on :$(TEST_DB_PORT) (DSN: $(TEST_DATABASE_URL))"

db-test-down: ## Stop and remove the integration-test Postgres container
	docker rm -f msp-postgres-test 2>/dev/null || true

db-test-reset: db-test-down db-test-up ## Forcibly recreate the integration-test Postgres (wipes template DB)

db-test-prune: ## Drop leftover test_* DBs inside msp-postgres-test (defensive sweep)
	@docker exec -e PGPASSWORD=$(TEST_DB_PASS) msp-postgres-test \
		psql -U $(TEST_DB_USER) -d postgres -At -c \
		"SELECT datname FROM pg_database WHERE datname LIKE 'test%'" 2>/dev/null \
		| while read db; do \
			[ -z "$$db" ] && continue; \
			docker exec -e PGPASSWORD=$(TEST_DB_PASS) msp-postgres-test \
				psql -U $(TEST_DB_USER) -d postgres -c "DROP DATABASE IF EXISTS \"$$db\" WITH (FORCE)" >/dev/null; \
			echo "  dropped $$db"; \
		done
	@echo "✔ Pruned leftover test_* DBs"

db-test-url: ## Print TEST_DATABASE_URL for the integration-test container
	@echo "$(TEST_DATABASE_URL)"

# ── Firebase Auth emulator (dev-only; for integration tests) ─────
fb-emu-up: ## Levanta el Firebase Auth emulator en localhost:9099 (Docker).
	@docker compose -f docker/firebase-emulator.yml up -d --build
	@echo ""
	@echo "Firebase Auth emulator listening on http://localhost:9099"
	@echo "Run integration tests with:"
	@echo "  FIREBASE_AUTH_EMULATOR_HOST=localhost:9099 go test -count=1 -v -run Integration ./internal/auth/infra/firebase/..."

fb-emu-down: ## Tumba el Firebase Auth emulator.
	@docker compose -f docker/firebase-emulator.yml down -v

fb-emu-logs: ## Sigue los logs del Firebase Auth emulator.
	@docker compose -f docker/firebase-emulator.yml logs -f
