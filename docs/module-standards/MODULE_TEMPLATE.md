# Module Template

The canonical layout, build steps, and minimum quality bar for every new module in msp-api. Use this checklist when adding a new bounded context (e.g. `cobranza`, `pagos`, `traspasos`).

## Directory layout

```
internal/{module}/
  domain/                                   ← Aggregates, entities, VOs, sentinel errors, events.
  ports/
    outbound/                               ← Interfaces the module needs from outside.
  app/                                      ← Service + commands + queries.
  infra/
    {module}fb/                             ← Firebird-backed repos. Use the {module}fb suffix
                                              (e.g. authfb, ventfb) to avoid collisions with
                                              platform packages.
    {module}http/                           ← HTTP transport (Huma over chi).
    {module}outbox/                         ← Best-effort outbox enqueuer (mirrors auth pattern).
    storage/                                ← Only when blobs are needed (see ventas).
  {module}_contracts.go                     ← Re-exports + cross-module types. Only file other
                                              modules may import.
  {module}_contracts_mapper.go              ← Domain → contract projection if cross-module
                                              consumers need it.
```

The `internal/{module}` root package is the **only** import-allowed surface for other modules. depguard enforces this — see `.golangci.yml`.

## Step-by-step build checklist

1. **Migration first** (`migrations-firebird/0000NN_create_{module}_tables.up.sql`):
   - No `DEFAULT now()`, no `DEFAULT gen_random_uuid()`, no triggers, no procedures. Structural-only.
   - Replicate every business invariant as a CHECK constraint (it documents intent and gives a second line of defense).
   - Pair with `0000NN_create_{module}_tables.down.sql` for reversibility.

2. **Permissions**: add `Perm{Module}{Action}` constants in `internal/auth/domain/permission_codes.go`, register them in `AllPermissions()`, and re-export from `internal/auth/auth_contracts.go`. Update `permission_codes_test.go` to include the new codes in the expected catalog.

3. **Domain**: aggregate root + child entities (private constructors) + value objects (one file per VO type, package `domain`). See `AGGREGATE_PATTERNS.md`.

4. **Ports**: declare the outbound interfaces the app needs (`{Module}Repo`, `StorageProvider` if applicable, `Clock`, `OutboxEnqueuer`). One file per concern (`repos.go`, `storage.go`, `outbox.go`, `clock.go`).

5. **App**: `Service` struct + commands + queries split across files in the SAME package (`app/`). Subpackages do not work because methods can't span Go packages. Use file naming for organization. See `CQRS_PATTERN.md`.

6. **Infra**:
   - `{module}fb/` — `VentaRepo` + queries + rowmappers + pagination. Multi-table writes happen in one tx by using `firebird.GetQuerier(ctx, pool.DB)` which honors the ambient tx installed by `firebird.TxManager`. **Every `time.Time` passed to `q.ExecContext` must be wrapped in `firebird.ToWallClock(t)`; every `TIMESTAMP` column read must use `firebird.ScanUTCTime`. See `DATETIME_HANDLING.md`.**
   - `{module}http/` — Huma over chi. See `HUMA_WIRING.md`. **Date fields are RFC3339 UTC strings on both request and response — see `DATETIME_HANDLING.md` for the frontend contract.**
   - `{module}outbox/` — wraps `transaction.Manager` for best-effort Postgres outbox writes. Copy `internal/auth/infra/authoutbox/enqueuer.go` verbatim and change the log key + import path.
   - `storage/` (if applicable) — see `internal/ventas/infra/storage/` for the filesystem + R2-stub split.

7. **Wiring** (`cmd/api/{module}_wiring.go`):
   - One `provide{Module}X` function per port implementation.
   - Add to `fx.Provide(...)` in `cmd/api/main.go`.
   - Mount the HTTP router under `/v2` inside `provideHTTPServer` in `cmd/api/server.go`. Apply the shared `authn` + `idem` chi middlewares once at the `/v2` sub-group when the module is auth-protected.

8. **Lint rules**: add the module's package import aliases to `.golangci.yml` under `importas.alias` so `no-extra-aliases: true` keeps the aliasing consistent.

9. **Tests**: see `TESTING_REQUIREMENTS.md`. Domain ≥99%, app ≥90%, infra ≥80%, mutation kill-rate ≥80%. **Time-related tests must use `time.Date(..., time.UTC)` for fixtures and `fixedClock`, never `time.Now()` directly. See `DATETIME_HANDLING.md` test patterns.**

10. **OpenAPI**: Huma generates `/v2/openapi.json` automatically. No manual YAML.

11. **Dates and times**: read `DATETIME_HANDLING.md` before writing any code that touches `time.Time`. The three non-negotiable rules: (a) domain/app always operate in UTC; (b) `firebird.ToWallClock` wraps every write; (c) `firebird.ScanUTCTime` decodes every read. The frontend contract (RFC3339 with explicit TZ, response always UTC `Z`) is documented in the same file.

## File-suffix convention

Module HTTP/firebird package names use the `{module}` prefix + `{layer}` suffix (e.g. `authhttp`, `authfb`, `venthttp`, `ventfb`). This avoids collisions with stdlib (`http`), platform packages (`firebird`), and other modules.

When two modules need the same dependency name (e.g. both `auth/app` and `ventas/app` exist as `package app`), use Go import aliases:

```go
import (
    authapp   "github.com/abdimuy/msp-api/internal/auth/app"
    ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
)
```

Register these aliases in `.golangci.yml` under `linters-settings.importas.alias` so the linter doesn't reject them.
