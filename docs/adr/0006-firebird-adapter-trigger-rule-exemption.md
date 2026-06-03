# ADR 0006 — Firebird adapter exempt from CLAUDE.md §1 ("no logic in the database")

- **Status:** Accepted
- **Date:** 2026-06-02
- **Decision drivers:** Microsip's `MUEBLERA.FDB` is the source of truth for cobranza, ventas, and clientes. Microsip itself is **trigger-driven by construction** — hundreds of native PSQL triggers on `DOCTOS_CC`, `IMPORTES_DOCTOS_CC`, `CLIENTES`, `SALDOS_CC` and friends materialize the database's invariants. The "no logic in the database" rule in `CLAUDE.md` §1 was authored for our **own Postgres app schema** (`internal/{module}/`), not for the legacy Firebird the API mirrors. This ADR clarifies the scope of that rule so future agents do not mistakenly try to remove the triggers/procedures from `migrations-firebird/`.

## Context

`CLAUDE.md` §1 declares the database "a dummy store" and forbids triggers, stored procedures, `DEFAULT now()`, `DEFAULT gen_random_uuid()`, etc. This applies to our **app schema in Postgres** (`migrations/`). It does **not** apply to the **Firebird adapter** (`migrations-firebird/`), because:

1. **Microsip's database is not our schema.** It belongs to a third-party ERP that we read from and (in cobranza) write back to via a narrow channel. Microsip already writes to dozens of tables on every business transaction through its own triggers. Any read-model we want over Microsip data has to deal with that reality.

2. **Materialized caches need triggers to stay fresh.** The read-models `MSP_SALDOS_VENTAS` and `MSP_PAGOS_VENTAS` deliver sub-10 ms answers to the cobranza module's hot reads. The naive alternative — joining 3 to 5 Microsip tables per read — measured 600 ms to 12 seconds in dev. The only way to keep the cache in sync without a Go polling worker is to hook Microsip's own commit boundaries, which means triggers on Microsip's tables.

3. **A polling worker is strictly worse.** A ~600 LOC Go worker that scans Microsip tables every N seconds would (a) duplicate Microsip's own commit lifecycle, (b) introduce a window of staleness, (c) require its own coordination + idempotency story, (d) need to be paused/resumed across deploys. Triggers ride Microsip's atomicity for free: the cache updates in the same `COMMIT` as the original write.

4. **Atomicity is a feature.** Because the cache update is in the user's own transaction, an external observer can never see a cache row that's out of sync with the data it mirrors. With a worker, every read between source-write and worker-tick is racey.

5. **The pattern is already idiomatic for this engine.** Firebird's PSQL has `WHEN ANY DO` for defensive trigger-side error handling and `POST_EVENT` for commit-bound notifications. We are not inventing a paradigm — we are following the same idiom Microsip itself uses.

## Decision

**The Firebird adapter (`migrations-firebird/`) is exempt from `CLAUDE.md` §1.** It is allowed (and expected) to:

- Create triggers (`CREATE TRIGGER ... AFTER INSERT OR UPDATE OR DELETE ... POSITION 100 ...`).
- Create stored procedures (`CREATE PROCEDURE MSP_RECOMPUTE_*`).
- Use `CURRENT_TIMESTAMP` inside triggers/procedures to stamp `UPDATED_AT`.
- Use `POST_EVENT '<name>'` inside procedures/triggers to notify the Go listener (commit boundary).
- Use Firebird `WHEN ANY DO` to keep cache-side errors from aborting the user's tx, redirecting them to `MSP_SALDOS_ERRORS` for offline diagnosis.

What it is **still not allowed** to do:

- Encode **business rules** in triggers. The trigger's job is to keep the cache consistent with Microsip data, not to make business decisions. Anything beyond "row X changed → recompute cache row Y" belongs in Go (see the `Reconciler` in `internal/cobranza/app/`).
- Spread logic across many small triggers. Each cache table has **one** authoritative recompute procedure (e.g. `MSP_RECOMPUTE_PAGO`, `MSP_RECOMPUTE_SALDO_VENTA`). Triggers are thin: they detect an interesting change and `EXECUTE PROCEDURE` the recompute. Bodies stay small.
- Mirror operational data the API itself owns. The `MSP_PAGOS_RECIBIDOS` outbox (writes pushed back to Microsip) is owned by Go — no triggers fire on it from our side.

## Inventory of trigger-side logic as of mig 21

For reference (read the migration source files for canonical bodies, not this list):

| Migration | What it adds |
| --- | --- |
| `000010_msp_saldos_ventas` | `MSP_SALDOS_VENTAS` cache + `MSP_RECOMPUTE_SALDO_VENTA` procedure + triggers on `DOCTOS_CC`, `IMPORTES_DOCTOS_CC`, `CLIENTES`. |
| `000011_fix_fecha_ult_pago_y_conceptos` | Patches `MSP_RECOMPUTE_SALDO_VENTA` for `FECHA_ULT_PAGO` precision + concept set. |
| `000012_fecha_ult_pago_timestamp_coalesce` | Patches `MSP_RECOMPUTE_SALDO_VENTA` to coalesce `FECHA_ULT_PAGO` to a timestamp. |
| `000013_msp_pagos_ventas` | `MSP_PAGOS_VENTAS` cache + `MSP_RECOMPUTE_PAGO` procedure + triggers on `IMPORTES_DOCTOS_CC`, `DOCTOS_CC`, `CLIENTES`, `MSP_PAGOS_RECIBIDOS`. |
| `000014_tombstone_saldos` | `MSP_RECOMPUTE_SALDO_VENTA` writes tombstones on cancel (vs DELETE). |
| `000015_saldo_incluye_impuesto` | Patches `MSP_RECOMPUTE_SALDO_VENTA` to include tax in `SALDO`. |
| `000019_tombstone_pagos` | `MSP_RECOMPUTE_PAGO` writes tombstones on cancel. |
| `000020_tombstone_on_delete` | Trigger `MSP_PAGOS_IMPORTES_AIUD` DELETING branch + `MSP_SALDOS_DOCTOS_CC_AD` UPDATE-to-tombstone instead of DELETE. |
| `000021_post_event_recompute` | `POST_EVENT 'pagos_changed' / 'saldos_changed'` from `MSP_RECOMPUTE_*` and the tombstone-trigger branches. **Never** from `WHEN ANY DO`. |

The `POST_EVENT` in mig 21 enables the Go `FbEventListener` (`internal/cobranza/infra/ventfb/fb_event_listener.go`) to receive change notifications on tx commit and fan them out to the in-process `eventbus.Bus`, which the SSE handlers (`handlers_sse.go`) deliver to mobile clients.

## Alternatives considered

- **Go polling worker over Microsip.** Rejected: latency, staleness window, duplicated coordination, and a 600 LOC code surface that needs its own tests. The cache would always lag the source by up to N seconds and would need to be paused on deploy to avoid double-applying outbox events.
- **CDC stream out of Firebird.** Not feasible: Firebird 4.0 has no first-class CDC, and `RDB$EVENTS` does not include the changed row payload. The Microsip vendor would not support an external WAL reader on their production DB.
- **Read-through Go cache.** Rejected: the read path already hits sub-10 ms with the materialized cache; the additional moving part (Redis or in-process) buys nothing and adds a new failure mode (cache invalidation on Microsip rollback).

## Consequences

- The Firebird adapter is allowed to grow trigger/procedure logic as the cobranza module evolves. New `MSP_*` cache tables follow the same pattern: one recompute procedure + thin triggers + `POST_EVENT`.
- Reviewers (humans and AI agents) reading `migrations-firebird/` should not invoke `CLAUDE.md` §1 to reject new trigger/procedure code. They should still reject **business logic** in triggers — that goes in Go.
- The on-prem deployment target (Windows Server 2016, no Docker, no orchestrators) does not change. Triggers and procedures are pure Firebird and ship with the database file.
- The Postgres app schema (`migrations/`) remains strictly governed by `CLAUDE.md` §1. The exemption is scoped to `migrations-firebird/` only.
