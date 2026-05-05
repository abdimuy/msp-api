# Integration Tests

> Reference for agents writing or maintaining integration tests in msp-api.
> Production-shape Postgres via testcontainers; tests run in seconds.

## TL;DR

- One Postgres container per `go test` run, migrations applied to a template DB once.
- Each test package gets its own DB by `CREATE DATABASE … TEMPLATE msp_template` (~50 ms).
- Each test runs in a transaction that auto-rolls back (~1 ms).
- Gate by env var `INTEGRATION=1`. Never `//go:build` tags.
- All tests `t.Parallel()`. No `time.Sleep`. No mocking libs.

## Hard rules

1. `t.Parallel()` on every test.
2. `t.Helper()` first line of every helper.
3. `require` for setup, `assert` for results.
4. `require.Eventually` (or channels) instead of `time.Sleep`.
5. Mocks are structs with function fields. No mocking libraries.
6. Default `WithTestTransaction`. `WithTestCommit` only with a comment explaining why.
7. Env-var gating in `TestMain`. No build tags.
8. One `TestMain` per integration package; shared `testPool` package var.
9. Factories live in `internal/platform/testutil/factories.go` with functional options.
10. No DB defaults / triggers (per CLAUDE.md). Tests pass timestamps and UUIDs explicitly, same as production.
11. `t.Cleanup`, never `defer`, for resource teardown.
12. Test names: `Test{Function}_{Scenario}`.

## File layout

```
internal/{module}/
├── domain/{x}_test.go              unit, no DB
├── app/services/{x}_test.go        unit, mock ports
├── infra/postgres/{x}_repo_test.go integration (TestMain here)
└── infra/http/{x}_handler_test.go  integration (TestMain here)

internal/platform/testutil/
├── testdb.go    NewTestDatabasePool, NewTestDatabase
├── testtx.go    WithTestTransaction, WithTestCommit
├── migrations.go runMigrations
└── factories.go NewTest{Entity}, *Option
```

`{filename}_test.go` lives next to the file it tests.

## TestMain template (copy-paste)

```go
package postgres_test

import (
    "fmt"
    "os"
    "testing"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/abdimuy/msp-api/internal/platform/testutil"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
    if os.Getenv("INTEGRATION") == "" && os.Getenv("TEST_DATABASE_URL") == "" {
        fmt.Println("skipping: set INTEGRATION=1")
        os.Exit(0)
    }
    testPool = testutil.NewTestDatabasePool()
    os.Exit(m.Run())
}
```

## testutil API

| Symbol | Purpose |
|---|---|
| `NewTestDatabasePool() *pgxpool.Pool` | Boots container (once) + copies template DB. Use in `TestMain`. |
| `NewTestDatabase(t) *pgxpool.Pool` | Per-test DB, dropped via `t.Cleanup`. For pipelines that bypass context-TX. |
| `WithTestTransaction(t, pool, fn)` | Run `fn(ctx)` in a TX that always rolls back. Default for CRUD. |
| `WithTestCommit(t, pool, fn)` | Run `fn(ctx)` with no auto-rollback. For optimistic locking, unique constraints, outbox. |
| `NewTest{Entity}(t, opts...)` | Build a valid entity. Override only what matters. |

`transaction.InjectTx(ctx, tx)` is the test-only seam in `internal/platform/transaction/testing.go` that lets `WithTestTransaction` plant a tx in context.

## When to use each helper

| Scenario | Helper |
|---|---|
| CRUD tests | `WithTestTransaction` |
| Optimistic locking | `WithTestCommit` |
| Unique constraint violations | `WithTestCommit` |
| Outbox dispatcher (`SELECT … FOR UPDATE SKIP LOCKED` needs committed rows) | `WithTestCommit` |
| Idempotency `Store` | `WithTestTransaction` |
| Pipelines that start their own TXs | `NewTestDatabase` |
| Code that uses `pool.QueryRow` directly | `NewTestDatabase` |

If unsure: `WithTestTransaction`.

## Repo test pattern

```go
func TestPostgresRutaRepo_CreateAndGet(t *testing.T) {
    t.Parallel()
    testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
        repo := postgres.NewRutaRepository(testPool)
        ruta := testutil.NewTestRuta(t)

        require.NoError(t, repo.Create(ctx, ruta))

        got, err := repo.GetByID(ctx, ruta.ID())
        require.NoError(t, err)
        assert.Equal(t, ruta.Nombre(), got.Nombre())
    })
}

// Real commits: stale write must see the bumped version.
func TestPostgresRutaRepo_Update_VersionConflict(t *testing.T) {
    t.Parallel()
    testutil.WithTestCommit(t, testPool, func(ctx context.Context) {
        repo := postgres.NewRutaRepository(testPool)
        ruta := testutil.NewTestRuta(t)
        require.NoError(t, repo.Create(ctx, ruta))

        stale := *ruta
        require.NoError(t, ruta.Update("Norte", ruta.VendedorID(), uuid.New()))
        require.NoError(t, repo.Update(ctx, ruta))

        require.NoError(t, stale.Update("Sur", stale.VendedorID(), uuid.New()))
        require.ErrorIs(t, repo.Update(ctx, &stale), domain.ErrRutaConcurrentModification)
    })
}
```

## Handler test pattern

```go
func TestCreateRutaEndpoint_Created(t *testing.T) {
    t.Parallel()
    testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
        repo := pg.NewRutaRepository(testPool)
        svc := app.NewService(repo, transaction.NewManager(testPool))
        h := http.NewHandler(svc)

        body := strings.NewReader(`{"nombre":"X","vendedor_id":"` + uuid.NewString() + `"}`)
        req := httptest.NewRequestWithContext(ctx, "POST", "/v2/rutas", body)
        rec := httptest.NewRecorder()
        h.Create(rec, req)

        require.Equal(t, http.StatusCreated, rec.Code)
    })
}
```

The handler runs in the test TX because `req.WithContext(ctx)` carries the injected tx through service → repo.

## Factory pattern

```go
type rutaConfig struct {
    Nombre     string
    VendedorID uuid.UUID
    CreatedBy  uuid.UUID
}

type RutaOption func(*rutaConfig)

func WithRutaNombre(s string) RutaOption {
    return func(c *rutaConfig) { c.Nombre = s }
}

func NewTestRuta(t testing.TB, opts ...RutaOption) *domain.Ruta {
    t.Helper()
    cfg := rutaConfig{Nombre: "Centro", VendedorID: uuid.New(), CreatedBy: uuid.New()}
    for _, o := range opts {
        o(&cfg)
    }
    r, err := domain.NewRuta(cfg.Nombre, cfg.VendedorID, cfg.CreatedBy)
    require.NoError(t, err)
    return r
}
```

## Outbox tests

Two patterns; pick one per test, not both:

- **End-to-end** (with running dispatcher): `WithTestCommit`, start `outbox.NewDispatcher`, enqueue via `transaction.Manager.RunInTx`, `require.Eventually` until handler observes the event. Use sparingly — slower.
- **Direct `tick`** (deterministic): call `dispatcher.tick(ctx)` directly inside `WithTestCommit`, assert the row's `processed_at` / `failed_at` / `attempts`. Default for branch coverage (transient retry, dead-letter, missing handler).

## Idempotency Store tests

`WithTestTransaction`. Round-trip Save → Get; expired keys; UPSERT on duplicate. The middleware itself is unit-tested with the in-memory store; integration tests cover only the SQL layer.

## Firebird mirror tests (future)

Separate env var `FIREBIRD=1`. Independent `TestMain` reads `MICROSIP_TEST_DSN`. Day-to-day CI runs only `INTEGRATION=1`; a nightly job runs `INTEGRATION=1 FIREBIRD=1`. Most mirror logic is exercised against Postgres-only fakes — Firebird-bound tests verify the connector and SQL parsing only.

## Performance targets

| Step | Cost |
|---|---|
| Container startup (once) | ~2 s |
| Migrations into template (once) | ~1 s |
| Per-package DB copy | ~50 ms |
| TX rollback per test | <1 ms |
| Per-test DB drop (DB-per-test) | ~50 ms |
| 50 tests / 5 packages | ~4–5 s |

If a package's tests exceed ~2 s combined, suspect: unnecessary `WithTestCommit`, network calls, or `time.Sleep` (forbidden).

## Make targets

```
make test             unit only (-short)
make test-integration INTEGRATION=1 go test ./...
make test-all         unit + integration
```

CI runs `make test` per PR; `make test-integration` on merge to `main`.

## Agent checklist (per module)

- [ ] `_test.go` next to source; integration tests have `TestMain` + env gate.
- [ ] Repo covered: Create, GetByID (found + not found), List, Update (success + conflict), SoftDelete.
- [ ] Handler covered: 201, 422, 404, 409.
- [ ] Idempotency middleware end-to-end: replay + mismatch.
- [ ] Outbox: at least one Enqueue inserts a row.
- [ ] Factories in `testutil/factories.go`.
- [ ] All `t.Parallel()`; no `time.Sleep`.
- [ ] Repo coverage ≥ 80%.
