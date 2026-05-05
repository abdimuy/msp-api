# Integration Tests — msp-api

> Strategy, conventions, and copy-paste templates for integration tests
> against Postgres (and, eventually, Firebird) in this project.

---

## Goal

Run all integration tests in seconds, in parallel, with no manual cleanup.
Same DB engine, same migrations, same SQL the production binary uses.

Targets:

| Metric | Target |
|---|---|
| Container startup | ~2 s (once per `go test` invocation) |
| Migrations | ~1 s (once, into the template DB) |
| DB copy per test package | ~50 ms (`CREATE DATABASE … TEMPLATE`) |
| TX rollback per test | <1 ms |
| 50 tests across 5 packages | ~4–5 s wall time |

---

## Architecture — two-layer isolation

```
testcontainers Postgres 17                       (~2 s, once)
   │
   └── msp_template  (all migrations applied)    (~1 s, once)
          │
          ├── test_pkg_xxx  (TEMPLATE copy)      (~50 ms)
          │     ├── TestCreate     (TX rollback)
          │     ├── TestGetByID    (TX rollback)
          │     └── TestUpdate_Lock (real commit)
          │     → t.Cleanup → DROP DATABASE test_pkg_xxx
          │
          └── test_pkg_yyy  (TEMPLATE copy)
                ├── TestCreate     (TX rollback)
                └── TestList       (TX rollback)
```

- **Layer 1**: each test *package* gets its own DB by `CREATE DATABASE … TEMPLATE msp_template`. PostgreSQL copies data files at filesystem level — schema and seed data already inside.
- **Layer 2**: each *test* runs inside a transaction that rolls back at the end. No cleanup SQL. Tests within a package run in parallel without contention.

This works because:

1. Migrations cost is paid once.
2. Filesystem copy is constant-time vs. SQL replay.
3. TX isolation gives free per-test cleanup.
4. Each package gets its own database, so even tests that need real `COMMIT` don't leak across packages.

---

## When to use each pattern

| Scenario | Helper | Why |
|---|---|---|
| Plain CRUD repo tests | `WithTestTransaction` | Default. Fast, auto-cleanup. |
| Optimistic locking (version conflict) | `WithTestCommit` | Conflict needs two real commits. |
| Unique constraint violations | `WithTestCommit` | Deferred constraints fire at COMMIT. |
| Outbox dispatcher tests | `WithTestCommit` | `SELECT … FOR UPDATE SKIP LOCKED` reads committed rows. |
| Idempotency `Store` tests | `WithTestTransaction` | Just CRUD on `idempotency_keys`. |
| Multi-step pipelines that cross TX boundaries | DB-per-test (`NewTestDatabase` directly) | Pipeline starts/ends its own TXs. |
| Code that uses `pool.QueryRow` bypassing context TX | DB-per-test | Pool ignores `txKey` injection. |
| HTTP handler tests | `WithTestTransaction` | Handler service uses `transaction.Manager`. |

Default is `WithTestTransaction`. Reach for `WithTestCommit` only when you have a concrete reason.

---

## Where files live

```
internal/{module}/
├── domain/                  unit tests only — no DB
├── app/services/            unit tests with mock ports
├── infra/postgres/
│   ├── {entity}_repo.go
│   ├── {entity}_repo_test.go     ← integration test, has TestMain
│   └── ...
└── infra/http/
    ├── {entity}_handler.go
    ├── {entity}_handler_test.go  ← integration test, has TestMain
    └── ...

internal/platform/testutil/
├── testdb.go                ← NewTestDatabase, container + template
├── testtx.go                ← WithTestTransaction, WithTestCommit
├── migrations.go            ← runMigrations against template
└── factories.go             ← test-data factories per entity
```

`{filename}_test.go` always lives next to the file it tests.

---

## Gating — env var, never build tags

Build tags hide compilation errors in gated files and require non-standard
`go test` flags. We gate by env var so the file always compiles and the
test simply *skips* when the env is missing.

```go
// internal/{module}/infra/postgres/cliente_repo_test.go
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
        fmt.Println("skipping integration tests: set INTEGRATION=1 to run them")
        os.Exit(0)
    }
    testPool = testutil.NewTestDatabasePool()
    os.Exit(m.Run())
}
```

Run integration tests:

```bash
INTEGRATION=1 go test ./... -count=1
# or
make test-integration
```

Run only unit tests (the default `make test`):

```bash
go test -short ./...
```

> **Why a `Pool` flavor of `NewTestDatabase`?** Integration tests usually need a pool that survives across many sub-tests in the same package, not a per-test pool. `NewTestDatabasePool()` returns a pool tied to `TestMain`'s lifetime; `NewTestDatabase(t)` returns one tied to a single test (used in DB-per-test cases).

---

## `testutil/testdb.go` — Layer 1

```go
// Package testutil provides shared helpers for integration tests.
package testutil

import (
    "context"
    "fmt"
    "os"
    "regexp"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"
)

const (
    templateDBName = "msp_template"
    pgUser         = "test"
    pgPassword     = "test"
)

var (
    templateOnce sync.Once
    adminDSN     string // DSN to the template DB (admin privileges)
    templateErr  error  // captured by templateOnce so subsequent calls return it
)

// ensureTemplate boots the Postgres container and runs the full migrations
// once per `go test` invocation. Subsequent callers reuse the same template DB.
func ensureTemplate() error {
    templateOnce.Do(func() {
        ctx := context.Background()

        container, err := postgres.Run(ctx, "postgres:17-alpine",
            postgres.WithDatabase(templateDBName),
            postgres.WithUsername(pgUser),
            postgres.WithPassword(pgPassword),
            testcontainers.WithWaitStrategy(
                wait.ForLog("database system is ready to accept connections").
                    WithOccurrence(2).
                    WithStartupTimeout(60*time.Second),
            ),
        )
        if err != nil {
            templateErr = fmt.Errorf("testutil: start postgres: %w", err)
            return
        }
        // Container is reaped automatically by the testcontainers reaper at
        // the end of the test process.

        dsn, err := container.ConnectionString(ctx, "sslmode=disable")
        if err != nil {
            templateErr = fmt.Errorf("testutil: connection string: %w", err)
            return
        }
        adminDSN = dsn

        if err := runMigrations(ctx, dsn); err != nil {
            templateErr = fmt.Errorf("testutil: run migrations: %w", err)
            return
        }
    })
    return templateErr
}

// NewTestDatabasePool creates an isolated DB for the calling test package
// and returns a pool to it. Intended to be called once from TestMain.
//
// The DB is dropped at process exit (deferred via the testcontainers reaper
// when it kills the container). Per-package isolation is permanent for the
// duration of the test run.
func NewTestDatabasePool() *pgxpool.Pool {
    if err := ensureTemplate(); err != nil {
        panic(err)
    }
    return createDBFromTemplate(packageDBName())
}

// NewTestDatabase creates an isolated DB scoped to a single test (preferred
// for the DB-per-test pattern). The DB is dropped automatically via t.Cleanup.
func NewTestDatabase(t testing.TB) *pgxpool.Pool {
    t.Helper()
    if err := ensureTemplate(); err != nil {
        t.Fatalf("ensure template: %v", err)
    }

    name := sanitize(fmt.Sprintf("test_%s_%d", t.Name(), time.Now().UnixNano()))
    pool := createDBFromTemplate(name)

    t.Cleanup(func() {
        pool.Close()
        adminPool, err := pgxpool.New(context.Background(), adminDSN)
        if err != nil {
            return
        }
        defer adminPool.Close()
        _, _ = adminPool.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name)
    })

    return pool
}

// createDBFromTemplate runs CREATE DATABASE TEMPLATE and returns a pool to it.
func createDBFromTemplate(name string) *pgxpool.Pool {
    ctx := context.Background()

    adminPool, err := pgxpool.New(ctx, adminDSN)
    if err != nil {
        panic(fmt.Errorf("testutil: connect admin: %w", err))
    }
    defer adminPool.Close()

    if _, err := adminPool.Exec(ctx,
        fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, templateDBName),
    ); err != nil {
        panic(fmt.Errorf("testutil: create db %s: %w", name, err))
    }

    pool, err := pgxpool.New(ctx, replaceDBName(adminDSN, name))
    if err != nil {
        panic(fmt.Errorf("testutil: open db %s: %w", name, err))
    }
    return pool
}

// packageDBName builds a stable name based on the test binary path so
// each package's TestMain gets the same DB across re-runs. We don't need
// uniqueness across packages because each package runs in its own process
// when using `go test ./...`.
func packageDBName() string {
    exe, _ := os.Executable()
    base := strings.NewReplacer("/", "_", ".", "_", "-", "_").Replace(exe)
    return sanitize(fmt.Sprintf("test_%s_%d", base, time.Now().UnixNano()))
}

var sanitizer = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sanitize(s string) string {
    out := sanitizer.ReplaceAllString(s, "_")
    if len(out) > 60 {
        out = out[:60]
    }
    return strings.ToLower(out)
}

// replaceDBName swaps the DB segment of a postgres DSN.
func replaceDBName(dsn, name string) string {
    // dsn looks like: postgres://user:pass@host:port/dbname?sslmode=disable
    qIdx := strings.Index(dsn, "?")
    query := ""
    if qIdx >= 0 {
        query = dsn[qIdx:]
        dsn = dsn[:qIdx]
    }
    slashIdx := strings.LastIndex(dsn, "/")
    return dsn[:slashIdx+1] + name + query
}
```

---

## `testutil/migrations.go` — apply migrations to the template

```go
package testutil

import (
    "context"
    "fmt"

    "github.com/golang-migrate/migrate/v4"
    _ "github.com/golang-migrate/migrate/v4/database/postgres"
    _ "github.com/golang-migrate/migrate/v4/source/file"
)

// runMigrations applies every migration in ./migrations/ to the given DSN.
// Tests find the migrations directory by walking up from the current package
// to the repo root.
func runMigrations(_ context.Context, dsn string) error {
    path, err := findMigrationsDir()
    if err != nil {
        return err
    }
    m, err := migrate.New("file://"+path, dsn)
    if err != nil {
        return fmt.Errorf("migrate.New: %w", err)
    }
    defer m.Close()
    if err := m.Up(); err != nil && err != migrate.ErrNoChange {
        return fmt.Errorf("migrate.Up: %w", err)
    }
    return nil
}

// findMigrationsDir walks up from the working directory looking for the
// `migrations` folder at the repo root.
func findMigrationsDir() (string, error) {
    // implementation: start at os.Getwd, climb until ".git" or "go.mod" sibling.
    // Returns the absolute path to the `migrations/` directory or an error.
    // Kept small enough that a fresh clone runs `go test` from anywhere.
    panic("TODO: walk up to repo root")
}
```

> The `findMigrationsDir` body is small (~15 LOC) — walk up checking for a sibling `go.mod`. We'll fill it in when we create the file.

---

## `testutil/testtx.go` — Layer 2

This file plugs into our existing `internal/platform/transaction/` package.
We expose a tiny test-only injector inside that package so tests don't have
to know about `txKey{}`.

```go
// internal/platform/transaction/testing.go
package transaction

import (
    "context"

    "github.com/jackc/pgx/v5"
)

// InjectTx exposes the unexported tx context key to the testutil package.
// Production code never calls this — it's a test seam.
func InjectTx(ctx context.Context, tx pgx.Tx) context.Context {
    return context.WithValue(ctx, txKey{}, tx)
}
```

```go
// internal/platform/testutil/testtx.go
package testutil

import (
    "context"
    "testing"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/abdimuy/msp-api/internal/platform/transaction"
)

// WithTestTransaction runs fn inside a transaction that always rolls back.
// Repos retrieve the active tx via transaction.GetQuerier(ctx, fallback),
// so this is the sweet spot for fast, isolated tests.
func WithTestTransaction(t testing.TB, pool *pgxpool.Pool, fn func(ctx context.Context)) {
    t.Helper()
    ctx := context.Background()

    tx, err := pool.Begin(ctx)
    if err != nil {
        t.Fatalf("begin tx: %v", err)
    }
    defer func() { _ = tx.Rollback(ctx) }() // always rollback in tests

    fn(transaction.InjectTx(ctx, tx))
}

// WithTestCommit runs fn with a plain context — no auto-rollback. The
// per-package DB is dropped after the run, so committed rows do not leak
// across packages, but they DO leak across tests in the same package.
//
// Use only for: optimistic locking, unique constraint violations, outbox
// dispatcher, transaction-boundary tests. Otherwise prefer WithTestTransaction.
func WithTestCommit(t testing.TB, _ *pgxpool.Pool, fn func(ctx context.Context)) {
    t.Helper()
    fn(context.Background())
}

// Pool returns the package-level pool for ad-hoc use (e.g., seeding helpers
// that need to run outside a TX). Most tests should not need this.
func Pool(p *pgxpool.Pool) pgx.Tx {
    panic("not implemented — placeholder to remind us we expose a pool, not a tx")
}
```

---

## `testutil/factories.go` — test data with sensible defaults

When the first entity ships, factories live here:

```go
// internal/platform/testutil/factories.go
package testutil

import (
    "testing"

    "github.com/google/uuid"
    "github.com/stretchr/testify/require"

    "github.com/abdimuy/msp-api/internal/rutas/domain"
)

type rutaConfig struct {
    Nombre     string
    VendedorID uuid.UUID
    CreatedBy  uuid.UUID
}

// RutaOption mutates a rutaConfig — used as a functional option.
type RutaOption func(*rutaConfig)

// WithRutaNombre sets the ruta name.
func WithRutaNombre(s string) RutaOption {
    return func(c *rutaConfig) { c.Nombre = s }
}

// WithRutaVendedorID sets the vendedor.
func WithRutaVendedorID(id uuid.UUID) RutaOption {
    return func(c *rutaConfig) { c.VendedorID = id }
}

// NewTestRuta builds a valid Ruta with sensible defaults. Override only what
// the test cares about.
func NewTestRuta(t testing.TB, opts ...RutaOption) *domain.Ruta {
    t.Helper()
    cfg := rutaConfig{
        Nombre:     "Ruta Centro",
        VendedorID: uuid.New(),
        CreatedBy:  uuid.New(),
    }
    for _, opt := range opts {
        opt(&cfg)
    }
    r, err := domain.NewRuta(cfg.Nombre, cfg.VendedorID, cfg.CreatedBy)
    require.NoError(t, err)
    return r
}
```

Rules for factories:
- Always live in `testutil/factories.go` (not duplicated per package).
- `t.Helper()` first line.
- Defaults must produce a valid entity (no required field empty).
- Functional options for overrides — never positional args.
- One factory per entity. Name: `NewTest{Entity}`.

---

## `TestMain` template — copy-paste

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
        fmt.Println("skipping integration tests: set INTEGRATION=1 to run them")
        os.Exit(0)
    }
    testPool = testutil.NewTestDatabasePool()
    os.Exit(m.Run())
}
```

One `TestMain` per package that has integration tests. `testPool` is the
template-copied DB shared by every test in the package.

---

## Repo integration test — pattern

```go
package postgres_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/abdimuy/msp-api/internal/platform/testutil"
    "github.com/abdimuy/msp-api/internal/rutas/infra/postgres"
)

func TestPostgresRutaRepository_CreateAndGet(t *testing.T) {
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

// Optimistic locking needs real commits — second update sees the first commit's version bump.
func TestPostgresRutaRepository_Update_VersionConflict(t *testing.T) {
    t.Parallel()
    testutil.WithTestCommit(t, testPool, func(ctx context.Context) {
        repo := postgres.NewRutaRepository(testPool)
        ruta := testutil.NewTestRuta(t)
        require.NoError(t, repo.Create(ctx, ruta))

        stale := *ruta // snapshot before the bump

        require.NoError(t, ruta.Update("Ruta Norte", ruta.VendedorID(), uuid.New()))
        require.NoError(t, repo.Update(ctx, ruta))

        require.NoError(t, stale.Update("Ruta Sur", stale.VendedorID(), uuid.New()))
        err := repo.Update(ctx, &stale)

        require.ErrorIs(t, err, domain.ErrRutaConcurrentModification)
    })
}
```

---

## HTTP handler integration test — pattern

```go
package http_test

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/abdimuy/msp-api/internal/platform/testutil"
    "github.com/abdimuy/msp-api/internal/platform/transaction"
    rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
    rutashttp "github.com/abdimuy/msp-api/internal/rutas/infra/http"
    rutaspg "github.com/abdimuy/msp-api/internal/rutas/infra/postgres"
)

func TestCreateRutaEndpoint_Created(t *testing.T) {
    t.Parallel()
    testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
        repo := rutaspg.NewRutaRepository(testPool)
        svc := rutasapp.NewService(repo, transaction.NewManager(testPool))
        h := rutashttp.NewHandler(svc)

        body := `{"nombre":"Centro","vendedor_id":"` + uuid.NewString() + `"}`
        req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v2/rutas", strings.NewReader(body))
        rec := httptest.NewRecorder()

        h.Create(rec, req)

        require.Equal(t, http.StatusCreated, rec.Code)
        var resp rutashttp.RutaDTO
        require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
        assert.Equal(t, "Centro", resp.Nombre)
    })
}
```

Note: handler receives `req.WithContext(ctx)` so the active TX flows
into the service → repo chain.

---

## Outbox-specific testing

The outbox dispatcher reads with `SELECT … FOR UPDATE SKIP LOCKED` and
needs **committed** rows. Two strategies:

### A) Test the dispatcher in isolation

Boot the dispatcher in a test, insert events with `WithTestCommit`, assert
they were processed.

```go
func TestDispatcher_ProcessesPendingEvent(t *testing.T) {
    t.Parallel()
    testutil.WithTestCommit(t, testPool, func(ctx context.Context) {
        registry := outbox.NewHandlerRegistry()

        var seen outbox.Event
        registry.Register(testHandler{
            eventType: "ping",
            handle: func(_ context.Context, e outbox.Event) error {
                seen = e
                return nil
            },
        })

        d := outbox.NewDispatcher(testPool, registry, outbox.DispatcherConfig{
            PollInterval: 50 * time.Millisecond,
            BatchSize:    10,
            MaxAttempts:  3,
            LockTimeout:  2 * time.Second,
        })
        require.NoError(t, d.Start(ctx))
        t.Cleanup(func() {
            stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
            defer cancel()
            _ = d.Stop(stopCtx)
        })

        // Insert an event in its own committed tx so the dispatcher can see it.
        mgr := transaction.NewManager(testPool)
        require.NoError(t, mgr.RunInTx(ctx, func(ctx context.Context) error {
            return outbox.Enqueue(ctx, "test", uuid.New(), "ping", map[string]any{"a": 1})
        }))

        require.Eventually(t, func() bool { return seen.EventType == "ping" },
            2*time.Second, 50*time.Millisecond)
    })
}
```

### B) Test the dispatcher's `tick` directly

For deterministic tests of retry / dead-letter / mismatched-event-type
logic, prefer calling `tick` directly (export it via test-only file or
a private helper). Avoids polling races.

> **Rule of thumb**: end-to-end behavior → strategy A; deterministic
> branch coverage → strategy B.

---

## Idempotency `Store` testing

The Postgres-backed `idempotency.Store` is a thin UPSERT — `WithTestTransaction`
is enough.

```go
func TestPostgresIdempotencyStore_RoundTrip(t *testing.T) {
    t.Parallel()
    testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
        store := idempotencypg.NewStore(testPool)

        require.NoError(t, store.Save(ctx, idempotency.Record{
            Key:            "k1",
            Method:         http.MethodPost,
            Path:           "/v2/cobros",
            RequestHash:    "deadbeef",
            ResponseStatus: http.StatusCreated,
            ResponseBody:   []byte(`{"ok":true}`),
            ExpiresAt:      time.Now().Add(time.Hour),
        }))

        got, err := store.Get(ctx, "k1")
        require.NoError(t, err)
        require.NotNil(t, got)
        assert.Equal(t, http.StatusCreated, got.ResponseStatus)
    })
}
```

---

## Firebird mirror tests (when modules with Microsip ship)

Firebird is harder to virtualize than Postgres:

- `nakagami/firebirdsql` works, but there is no first-class testcontainers
  module for Firebird.
- The image `jacobalberty/firebird:v4.0` runs in Docker but lacks the
  `MUEBLERA.FDB` schema we mirror.

Strategy: tests that exercise the Firebird boundary live in their own
package and gate on a **separate** env var:

```go
func TestMain(m *testing.M) {
    if os.Getenv("FIREBIRD") == "" {
        fmt.Println("skipping firebird tests: set FIREBIRD=1 (requires running container)")
        os.Exit(0)
    }
    fbPool = firebird.OpenForTest()
    os.Exit(m.Run())
}
```

Day-to-day CI runs only `INTEGRATION=1`. A nightly job can run
`INTEGRATION=1 FIREBIRD=1` against a fixture Firebird with a stripped
Microsip schema.

We design Firebird mirror code so it can be exercised against an
in-memory fake (just Postgres with a parallel `mirror_*` table — same
shape, no Firebird involved) for the bulk of tests. The Firebird-bound
tests verify only the connector and SQL.

---

## Reliability and concurrency cases worth testing

- Timeout / cancellation propagation across DB calls.
- Retry only on transient errors (use `failsafe-go` policies).
- Idempotency replay: same key + same body → same response.
- Optimistic locking: stale write returns the right sentinel error.
- Outbox at-least-once: handler invoked twice for same event must be a no-op.
- `SELECT … FOR UPDATE SKIP LOCKED`: two parallel dispatchers do not see
  the same row.

---

## Performance — what to expect

| Scenario | Time |
|---|---|
| Cold start: container + migrations + first DB | ~3.5 s |
| Per-package DB copy | ~50 ms |
| Per-test TX rollback | <1 ms |
| Per-test commit + DB drop (DB-per-test) | ~50 ms |
| Total: 50 tests across 5 packages | ~4–5 s |
| Total: 200 tests across 10 packages | ~10–12 s |

If a single package's tests take >2 s combined, look for:

- Tests using `WithTestCommit` when `WithTestTransaction` would do.
- Network calls in tests (probably should be unit tests with mocks).
- `time.Sleep` (forbidden — use `require.Eventually` or channels).

---

## Hard rules

1. **`t.Parallel()` on every integration test.** No exceptions.
2. **`t.Helper()` on every helper.**
3. **`require` for setup, `assert` for results.** `require.NoError(t, err)` before exercising the system; `assert.Equal(t, want, got)` after.
4. **`require.Eventually` instead of `time.Sleep`.** No ad-hoc sleeps.
5. **No mocking libraries.** Mocks are structs with function fields.
6. **Default to `WithTestTransaction`.** Reach for `WithTestCommit` only with a written reason in the test comment.
7. **Env var gating in `TestMain`.** Never `//go:build integration`.
8. **One `TestMain` per integration test package.** Single `testPool` shared with the package's tests.
9. **Factories go in `testutil/factories.go`.** Functional options, never positional.
10. **No DB defaults / triggers in test schemas either.** The same rule as production: timestamps, IDs, audit fields all set in Go (see `CLAUDE.md`).
11. **`t.Cleanup` for resource teardown.** Never `defer` cleanup in tests — `t.Cleanup` runs even on `t.FailNow`.
12. **Test names: `Test{Function}_{Scenario}`.** Examples: `TestPostgresRutaRepository_Create_DuplicateNombre`, `TestCreateRutaEndpoint_ValidationFails`.

---

## Make targets

Already in place; documented here for reference:

```makefile
test-integration:    ## Run all integration tests
	INTEGRATION=1 go test ./... -race -count=1 -timeout 600s

test-all:            ## Unit + integration
	INTEGRATION=1 go test ./... -race -count=1 -timeout 600s
```

CI runs `make test` (unit) on every PR and `make test-integration` on
merge to `main`.

---

## Implementation order — when we wire this up

1. `internal/platform/transaction/testing.go` — `InjectTx` test seam.
2. `internal/platform/testutil/migrations.go` — `runMigrations` + `findMigrationsDir`.
3. `internal/platform/testutil/testdb.go` — `NewTestDatabasePool`, `NewTestDatabase`, `createDBFromTemplate`.
4. `internal/platform/testutil/testtx.go` — `WithTestTransaction`, `WithTestCommit`.
5. First module's `infra/postgres/*_test.go` — proves the pattern end-to-end.
6. `internal/platform/testutil/factories.go` — grows entity by entity.
7. CI workflow — adds `INTEGRATION=1` job.

---

## Agent checklist (per module)

Before declaring integration tests done for a module:

- [ ] Repo file has a sibling `_test.go` with `TestMain` + env gating.
- [ ] Tests cover Create, GetByID (found + not found), List, Update (success + version conflict), SoftDelete.
- [ ] Default tests use `WithTestTransaction`. Comments explain any `WithTestCommit`.
- [ ] Handlers covered for: 201 success, 400/422 validation, 404 not found, 409 conflict.
- [ ] Idempotency middleware covered: same key + same body replay; mismatch → 409.
- [ ] Outbox: at least one Enqueue test that asserts a row appears in `outbox_events`.
- [ ] Module-specific factories live in `testutil/factories.go`.
- [ ] Test names match `Test{Function}_{Scenario}`.
- [ ] `t.Parallel()` on every test; no `time.Sleep`.
- [ ] Coverage of the repo file ≥80%.
