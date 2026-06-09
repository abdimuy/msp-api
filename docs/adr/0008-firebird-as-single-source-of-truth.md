# ADR 0008 — Firebird as the single source of truth

- **Status:** Accepted
- **Date:** 2026-06-08
- **Supersedes:** [ADR 0001 — Outbox event strategy for cross-DB writes](0001-outbox-strategy.md)
- **Decision drivers:** the inventario module landed, traspasos events became operationally critical, and the cost of "best-effort dual-write" finally exceeded the cost of consolidating.

## Context

When ADR-0001 was written (2026-05-10), the API only emitted soft audit events
from the auth module (`user.synced`, `role.assigned`, …). A lost event was
recoverable by hand from `slog` because the canonical state lived in Firebird
and could always be re-derived. That tolerance bought us a year of zero
infrastructure: a single `outbox_events` table in Postgres, dual-write with
slog-as-backup, no XA.

ADR-0001 §Consequences explicitly predicted the breaking point:

> "ADR-0003 (TBD) will introduce `MSP_OUTBOX_EVENTS` once a module needs hard
> guarantees (likely `pagos` or `traspasos`)."

That moment arrived. The inventario module pushes `traspaso.creado` events
that drive the warehouse-side reservation flow; the ventas module's
`venta.creada` event materialises read-models the cobranza dashboard depends
on. A crash between the Firebird COMMIT and the Postgres INSERT — the exact
window ADR-0001 documented as recoverable — now corrupts operator-visible
state. The recovery path (grep `slog`, decode JSON, replay by hand) is not
something the on-call rotation can execute under load.

At the same time, the operational surface of running Postgres alongside Firebird
on Windows Server 2016 has only grown:

- Two backup pipelines, two snapshot tools, two restore drills.
- A cross-DB `transaction.Manager` mirrored by `firebird.TxManager` — both
  maintained, both subtly different.
- `make fb-snapshot` does not capture outbox/idempotency/failed-intent state,
  so a snapshot-restore cycle silently drops events.
- The deploy target (Windows Server 2016 running Microsip) already runs
  Firebird; Postgres is a *purely additional* service we install and babysit.

The hex architecture in `internal/{module}/` shields the cost of swapping
storage: the ports (`OutboxEnqueuer`, `idempotency.Store`, `failedintent.Store`)
are stable; only the infra implementations change.

## Options considered

### 1. Stay on dual-write, harden the recovery path

Keep Postgres for outbox/idempotency/failed-intents. Add a replay tool that
scans `slog` and re-enqueues lost events. Add a sentinel row in Postgres
written *before* the Firebird commit and reconciled after, so the gap window
is observable.

**Pros**
- Zero migration. No change to the deploy.

**Cons**
- The gap window is still real, just instrumented. Operator pain unchanged.
- A second DB on Windows Server 2016 stays in the operational budget forever.
- Re-implements outbox semantics in `slog` + a sentinel — the very pattern an
  outbox is supposed to replace.

### 2. Move outbox/idempotency/failed-intents to Firebird (chosen)

Create three `MSP_*` tables in Firebird (`MSP_OUTBOX_EVENTS`,
`MSP_IDEMPOTENCY_KEYS`, `MSP_FAILED_INTENTS`). Implement the ports against
Firebird via new `outboxfb` / `idempotency/firebird` / `failedintent/firebird`
packages. Drop the entire Postgres dependency tree.

**Pros**
- **True atomicity**: `firebird.TxManager.RunInTx` is re-entrant, so the
  business write and the outbox INSERT run inside the same transaction.
  Either both persist or neither does. Zero gap window.
- **One backup, one snapshot, one restore.** `make fb-snapshot` now captures
  the full operational state.
- **One transaction manager.** `internal/platform/transaction/` deletes
  entirely; `firebird.TxManager` is the only manager.
- **Convergence with the deploy target.** Windows Server 2016 was already
  running Firebird for Microsip; Postgres becomes one less service to
  install, monitor, back up, and restart.
- **Stack constraint #5 alignment.** The CLAUDE.md hard rule "this project
  targets Windows Server 2016 legacy" is best served by fewer moving parts.

**Cons**
- Per-query latency goes from ~1ms (pgx) to ~5-15ms (nakagami/firebirdsql).
  At ~1,000 mutating reqs/day (≈12/min in pick), this is operationally
  invisible — well under the p95 budget on every endpoint we measure.
- Firebird has no `SELECT ... FOR UPDATE SKIP LOCKED`, so the outbox
  dispatcher is restricted to a single worker. See "Concurrency" below.
- The mutation surface widens slightly: three new `MSP_*` tables now require
  the same "no logic in DB" discipline as the existing ones. This is enforced
  by CLAUDE.md §1 and verified by code review on every migration.

### 3. Keep Postgres but switch dispatch to Firebird-side outbox

Hybrid: Firebird holds `MSP_OUTBOX_EVENTS` (atomic with business writes), but
the dispatcher writes processed events back into Postgres `outbox_events` for
existing consumers. Idempotency + failed-intents stay on Postgres.

**Pros**
- Closes the atomicity gap that motivated this ADR.

**Cons**
- Worst of both: still two DBs to back up, snapshot, restore. Still two tx
  managers. Adds a *third* event pipeline (Firebird → Postgres → consumers).
  Solves the immediate pain without paying down the operational debt.

## Decision

**Adopt option 2.** All operational state of `msp-api` lives in Firebird:
Microsip's pre-existing tables, our `MSP_*` business tables, plus the three
new `MSP_OUTBOX_EVENTS` / `MSP_IDEMPOTENCY_KEYS` / `MSP_FAILED_INTENTS`
tables introduced by migrations 000029-000031.

ADR-0001's option 2 (Firebird-side outbox) was the eventual destination from
the start. This ADR is its enactment.

### Atomicity contract

The outbox INSERT happens inside the **same** Firebird transaction as the
business write:

```go
runner.RunInTx(ctx, func(ctx context.Context) error {
    if err := businessWrite(ctx); err != nil { return err }
    return outboxfb.Enqueue(ctx, q, event)
})
```

`firebird.TxManager.RunInTx` is re-entrant (see
`internal/platform/firebird/transaction.go`): nested `RunInTx` calls reuse
the outer transaction. The enqueuer call inside a service that already opened
a tx therefore writes into the existing tx, and the COMMIT covers both the
business row and the event row. If the COMMIT fails or the service returns
an error before COMMIT, both rollback together.

This is the property that ADR-0001 documented as desirable but could not
deliver across DBs.

### Concurrency

Firebird does not support `SELECT ... FOR UPDATE SKIP LOCKED`. The dispatcher
runs as a **single goroutine** that polls `MSP_OUTBOX_EVENTS` for pending
rows ordered by `CREATED_AT ASC`, dispatches each event to its handler, then
updates `PROCESSED_AT` / `FAILED_AT` / `ATTEMPTS`. At our scale (~12
events/min in pick), one worker is order-of-magnitude headroom.

When the scale outgrows a single worker — order of magnitude estimate ≥150
events/sec sustained — the upgrade path is documented:

1. Add `CLAIMED_AT TIMESTAMP` and `CLAIMED_BY VARCHAR(64)` columns to
   `MSP_OUTBOX_EVENTS`.
2. Replace `SELECT ... ROWS N` with `UPDATE MSP_OUTBOX_EVENTS SET CLAIMED_AT
   = ?, CLAIMED_BY = ? WHERE ID = ? AND CLAIMED_AT IS NULL`. The
   first-writer-wins semantics fall out of the row lock — losers see 0
   affected rows and skip.
3. Add a janitor that clears `CLAIMED_AT` on rows whose worker died (e.g.
   `WHERE CLAIMED_AT < now() - '5 min'::interval AND PROCESSED_AT IS NULL`).

Do not add this complexity now. We will not need it.

### Idempotency under concurrent retries

Two requests arriving simultaneously with the same `Idempotency-Key` race on
the `MSP_IDEMPOTENCY_KEYS` PK. The Firebird store handles this as
first-writer-wins: the loser catches the PK-violation error and treats it as
a successful no-op (the cached response is whatever the winner persisted).
This mirrors the Postgres `INSERT ON CONFLICT` semantics without `ON
CONFLICT` itself — Firebird does not have it.

### Failed-intent capture is unaffected

`failedintent.Store` is a write-only-then-read interface (the middleware
saves, admins later list / replay / purge). It has no transactional coupling
to business writes, so the swap is purely a Postgres→Firebird rewrite of the
five SQL methods.

## Consequences

- **Operational**: `docker compose` no longer requires the `msp-postgres`
  service. `make fb-snapshot` captures the full API state. One restore drill,
  one backup pipeline.
- **Codebase**: `internal/platform/{transaction,outbox,postgres}` delete.
  `internal/platform/{idempotency,failedintent}/postgres/` delete. Three new
  Firebird-backed packages take their place. The change is invisible to
  `internal/{module}/app/` and above — services, handlers, domain entities
  do not move.
- **CLAUDE.md §1**: the "no logic in the database" rule now applies
  uniformly to all `MSP_*` tables, including the three new ones. The ADR-0006
  exemption remains for tables that mirror Microsip's read-model
  (`mirror_*` cobranza tables that depend on POST_EVENT-driven triggers).
- **CLAUDE.md §7**: integration tests no longer require a Postgres
  testcontainer. The Firebird container that already serves Microsip tests
  covers the full surface.
- **Migration footprint (one-time)**: the dispatcher table in the old
  Postgres outbox has rows pending at cutover. Runbook exports them to a
  JSONL file (`outbox-pending-pre-migration.jsonl`) for manual replay if any
  prove load-bearing. Most are cosmetic (`user.synced`) and recoverable from
  Firebird state.
- **Future ADR space**: if Firebird query latency on the idempotency
  middleware ever becomes a real p95 regression (it will not at this scale),
  an in-memory LRU in front of `idempotency.Store` is the cheapest fix and
  does not change the durability story.

## Out of scope

- The Microsip mirror / projection tables are governed by **ADR-0006**
  (trigger-rule exemption) and **ADR-0007** (cobranza push watermark). Those
  tables remain trigger-driven by construction; this ADR does not weaken or
  reinterpret either.
- The `audit.Auditable` / `audit.Timestamped` embed-pattern in
  `internal/platform/audit/` is unchanged. Domain entities still set IDs and
  timestamps in Go (CLAUDE.md §1).
- This ADR does not introduce a new event broker. `outboxfb.Handler`
  implementations remain in-process Go code, identical to today's
  `outbox.Handler`.

## How to validate

1. After the migration, every commit on the migration branch keeps `make
   lint`, `make test`, `make test-firebird-all` green.
2. The E2E test `TestAtomicity_VentaCreada_RollbackTakesOutboxWithIt`
   reproduces the gap window ADR-0001 documented and asserts it no longer
   exists.
3. The runbook scenario "Firebird unreachable mid-write" replaces the legacy
   "search slog for lost auth events" runbook.
