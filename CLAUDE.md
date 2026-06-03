# CLAUDE.md — msp-api project conventions

This file is loaded by Claude Code (and any AI/agent reading the repo) on every session. It encodes hard rules and decisions that override default behavior.

## Hard rules

### 1. No logic in the database

**The database is a dummy store.** All behavior — ID generation, timestamps, defaults, validation, derived fields, computed columns — lives in Go. The schema is structural only.

> **Scope:** this rule governs our **own Postgres app schema** (`migrations/`). The **Firebird adapter** (`migrations-firebird/`) is exempt — Microsip's `MUEBLERA.FDB` is trigger-driven by construction, so the read-model caches there are materialized with triggers + procedures + `POST_EVENT` and we follow that idiom. See [ADR 0006](docs/adr/0006-firebird-adapter-trigger-rule-exemption.md) for the rationale and what is still forbidden in the Firebird adapter (business rules in triggers, large bodies, mirroring API-owned data).

Forbidden in migrations:
- `DEFAULT gen_random_uuid()`, `DEFAULT uuid_generate_v4()`, or any UUID generator default. UUIDs are always created in Go via `uuid.New()`.
- `DEFAULT now()`, `DEFAULT CURRENT_TIMESTAMP`. Timestamps are always set in Go via `time.Now()` (or carried in from a domain method like `audit.MarkUpdated()`).
- `CREATE TRIGGER` of any kind.
- `CREATE FUNCTION` / stored procedures / `LANGUAGE plpgsql`.
- `SERIAL`, `BIGSERIAL`, identity columns, sequences exposed to writes. (Sequences for internal Postgres use are fine; we just never depend on them for app logic.)
- `GENERATED ALWAYS AS (...)` columns.
- `CHECK` constraints that encode business rules. Simple structural checks (`amount >= 0`) are tolerated as guardrails but the canonical rule lives in the domain entity/VO.
- Extensions whose only purpose is to add logic (`pgcrypto` for `gen_random_uuid()`, etc.). Extensions that provide *index types* or *data types* (e.g. `btree_gin`, `uuid-ossp` for the column type only) are fine.

Allowed and encouraged in migrations:
- `PRIMARY KEY`, `UNIQUE`, `FOREIGN KEY`, `NOT NULL`, `REFERENCES`.
- Indexes (including partial, expression, GIN, BRIN).
- Column types, including `uuid`, `timestamptz`, `numeric`, `jsonb`.

Why: portability (we may swap Postgres later, even if unlikely), determinism (one source of truth in Go), testability (unit tests don't need a DB to know what an entity will look like), and AI-safety (an agent generating code can never produce a half-defined entity that "works in Postgres but not in tests").

How to apply this rule:
- Every `INSERT` statement passes `id`, `created_at`, `updated_at` (and any nullable timestamp columns) explicitly.
- Every entity's `New{Entity}` constructor in Go calls `uuid.New()` and `time.Now()`.
- Every `UPDATE` that touches `updated_at` passes the new value as a parameter, not via a SQL function.
- The outbox dispatcher passes `processed_at` / `failed_at` as parameters from Go time.

### 2. Vertical slices

Code is organized by module (`internal/{module}/`), not by layer. Domain, app, ports, infra of a single module live together. Cross-module access is **only** via the module's contracts package — never reach into another module's `domain/`, `app/`, or `infra/`. The `depguard` linter enforces this.

### 3. Code in English, user-facing messages in Spanish

- Identifiers, comments, error codes (`apperror.New*` first arg), variable names: **English, snake_case for codes, camelCase/PascalCase for Go identifiers**.
- User-facing error messages (`apperror.New*` second arg), validation messages, log messages destined for support: **Spanish, lowercase, no trailing period**.

Example:
```go
ErrClienteNombreRequired = apperror.NewValidation(
    "cliente_nombre_required",          // English code
    "el nombre del cliente es obligatorio", // Spanish message
)
```

### 4. AI safety nets

The repo is set up so an AI/agent generating code cannot easily produce broken or unsafe code:
- `golangci-lint` runs ~55 strict linters (see `.golangci.yml`).
- `depguard` blocks cross-layer and cross-module imports.
- `lefthook` pre-commit hook runs lint, vet, build, format, secrets check.
- `lefthook` pre-push hook runs the full test suite with `-race`.
- Conventional commit message format enforced by `commit-msg` hook.

If the AI bypasses these (`--no-verify`, etc.) without explicit user approval, that's a serious mistake.

### 5. Stack constraints

This API targets **Windows Server 2016 legacy** for production. That means:
- No Docker in production (Docker Desktop / Compose are dev-only).
- No Kubernetes / k3s / orchestrators.
- No external dependencies that don't ship as a Windows binary.
- Cross-compile from Mac with `GOOS=windows GOARCH=amd64 CGO_ENABLED=0`.
- Run as a Windows Service via `nssm`.

The "modern stack" version of this app is **bonanza-api** — a separate project. Don't pollute msp-api with bonanza's stack assumptions.

### 6. Blob storage is local filesystem only

Image uploads (cobranza receipts, INE photos, evidencia de venta) live on the API server's local disk under `STORAGE_DIR`. The `outbound.StorageProvider` port in each module has a single concrete implementation (`FilesystemProvider`) — no cloud backends, no selector, no stub for "future" cloud storage. See ADR-0003.

If a future module ever needs cloud storage, add a new concrete implementation alongside `FilesystemProvider` and wire it at the composition root. Do not reintroduce a configurable selector "just in case".

### 7. Everything runs locally — no remote CI/CD

This project does **not** use GitHub Actions, GitLab CI, or any other remote CI provider. The full quality gate runs on the developer's machine via lefthook hooks:

- `pre-commit`: gofmt, go vet, golangci-lint (on staged), build, secrets check, no-debug, mod tidy.
- `pre-push`: full `golangci-lint run ./...` + `go test -race -short ./...`.

Integration tests (`make test-integration`) run on demand on the developer's machine — they require Docker Desktop locally for the testcontainers Postgres. Do not write GitHub Actions workflows, do not add `.github/`, do not document CI badges.

If we ever add coverage gates, mutation testing, or scheduled benchmarks, they go into Make targets and lefthook hooks, not into a remote pipeline.

## Architecture summary

```
internal/{module}/
  domain/          ← entities + VOs + sentinel errors. ZERO imports outside stdlib + uuid + decimal + platform/{domain,apperror}.
  app/             ← services / commands / queries. Imports domain + ports.
  ports/
    inbound/       ← interfaces the module exposes to drive it (rare; usually handlers call services directly).
    outbound/      ← interfaces the module needs from outside (e.g. ClientesClient when consuming clientes module).
  infra/
    postgres/      ← repositories, sqlc-generated code wrappers.
    firebird/      ← Microsip pull/push adapters.
    http/          ← handlers + routes.
    clients/       ← implementations of outbound ports (cross-module clients).
  {module}_contracts.go         ← types this module exposes to other modules.
  {module}_contracts_mapper.go  ← entity → contract mapping. Called only from infra/clients of other modules.
```

## Entity types

| Type | Use when | Embeds |
|------|----------|--------|
| **A** CRUD | msp-only entity (rutas, ventas_locales, comisiones) | `audit.Auditable` |
| **B** Pipeline | state-machine entity (traspasos with cotización→entrega) | `audit.Timestamped` + transient `previousState`/`stateChanged` |
| **C-bidi** | mirrored Microsip entity, both directions (clientes) | `audit.Auditable` + `audit.MicrosipSync` |
| **C-push** | created locally, pushed to Microsip only (pagos) | `audit.Auditable` + `audit.MicrosipSync` |

Tables that mirror Microsip 1:1 (`mirror_*`) and pre-computed read models (`projection_*`) are **not entities**. They live in `internal/{module}/mirror/` and `internal/{module}/projection/`.

## Commit conventions

```
<type>(<scope>): <subject>
```

`type` ∈ `feat | fix | chore | docs | refactor | test | perf | build | ci | style`. Enforced by lefthook commit-msg hook.

## When building a new module

Read `docs/module-standards/MODULE_TEMPLATE.md` FIRST. Then for the specifics:

- `docs/module-standards/AGGREGATE_PATTERNS.md` — entity design (private fields, two constructors, child entities, iter.Seq, domain events).
- `docs/module-standards/CQRS_PATTERN.md` — when to split commands and queries across files.
- `docs/module-standards/HUMA_WIRING.md` — Huma + chi composition, DTO conventions, multipart.
- `docs/module-standards/DATETIME_HANDLING.md` — **OBLIGATORIO**. Cómo manejar fechas en domain/app/infra y el contrato RFC3339 UTC con el frontend. UTC en Go, `firebird.ToWallClock` al escribir, `firebird.ScanUTCTime` al leer.
- `docs/module-standards/ENCODING_HANDLING.md` — **OBLIGATORIO**. UTF-8 everywhere. Columnas `MSP_*` son `CHARACTER SET UTF8`; el adapter Microsip aísla las tablas legacy. NFC en domain, sin `firebird.Win1252` ni `EncodeWin1252` para tablas nuestras.
- `docs/module-standards/TESTING_REQUIREMENTS.md` — coverage gates, security sweep, mutation testing.

The `auth` module is the reference for simple modules (single aggregate, no children, chi + manual openapi.yaml).

The `ventas` module is the reference for complex modules (aggregate with child collections, CQRS, Huma auto-OpenAPI, blob storage).

## When in doubt

1. Read this file again.
2. Check `.golangci.yml` and `lefthook.yml` for what's enforced automatically.
3. Read the existing platform packages (`internal/platform/`) for the established patterns.
4. Ask the user before creating new patterns or breaking established ones.
