# Testing Requirements

Coverage thresholds, test categories, and how to run each.

## Coverage gates

| Package           | Target | How measured                                      |
|-------------------|--------|---------------------------------------------------|
| `internal/{module}/domain`            | ≥ 99% | `go test -cover ./internal/{module}/domain` |
| `internal/{module}/app`               | ≥ 90% | `go test -cover ./internal/{module}/app`    |
| `internal/{module}/infra/{module}fb`  | ≥ 80% | `go test -cover ./internal/{module}/infra/{module}fb` with `FB_DATABASE` set |
| `internal/{module}/infra/storage`     | ≥ 85% | `go test -cover ./internal/{module}/infra/storage` |
| `internal/{module}/infra/{module}http`| ≥ 70% | `go test -cover ./internal/{module}/infra/{module}http` |
| `internal/{module}/infra/{module}outbox`| ≥ 80% | unit test with a fake transaction runner |
| **Mutation kill-rate** (domain + app) | ≥ 80% | `gremlins unleash ./internal/{module}/domain` and `./internal/{module}/app` |

These are floors, not ceilings. A module that targets 95% on `app` is welcome.

## Test categories

### Domain unit tests (`*_test.go` next to source, `package domain_test`)

- Black-box tests using `github.com/stretchr/testify/{assert,require}`.
- Table-driven for each VO constructor: valid input → no error; each invalid case → specific sentinel.
- For aggregates, every CHECK constraint replicated in Go gets a positive and negative test.

### Property-based tests (`venta_property_test.go`)

Use `pgregory.net/rapid`:

```go
func TestProperty_CrearVenta_ValidInputs(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        params := genValidParams(t) // built with rapid.IntRange, Float64Range, etc.
        v, err := domain.CrearVenta(params)
        require.NoError(t, err)
        require.NotNil(t, v)
    })
}
```

Generate edge-case-rich inputs that the table-driven tests would miss. One property per invariant.

### Fuzz tests (`fuzz_test.go`)

Use `testing.F`:

```go
func FuzzNewGPSCoords(f *testing.F) {
    f.Add(45.0, 89.0)
    f.Fuzz(func(t *testing.T, lat, lng float64) {
        _, _ = domain.NewGPSCoords(lat, lng) // must not panic
    })
}
```

The contract is "no panics on any input." Run with `go test -fuzz=Fuzz... -fuzztime=10s` ad hoc.

### App unit tests (`package app_test`)

Use hand-rolled in-memory fakes for each port. No mock libraries:

```go
type fakeVentaRepo struct {
    mu    sync.Mutex
    store map[uuid.UUID]*domain.Venta
}

func (f *fakeVentaRepo) Save(_ context.Context, v *domain.Venta) error { ... }
```

Test each command's happy path, validation errors, repository errors, and outbox failures (which must NOT fail the call — best-effort contract).

### Firebird integration tests (`{module}fb/*_test.go`)

Gate on `FB_DATABASE`:

```go
if os.Getenv("FB_DATABASE") == "" {
    t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
}
```

Use `fbtestutil.WithTestTransaction` for rollback-only tests:

```go
func TestVentaRepo_SaveRoundTrip(t *testing.T) {
    if os.Getenv("FB_DATABASE") == "" { t.Skip() }
    fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
        repo := NewVentaRepo(pool)
        require.NoError(t, repo.Save(ctx, v))
        got, err := repo.FindByID(ctx, v.ID())
        require.NoError(t, err)
        // ... assert ...
    })
}
```

Every test rolls back at the end — no state leaks between runs.

This applies to **multi-step flows too**, including ones that internally call `firebird.TxManager.RunInTx` (e.g. `AplicarVenta`, `CrearPagoConImagenes`). The TxManager is re-entrant (`transaction.go:48-69`): if the ctx already carries a tx, it reuses it instead of opening a new one. Result: every write — including Microsip cascades (`DOCTOS_PV` triggers, `DOCTOS_CC`, `CLIENTES`, `LIBRES_*`) — joins the ambient rollback-only tx. **No `t.Cleanup` with manual `DELETE` is needed.** If you're tempted to write one defensively, drop it and verify with a post-test `SELECT COUNT(*)` — the count will be 0.

Real-commit E2E tests (`pago_writer_e2e_test.go`, `pagos_recibidos_concurrency_test.go`, `atomicity_test.go`) are the exception, not the default — see `docs/integration-tests.md` § "When real commits are unavoidable" for when they're justified.

### HTTP handler tests (`{module}http/*_test.go`)

Use the chi router directly with the planter middleware (plants a `CurrentUser` on the context):

```go
r := chi.NewRouter()
r.Use(planter(currentUser))
venthttp.MountRouter(r, svc)
```

`httptest.NewRecorder` for the response. Build requests with `httptest.NewRequest` + `json.Marshal` (or `multipart.Writer` for uploads).

### Composition tests (handler chain + middleware integration)

HTTP handler tests stub the middleware chain — they plant `CurrentUser` directly and call `MountRouter` against a bare chi router. That keeps each handler test fast and focused, but it misses every bug that lives *between* the middleware and the route: chi `RouteContext` leaking from a parent dispatcher, authn rejecting an already-planted `CurrentUser`, an idempotency-key being reused on a replay path, `RequirePermission` silently bypassed because someone forgot the `.With(...)` chain. Commit `2086632` introduced one of these per fix in a five-commit sweep — none of them showed up in the per-handler unit tests.

**Rule:** every `r.Route(...)` block in `cmd/api/server.go::provideRootHandler` requires a composition test that wires the same middleware stack and exercises at least one request end-to-end through it. A new top-level route is not done until its composition test lands.

**Stubbing policy:**

- Stub only adapters of the outermost boundary: Firebase (`outbound.FirebaseClient`), Microsip (`firebird.*`), persistence (Postgres pools). Anything inside the chi router — authn, idempotency, capture, `RequirePermission` — must be the real production code path. Substituting a `fakeDispatcher` or a no-op `authn` defeats the point of the test.
- The dispatcher used by the failed-intent replay path must round-trip through the same chi router. Wire it the way `provideRootHandler` does (build the router, then `dispatcher.Set(router)`); don't shortcut with a direct `ServeHTTP` to the inner handler.

**Canonical examples:**

- `internal/platform/failedintent/http/e2e_test.go` — `TestE2E_ReplayCycle_FullMiddlewareChain` (full chi+authn+idem+capture cycle), `TestE2E_AdminFailedIntents_PermissionGrid` (per-route `RequirePermission` matrix), `TestE2E_MeFailedIntents_ScopedToCurrentUser` (`MeListar` user-scoping under real authn).
- `internal/auth/infra/authhttp/e2e_test.go` — `/v2/auth/login` idempotency through the real middleware, `GET /v2/me` planted-user bypass, `PATCH /v2/usuarios/{id}` under `RequirePermission` + idem.

**Shared helpers:** `internal/platform/httptesting/fakes.go` provides `FakeFirebase`, `FakeUsuarioRepo` (multi-user + configurable permissions), `InMemoryIdempotencyStore`, and `NewE2ERequest`. Use these instead of re-defining per-module variants — when the auth side changes shape (e.g. a new `outbound.FirebaseClient` method), a single fake update covers every composition test in the repo.

**Naming convention:** tests in this category begin with `TestE2E_` so `go test -run TestE2E ./...` runs the full composition suite without picking up unrelated unit tests.

### Security sweep (`security_test.go`)

Table-driven coverage of every protected route:

1. **Authn bypass**: route without `CurrentUser` on context → 401.
2. **Authz sweep**: route with `CurrentUser` holding every perm EXCEPT the one under test → 403.
3. **Path injection**: malformed UUID / URL-encoded SQL injection → not 500.

Important: Huma input validation runs before the handler. The sweep table must include a `bodyBuilder` for POST/PATCH/multipart routes so the request body validates and the handler actually executes.

### Mutation testing (`gremlins`)

Config in `.gremlins.yaml`. Run on demand:

```bash
make test-mutation-ventas-domain
make test-mutation-ventas-app
```

Target kill-rate ≥ 80%. A surviving mutant is either:

- A real test gap → add an assertion.
- An equivalent / defensive guard → annotate via `.gremlins.yaml` exclusions with a justification.

Time-out mutants happen when the test suite takes longer than gremlins's per-mutant timeout. They are counted as killed for efficacy purposes but they're worth investigating if the suite is slow.

## What we explicitly do NOT do

- **No remote CI**. Every quality gate runs locally via `lefthook` pre-commit and pre-push hooks. See `lefthook.yml`.
- **No goldens for full HTTP responses by default**. Huma's auto-generated error format and our DTOs are stable enough that golden snapshots add maintenance cost without proportionate signal. Handler tests + OpenAPI introspection tests cover the substance.
- **No mock generation** (`mockery` / `gomock`). Hand-rolled fakes are short, readable, and untangle from generator versioning.
- **No DB-side test seeding** outside the test transaction. Every test seeds inside `WithTestTransaction` so it rolls back cleanly.

## Determinism

For tests that exercise `audit.MarkUpdated` (which calls `time.Now()` directly), either:

- Compare timestamps with `assert.WithinDuration(t, expected, got, time.Second)`, or
- Use a `FixedClock{T: time.Date(...)}` in the Service and let the audit struct accept the clock as `time.Time` parameter (the auth and ventas modules both do this).
