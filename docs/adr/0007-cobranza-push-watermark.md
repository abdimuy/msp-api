# ADR 0007 — Cobranza push channel + xmin watermark architecture

- **Status:** Accepted
- **Date:** 2026-06-03
- **Related migrations:** `migrations-firebird/000022_changelog_and_tx_id.up.sql`, `000023_recompute_changelog.up.sql`, `000024_tombstone_changelog.up.sql`, `000025_tx_id_indices.up.sql`
- **Related ADR:** [ADR 0006](0006-firebird-adapter-trigger-rule-exemption.md) — Firebird adapter trigger-rule exemption

## Context

End of May 2026 we shipped real-time SSE for cobranza: `MSP_RECOMPUTE_PAGO` / `MSP_RECOMPUTE_SALDO_VENTA` emit `POST_EVENT 'pagos_changed'` / `'saldos_changed'` at commit; a Go listener consumes them and publishes to an in-process `eventbus.Bus`; SSE handlers stream the wake-up to mobile clients which then call the cursor-based `/sync/{kind}/zona/{zona}` endpoint. End-to-end latency from API insert to UI render measured ~327 ms on a real cobrador (zona 21563).

Validation with the customer surfaced two correctness gaps that the SSE-on-top-of-cursor design did not resolve:

1. **Deletion latency**: the client's SSE-triggered sync runs inside the server's `syncLagSeconds = 5 s` upper-bound window (`UPDATED_AT <= server_now - 5 s`). A tombstone row whose `UPDATED_AT` is "right now" is excluded by that filter, so the deletion is only delivered ~20 seconds later by the 30 s polling tick or the 5-minute reconciler. Acceptable for ordinary updates, not for "cobrador cancela su último pago" — the demo case for staff.

2. **Lost-update under Microsip GUI writers**: the customer told us "se escribe desde microsip pagos y ventas" — office staff enters charges and payments directly via the Microsip GUI. Those sessions hold open transactions for minutes while the operator fills the form. Microsip's own triggers fire at write time and stamp `UPDATED_AT = trigger fire time` (not commit time). When the operator finally commits, `POST_EVENT` fires — but the mobile client's cursor has long since advanced past that `UPDATED_AT`. The row is **permanently invisible** to that cobrador until a manual reconcile.

Extending `syncLagSeconds` to cover Microsip's longest GUI transaction (typically 1–10 minutes) would have made the SSE indistinguishable from polling — defeats the point.

## Decision

Add a **push channel + xmin watermark** layer in front of the existing cursor sync, **without removing the cursor sync** (it remains the safety net).

### Components

| Layer | Latency | Mechanism | Source of truth |
|---|---|---|---|
| Push channel | < 500 ms | Append-only changelog tables + listener publishes specific IDs + by-ids HTTP endpoint | `MSP_{PAGOS,SALDOS}_CHANGELOG.SEQ_ID` cursor |
| Cursor sync | ≤ 30 s | Existing `/sync/{kind}/zona/{zona}` + watermark filter | `(UPDATED_AT, pk)` cursor + `TX_ID < watermark` |
| Reconciler | ≤ 5 min | Drift detector + tombstone cleanup + changelog pruner | `MSP_SALDOS_VENTAS` row-by-row recompute |

### Watermark

```sql
SELECT COALESCE(MIN(MON$TRANSACTION_ID), 0x7FFFFFFFFFFFFFFF)
  FROM MON$TRANSACTIONS WHERE MON$STATE = 1
```

Returns the smallest **active** transaction id on the server, or `math.MaxInt64` if none are active. Both the listener (per event) and the cursor sync (per page) query this and use it as a **strictly less-than** upper bound on `TX_ID`.

By construction any committed transaction has a `TX_ID` lower than every currently-active TX_ID, so `TX_ID < watermark` selects only "definitely-committed, definitely-not-being-rewritten" rows. When a long-running Microsip GUI transaction eventually commits, its `TX_ID` becomes strictly less than the next poll's watermark, so the next cursor sync round picks it up cleanly. **Zero lost-updates by construction.**

### Changelog

Three migrations install an append-only event log:

- **`migrations-firebird/000022_changelog_and_tx_id.up.sql`** creates `MSP_PAGOS_CHANGELOG (SEQ_ID BIGINT PK, IMPTE_DOCTO_CC_ID INTEGER, TX_ID BIGINT, COMMIT_AT TIMESTAMP)`, an analogous `MSP_SALDOS_CHANGELOG`, two dedicated generators, and `ALTER TABLE MSP_{PAGOS,SALDOS}_VENTAS ADD TX_ID BIGINT DEFAULT 0 NOT NULL`. Existing rows backfill to `TX_ID = 0` — "committed in the distant past" semantically.

- **`migrations-firebird/000023_recompute_changelog.up.sql`** extends `MSP_RECOMPUTE_PAGO` and `MSP_RECOMPUTE_SALDO_VENTA` so every successful exit path (invalid → DELETE; tombstone upsert; normal upsert) appends a changelog row with `TX_ID = CAST(CURRENT_TRANSACTION AS BIGINT)` and sets `TX_ID` in the cache row. `WHEN ANY DO` is byte-for-byte unchanged from mig 21 — failure paths never append (the `INSERT` is rolled back atomically with the upsert).

- **`migrations-firebird/000024_tombstone_changelog.up.sql`** extends the `MSP_PAGOS_IMPORTES_AIUD` (DELETING branch) and `MSP_SALDOS_DOCTOS_CC_AD` triggers so physical deletes of `IMPORTES_DOCTOS_CC` or `DOCTOS_CC` rows also append to the changelog and set `TX_ID`. This is what closes gap 1: deletes are now first-class push events instead of being hidden by the lag window.

- **`migrations-firebird/000025_tx_id_indices.up.sql`** adds `IDX_MSP_PAGOS_VENTAS_TX_ID` and `IDX_MSP_SALDOS_VENTAS_TX_ID` so the watermark predicate in cursor sync hits an index instead of a table scan.

### Firebird version note

The procedures use `CAST(CURRENT_TRANSACTION AS BIGINT)` — the PSQL pseudo-register available since Firebird 2.1. We initially specified `RDB$GET_CONTEXT('SYSTEM','CURRENT_TRANSACTION')` but production runs Firebird 2.5 (Microsip's bundled engine), where that namespace returns `NULL`. The pseudo-register has identical semantics and is the canonical idiom on this engine.

### Listener protocol

`internal/cobranza/infra/ventfb/fb_event_listener.go`:

1. On Start: probe `MinActiveTransactionID()` and `Pagos/SaldosChangelogRepo.MaxSeqID(watermark)`; set `lastSeenSeq[topic]` to those values. The cursor begins at "everything currently visible" — no replay of history.

2. On each `FbEvent`: probe `MinActiveTransactionID()`, then `Changelog.Since(lastSeenSeq[topic], watermark, limit=500)`. Publish the returned IDs to the bus. Advance `lastSeenSeq[topic] = max(returned.SeqID)` — **only to max returned**, never further. Rows with `TX_ID >= watermark` (in-flight) stay below `lastSeenSeq` and reappear in the next poll once their tx commits.

3. On reconnect: publish `[]int{}` per topic. The empty slice is a wake-up signal — subscribers fall back to cursor sync, which is the authoritative recovery path during listener outages.

4. Errors during `MinActiveTransactionID` or `Since` log a warning and publish `[]int{}` so subscribers cursor-sync. **The cursor sync round is the safety net** for any missed push.

### Eventbus payload

`internal/cobranza/app/eventbus/eventbus.go` was refactored from `Publish(topic)` / `Subscribe() <-chan struct{}` to `Publish(topic, ids []int)` / `Subscribe() <-chan []int`. Coalescing semantics changed from "drop new when buffer is full" to **latest-wins** (drain stale, push new) — the subscriber's cursor sync round picks up the discarded intermediate IDs anyway, and we always want the most recent payload.

`nil` and `[]int{}` are equivalent: both delivered as `[]int{}`, both interpreted as "wake up and cursor-sync".

### SSE payload

`handlers_sse.go` emits exactly `data: {"ts":N,"ids":[i1,i2,...]}\n\n`. `ts` is millis epoch UTC for client-side latency measurement; `ids` is the slice from the bus. Empty slice → `"ids":[]`. Clients that only parse `"ts"` via regex (older mobile builds) ignore `"ids"` and degrade transparently to cursor sync. New clients parse both and call `/by-ids` for the specific PKs.

### By-ids endpoints

`GET /v2/cobranza/sync/{pagos,saldos}/by-ids?zona_id=N&ids=A,B,C` returns the rows for the given PKs constrained to the requested zone. Cap **500 IDs per request** (URL ≈ 4 KB, well under the 8 KB practical limit). Watermark filtering is **not** applied here — the IDs come from the listener which only publishes after `TX_ID < watermark`, so they are already definitely committed.

### Reconciler pruner

A 1-hour ticker in `CobranzaReconciler` calls `Pagos/SaldosChangelogRepo.DeleteOlderThan(now - 7d, max=50_000)`. Independent of the existing 7-day reconcile pass and 30-day tombstone cleanup. Cap exists to bound lock contention on the first run after a large catch-up.

## Consequences

### Positive

- **Latency**: deletes and Microsip GUI commits propagate to UI in < 500 ms instead of 20 s / never. Inserts and updates remain in the ~327 ms band measured pre-sprint.
- **Correctness**: `TX_ID < watermark` makes lost-update impossible by construction. No tuning of lag windows needed.
- **Coexistence**: old mobile clients that parse only `"ts"` continue to work; new clients opportunistically use `/by-ids`. No flag day.
- **Safety net**: cursor sync remains the authoritative path during listener outages; reconciler remains the authoritative drift detector. Push channel is a latency optimizer + structural correctness lift, not a single point of failure.

### Negative / accepted trade-offs

- **Two more tables to back up**: `MSP_{PAGOS,SALDOS}_CHANGELOG` grow ~1 k – 10 k rows/day in steady state. With 7-day retention and prune-per-hour, footprint is bounded to a few hundred MB.
- **`MON$TRANSACTIONS` per push event**: one extra cheap metadata read per FbEvent (~µs). Negligible at the volumes we see.
- **`CAST(CURRENT_TRANSACTION AS BIGINT)` vs portability**: this binds the schema to Firebird 2.1+. We are already non-portable on the engine itself; this does not narrow the deployment surface.
- **`lastSeenSeq` is in-memory**: a process restart loses it. Recovered by the Start-time `MaxSeqID(watermark)` probe + cursor sync at the bus subscriber level. Persisting was considered and rejected: digest reconciler at 5 min is the authoritative safety net, restarts are rare, and persistence adds boundary complexity for marginal ROI.
- **Long Microsip GUI transactions stall the watermark**: if an operator leaves a tx open for hours, the watermark stays low and cursor sync stops advancing. The system is **correct** during this window (no lost updates, the next watermark advance catches up), but other cobradores see fresh cargos delayed. Mitigation is operational: Microsip already warns on long sessions; if it becomes a problem we add a per-zona watermark or surface it on the admin dashboard. **Not solving this in this sprint.**

## Operator runbook

### "¿Por qué este pago no apareció en la app del cobrador?"

1. **Check that it landed in `MSP_PAGOS_VENTAS`** — `SELECT IMPTE_DOCTO_CC_ID, CANCELADO, UPDATED_AT, TX_ID FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`. If the row is absent, the recompute procedure failed — check `MSP_SALDOS_ERRORS` for the cargo id.

2. **Check that it landed in the changelog** — `SELECT SEQ_ID, TX_ID, COMMIT_AT FROM MSP_PAGOS_CHANGELOG WHERE IMPTE_DOCTO_CC_ID = ? ORDER BY SEQ_ID DESC`. If absent, the procedure exit path that should have appended (mig 23) did not run for this row — likely a `WHEN ANY DO` swallowed the upsert, see `MSP_SALDOS_ERRORS`.

3. **Check the current watermark** — `SELECT COALESCE(MIN(MON$TRANSACTION_ID), 9223372036854775807) FROM MON$TRANSACTIONS WHERE MON$STATE = 1`. Compare to the row's `TX_ID`. If `TX_ID >= watermark`, the row is correctly excluded right now; the watermark will advance when the holding tx commits. If you see a watermark stuck far below the current `MON$TRANSACTION_ID`, find the long-running tx: `SELECT * FROM MON$TRANSACTIONS WHERE MON$STATE = 1 ORDER BY MON$TRANSACTION_ID LIMIT 5` — the lowest is the culprit. Most often this is an open Microsip GUI form.

4. **Check the listener's cursor** (log line `fb_event_listener.published_ids`): if the `to_seq` is far behind the changelog's `MAX(SEQ_ID)`, the listener fell behind. A reconnect (kill the Firebird event TCP session) will trigger the synthetic `[]int{}` publish + cursor-sync recovery.

5. **Force a cursor sync from the client** by toggling network or pulling-to-refresh. The cursor sync is the authoritative safety net; if the row is in the cache, it WILL eventually arrive.

### "¿Cuánto pesa el changelog?"

```sql
SELECT
  (SELECT COUNT(*) FROM MSP_PAGOS_CHANGELOG)  AS pagos_n,
  (SELECT COUNT(*) FROM MSP_SALDOS_CHANGELOG) AS saldos_n,
  (SELECT MIN(COMMIT_AT) FROM MSP_PAGOS_CHANGELOG) AS pagos_oldest,
  (SELECT MIN(COMMIT_AT) FROM MSP_SALDOS_CHANGELOG) AS saldos_oldest
FROM RDB$DATABASE;
```

In steady state with the pruner running, `*_oldest` is within 7 days of `now`. If older rows linger, check the API logs for `cobranza.changelog_prune_failed`.

## What this sprint did NOT change

- **Cursor sync stays.** It is the safety net and the path of last resort for offline → online recovery, listener outages, and client misbehavior.
- **Multi-instance API.** If the API ever scales to multiple instances, a Postgres `LISTEN/NOTIFY` (or equivalent) layer in front of the eventbus will be needed so all instances receive the same wake-up. Out of scope; single-instance API today.
- **Drop of `syncLagSeconds`.** Replaced by a 1 s clock-skew margin (`syncClockSkewSeconds`) + `TX_ID < watermark`. The lag window's two responsibilities (clock skew + in-flight tx) are now split: skew handled by 1 s margin, in-flight handled by watermark.

## References

- `internal/cobranza/ports/outbound/changelog.go` — port definition.
- `internal/cobranza/infra/ventfb/changelog_repo.go` — Firebird adapter.
- `internal/cobranza/infra/ventfb/watermark.go` — `MinActiveTransactionID` helper.
- `internal/cobranza/infra/ventfb/fb_event_listener.go` — listener with changelog cursor + watermark.
- `internal/cobranza/infra/ventfb/page_helpers.go` — `serverNowAndWatermark` for cursor sync.
- `internal/cobranza/infra/cobranzahttp/handlers_sync_by_ids.go` — by-ids endpoints.
- `internal/cobranza/app/reconcile.go` — changelog pruner (1 h ticker).
- `internal/cobranza/infra/ventfb/money_integrity_property_test.go` — scenario J property test (200 rapid checks).
