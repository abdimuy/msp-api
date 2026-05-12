# ADR 0001 — Outbox event strategy for cross-DB writes

- **Status:** Accepted
- **Date:** 2026-05-10
- **Decision drivers:** auth module emits audit events from Firebird writes; the outbox table lives in Postgres.

## Context

The `msp-api` topology splits state across two databases:

- **Firebird (Microsip)** holds the canonical business data we write to: users, roles, role-assignments, cobranza, ventas, traspasos.
- **Postgres** holds the `outbox_events` table, projections (`projection_*`), idempotency keys, and other operational tables.

Domain commands that mutate Firebird (e.g. `SyncFromFirebase`, `AsignarRol`, `DesactivarUsuario`) must also publish an event so downstream consumers — projections, webhooks, an eventual Kafka bridge — observe the change. The natural pattern is the **transactional outbox**: write the row and the event in the same transaction, then have a dispatcher pump events out.

The problem: a single ACID transaction cannot span two databases without XA / 2-phase commit. The Firebird driver does not support XA, and we have already decided we do not want to operate a distributed transaction coordinator on Windows Server 2016. So the question is how to bridge the gap.

## Options considered

### 1. Best-effort dual-write (chosen)

Commit Firebird first. After the commit succeeds, open a separate Postgres transaction and `INSERT` the event into `outbox_events`. If the Postgres write fails, log a structured `slog.Error` with the full event payload and return nil to the caller — the business operation succeeded; only the auxiliary event was lost.

**Pros**
- Zero new infrastructure.
- No coordinator, no XA, no orchestrator.
- Simple mental model: business write is the source of truth.

**Cons**
- A crash between the Firebird commit and the Postgres insert leaves the event un-emitted. Recoverable from logs (the payload is structured) but not automatic.
- Eventually-consistent projections may briefly lag.

### 2. Firebird-side outbox table

Add a `MSP_OUTBOX_EVENTS` table in Firebird. Domain commands enqueue events in the same Firebird transaction as the business write. A separate dispatcher reads from this table and copies rows into Postgres (or directly into consumers).

**Pros**
- Truly transactional. No event is ever lost.
- Recovery from crashes is automatic — the dispatcher just keeps draining the table.

**Cons**
- Doubles the migration surface and adds a long-lived adapter.
- Adds an end-to-end test loop (the dispatcher must be running during integration tests).
- Premature complexity for the auth module's volume (≤1k events/day at our scale).

### 3. Domain events via a queue (Kafka / NATS)

Replace the outbox with a broker. Domain commands enqueue at-least-once.

**Pros**
- Decouples publishers and consumers.

**Cons**
- New infrastructure to install on Windows Server 2016 — defeats stack constraint #5 in CLAUDE.md.
- We don't have multi-consumer topology yet to justify the complexity.

## Decision

Adopt **option 1 (best-effort dual-write)** for v1. Document the failure mode loudly (this ADR), structure the logging so a lost event can always be reconstructed from `slog`, and treat option 2 as a planned epic once a module appears whose event volume or criticality makes the loss intolerable.

### Implementation rules

1. `app` services emit events through the `outbound.OutboxEnqueuer` port — they never see Postgres directly.
2. The infra implementation (`internal/auth/infra/outbox/enqueuer.go`) MUST commit Firebird before calling Postgres. The Firebird commit is the source-of-truth boundary.
3. If the Postgres `INSERT` fails:
   - Log `slog.Error("auth.outbox_enqueue_failed", "event_type", …, "aggregate_id", …, "payload", …, "error", err)`.
   - Return `nil` to the caller. The business operation is complete.
4. The `slog.Error` payload MUST be sufficient to replay the event manually. That means including the full marshalled JSON payload, not just a summary.
5. Never retry within the request path. The Postgres dispatcher already retries failed deliveries.

### Auth events covered

`user.synced`, `user.updated`, `user.deactivated`, `role.created`, `role.updated`, `role.deactivated`, `role.assigned`, `role.revoked`, `role.permission_granted`, `role.permission_revoked`.

For auth specifically, a lost event is recoverable: the canonical state is in Firebird and can be re-derived. This is the property that justifies "best-effort" here and would not justify it for, say, cobranza receipts.

## Consequences

- **Operational**: oncall must scan for `auth.outbox_enqueue_failed` in logs after any Postgres incident and manually re-enqueue.
- **Future work**: ADR-0003 (TBD) will introduce `MSP_OUTBOX_EVENTS` once a module needs hard guarantees (likely `pagos` or `traspasos`).
- **Testing**: integration tests must verify the slog payload is emitted when Postgres is unreachable; the contract is "no event reaches consumers, but it's recoverable".
- **No retries in the request path**: if Postgres is degraded, the API stays responsive — users still see their writes succeed.
