# Integration Tests

> Reference for agents writing or maintaining integration tests in msp-api.
> Firebird-backed (the dev `mueblera-firebird` container) — see ADR-0008.

## TL;DR

- One Firebird container shared by every package and every test (`mueblera-firebird`, the same one the dev API uses).
- Each integration test wraps its writes in `fbtestutil.WithTestTransaction` so the rollback at the end leaves the shared DB clean.
- Gate by env var `FB_DATABASE` (the same one the running API consumes). Never `//go:build` tags except the package-wide `!ci_skip_firebird` already in use.
- All Firebird integration tests in `internal/platform/...` are `t.Parallel()`-safe because they share the pool. Module-level dispatcher tests run sequentially when they exercise multi-row state (`internal/platform/outboxfb/dispatcher_integration_test.go`).
- No `time.Sleep`. No mocking libs.

## How the Firebird container is wired

`make test-firebird-all` and friends just call `go test` with `FB_DATABASE` set from the `.env` file. The container is brought up out of band (typically the first time you ran `make dev` or by the user manually):

```sh
docker compose up -d --wait api          # brings the dev API + reuses mueblera-firebird
make fb-migrate-status                   # confirms 000029-000031 + everything else is applied
make test-firebird-all                   # runs every Firebird-gated package
```

Day-to-day flow:

```sh
make test                  # unit-only, no Firebird needed
make test-firebird         # only internal/platform/firebird + fbtestutil
make test-firebird-all     # platform + auth + ventas + cobranza + idempotency + failedintent + outboxfb
make test-firebird-ventas  # narrower: platform + ventas (~25s)
```

If `FB_DATABASE` is empty the targets bail with an explicit error so you don't accidentally run the full suite without a DB.

Running `go test` directly without `FB_DATABASE` set is fine: every gated test calls `t.Skip` instead of failing. IDE / ad-hoc runs work.

## Hard rules

1. `t.Parallel()` on every test, EXCEPT when it mutates ambient state (`slog.SetDefault`, env vars, shared pool counters). Mark non-parallel tests with `//nolint:paralleltest // <reason>`.
2. `t.Helper()` first line of every helper.
3. `require` for setup, `assert` for results.
4. `require.Eventually` (or channels) instead of `time.Sleep`.
5. Mocks are structs with function fields. No mocking libraries.
6. Default `fbtestutil.WithTestTransaction`. The transaction always rolls back at the end of the callback — that is the ONLY safe pattern for the shared dev DB. There is no commit counterpart.
7. Env-var gating in `TestMain` or per-test via a small `requireFBEnv(t)` helper. The package-wide `!ci_skip_firebird` build tag stays on every `*_integration_test.go` so CI-style runs can opt out wholesale.
8. Factories / row-construction helpers live in the test package itself (`newRecord`, `newIntent`, `insertPendingEvent`) — small, self-contained, no global registry.
9. No DB defaults / triggers on `MSP_*` tables (CLAUDE.md §1). Tests pass timestamps and UUIDs explicitly, same as production.

## The `WithTestTransaction` pattern

```go
fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
    // Anything you write through ctx (or via firebird.GetQuerier(ctx, pool.DB))
    // joins the test's tx and rolls back at the end of this callback.
    err := store.Save(ctx, record)
    require.NoError(t, err)

    got, err := store.Get(ctx, record.Key)
    require.NoError(t, err)
    require.NotNil(t, got)
})
```

`firebird.TxManager.RunInTx` is re-entrant, so production code that opens its own
transaction inside the callback just joins the test's outer transaction
automatically. That is why `store.Save` works inside `WithTestTransaction` even
though the production implementation wraps its own `RunInTx`.

When a test truly needs committed rows (the outboxfb dispatcher reading rows
through a separate worker, for example), it commits via real `firebird.RunInTx`
and registers a `t.Cleanup` to `DELETE FROM ... WHERE ID = ?` so the shared DB
stays clean. Use UUIDs scoped per test (`uuid.New()`) so concurrent tests cannot
collide.

## Patterns by package shape

### Stores / repos (`internal/platform/{idempotency,failedintent}/firebird`)

- Save then Get round-trip.
- Save twice with same key → first-writer-wins (or PK no-op).
- Save replaces expired row (where TTL applies).
- Get filters expired.
- PurgeExpired / PurgeOlderThan returns the count and the side effects (blob paths) the janitor needs.

### Dispatchers (`internal/platform/outboxfb`)

- Insert pending row → start dispatcher → assert PROCESSED_AT / FAILED_AT / ATTEMPTS via a SELECT.
- ErrTransient + MaxAttempts boundary.
- No handler → marked failed with `outboxfb: no handler registered`.
- Ordering by CREATED_AT ASC inside a single tick.
- Stop drains in-flight work (each tick uses its own context derived from background, so Stop signalling does not interrupt the COMMIT).
- `goleak.VerifyNone(t, probeLeakIgnores()...)` after Start/Stop.

### Repos with strong cross-module coupling (ventas, cobranza)

- Smoke INSERT of an aggregate root + its children inside one tx, SELECT back column-by-column.
- Optimistic concurrency: second writer with the same `Version` must surface `apperror.NewConflict(...)`.
- Encoding round-trip (UTF-8 with Spanish accents, emoji) — see `internal/ventas/infra/venthttp/encoding_boundary_test.go`.

## Snapshot / restore safety

Long-running test sessions that smoke-test the API by hand should snapshot the
DB before any destructive interactive operation. Per the standard runbook:

```sh
make fb-snapshot NAME=before_experiment
# ... do stuff ...
make fb-restore NAME=before_experiment    # if it broke
make fb-snapshot-delete NAME=before_experiment  # if it didn't
```

The "master" baseline is `clean-with-admin.fbk` (schema migrated + admin user
seeded + every `MSP_*` table empty). After landing migrations 000029-000031 the
master is regenerated per `docs/firebird-snapshots.md` §"Baseline canónico".

## Anti-patterns

- ❌ Don't `INSERT` rows that escape `WithTestTransaction` — the shared dev DB is not yours to pollute.
- ❌ Don't write tests that assume "the only row in the table"; always scope by a unique UUID or `aggregate_id`.
- ❌ Don't `t.Parallel()` on tests that drive an outboxfb dispatcher — the driver's per-connection state races under nakagami/firebirdsql in this configuration.
- ❌ Don't add a Postgres testcontainer back. The Postgres adapter was removed in commit `0fff361`; integration tests on Firebird alone cover the same surface and run faster.
