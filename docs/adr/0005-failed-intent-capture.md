# ADR 0005 — Failed-intent capture for mutating ventas requests

- **Status:** Accepted
- **Date:** 2026-05-14
- **Decision drivers:** legal discovery for disputed offline-sync sales; ops debuggability when requests are lost silently; replay capability without requiring a separate event store.

## Context

The Android client queues sales offline and drains the queue over a sync connection when it comes back online. On each sync a subset of `POST /v2/ventas` requests fail — validation rejections, foreign-key violations, expired Firebase tokens, or transient server bugs. Today the server records only an access-log line: method, path, status code, latency. The original request payload is discarded after the handler returns.

This is acceptable for most failure modes, but not for disputed sales. If a customer claims a sale was captured on the device and never posted to the server, ops must be able to produce the original document for legal discovery. With no payload, they cannot. The only evidence is a timestamp in the access log and whatever the Android app retained locally — neither of which is under the company's control.

The existing auth-and-idempotency middleware stack already processes the request body before any handler runs. The gap is at the tail of that stack: there is nowhere to intercept a non-2xx response and persist the payload that caused it.

## Decision

Introduce `internal/platform/failedintent/` — a middleware component that intercepts every 4xx and 5xx response on mutating routes under `/v2/ventas` and persists the captured request to the `failed_intents` Postgres table.

### What is captured

Each row in `failed_intents` stores:

- `id` — UUID generated in Go.
- `received_at` — `time.Now()` set in Go, passed as a parameter (no SQL `DEFAULT`).
- `method`, `path`, `query` — from the request.
- `user_id` — the resolved `CurrentUser` from the auth middleware context, nullable (captures unauthenticated failures too).
- `idempotency_key` — the `Idempotency-Key` header value, nullable.
- `body` — the raw request body, capped at `DefaultBodyCapBytes` (256 KiB). Bodies that exceed the cap are stored truncated and `body_truncated` is set to `true`.
- `status_code` — the HTTP status code returned to the client.
- `error_code` — the structured error code from the response body, nullable.
- `status` — state-machine column: `new | replayed | resolved | ignored`.
- `resolved_at`, `resolved_by` — nullable, set when an admin closes the intent.
- `notes` — free-text field for the resolving admin.

### Architecture details

1. The capture middleware wraps the `http.ResponseWriter` with a recording writer that buffers the status code and response body. After the downstream handler chain returns, if the status code is 4xx or 5xx, it fires a synchronous `INSERT` via the `FailedIntentStore` port (a thin interface over `*pgxpool.Pool`).

2. A `SettableReplayDispatcher` holds an `atomic.Pointer[http.Handler]` that is populated by the composition root after the router is fully built. This breaks the dispatcher↔router↔handler dependency cycle without an init-time circular reference. The admin replay handler calls `dispatcher.Dispatch(w, r)` to re-run the original request through the live router.

3. Loop prevention: replayed requests carry an `X-Internal-Replay: 1` header. The capture middleware skips capture when this header is present, preventing a failed replay from generating a second intent row.

4. Placement in the middleware chain: the capture middleware sits inside the auth and idempotency layers. It therefore sees `CurrentUser` (which it reads from context to populate `user_id`) and any idempotency-cached response (a replayed request that hits the idempotency cache still resolves correctly).

5. The janitor is a `lifecycle.Hook` that runs hourly via the app's startup hooks. It issues `DELETE FROM failed_intents WHERE received_at < NOW() - INTERVAL '90 days'` (with the interval passed as a Go `time.Duration` parameter, never a SQL literal).

6. Admin endpoints live under `/v2/_admin/failed-intents` and are guarded by two permissions:
   - `failed_intents:ver` — required to list and inspect intents, including full body payloads.
   - `failed_intents:resolver` — required to mark an intent as `resolved` or `ignored`, and to trigger a replay.

7. Every successful capture emits `slog.Info("failedintent.captured", "id", …, "method", …, "path", …, "status_code", …, "user_id", …)`. Every failed capture attempt (store error) emits `slog.Error("failedintent.capture_failed", …)` with enough context to reconstruct the intent manually from the access log.

8. The schema migration is `000004_failed_intents` (up/down pair already present in `migrations/`). The down migration is a plain `DROP TABLE failed_intents`.

9. `DefaultBodyCapBytes = 262144` (256 KiB) and `DefaultRetention = 90 * 24 * time.Hour` are package-level constants in `internal/platform/failedintent/`. The environment variables `FAILED_INTENT_BODY_CAP_BYTES` and `FAILED_INTENT_RETENTION_DAYS` are defined in `.env.example` for operator visibility but are **not** wired into `internal/platform/config/config.go` in v1 — the wiring is a follow-up ticket. Until then, hardcoded defaults are used at the composition root.

10. The capture is synchronous within the request path. The extra cost is a bounded body read (already performed by the body-buffering writer) plus one Postgres `INSERT` — measured at tens of microseconds on loopback. This is acceptable given that failed requests are a small fraction of total traffic and each one already paid the cost of handler execution.

## PII trade-off

This is the load-bearing decision of the ADR.

We do **not** redact or summarize body payloads before storing them. The `failed_intents.body` column will contain customer names, addresses, phone numbers, amounts, and any other fields the Android client submitted.

Reasoning: the payload is the evidence. Redacting it before storage defeats the entire purpose of the feature — a legal dispute cannot be resolved with a hash or a summary. The capture exists specifically because the access log (which contains no payload) is insufficient for discovery.

Mitigations accepted in lieu of redaction:

- **Permission gating.** Every endpoint that returns a raw body — list, detail, replay — requires `failed_intents:ver`. This permission must be granted explicitly; it is not bundled with a general admin role by default.
- **Retention bound.** Rows are purged after 90 days. This limits the blast radius of a credential compromise: at most 90 days of failed-request payloads are exposed at any moment.
- **Admin token rotation policy.** The existing policy (rotate service credentials on personnel change) already covers this surface. No new policy is required.
- **Future escape hatch.** If Legal ever mandates a redact-by-default model, the resolution is to introduce a `failed_intents:ver_raw` permission and downgrade `failed_intents:ver` to return a redacted body by default. The split can be implemented without a schema migration. This is listed as a follow-up ticket below.

Anyone with `failed_intents:ver` should be considered to have access to customer PII. Grant accordingly.

## Retention

90 days. The janitor `lifecycle.Hook` runs hourly and issues a delete for rows with `received_at` older than the retention window. The retention duration is `failedintent.DefaultRetention`; the Go constant is the single source of truth for v1.

## Alternatives considered

### Log full payloads to disk

Extend the access logger to write the request body alongside the status code when the response is non-2xx.

Rejected. Log retention is ad-hoc (rotated by file size or age, not by business rule), logs are not queryable by `user_id` or `idempotency_key`, and there is no replay path. An ops engineer looking up a disputed sale would need to grep through potentially gigabytes of log files. The legal team cannot be pointed at a log file during discovery.

### Async outbox with later persistence

Buffer the payload in memory and enqueue it to the outbox for async write to Postgres.

Rejected. The async path introduces a race on server shutdown — if the process exits before the outbox drains, the capture is lost. Audit-grade evidence must be persisted synchronously within the same request, where the write either succeeds or the slog error fires immediately. ADR-0001 already documents that the outbox is best-effort by design; the failed-intent store must not inherit that characteristic.

### Capture only error code and summary

Store `status_code`, `error_code`, and a fixed-length truncation of the first N bytes.

Rejected. This loses the full document needed for dispute resolution. The whole point is to be able to reconstruct exactly what the client sent. A truncated summary is no better than the access log we already have.

## Consequences

### Positive

- Complete audit trail for every failed mutating request on `/v2/ventas`.
- Replay capability allows ops to re-run a failed request against the live router after a root cause is fixed (e.g., a data integrity issue is corrected), without requiring the Android client to resend.
- Single source for ops debugging: `GET /v2/_admin/failed-intents?status=new` shows all open failures.

### Negative

- PII surface is widened. Anyone with `failed_intents:ver` can read customer names, addresses, and transaction amounts from captured bodies. This is a deliberate and documented trade-off.
- Storage growth: up to 256 KiB per captured intent in Postgres. At an estimated 100 failed requests per day this is ~25 MiB per day before compression, well within the capacity of the existing Postgres instance.
- An extra middleware hop on every mutating request under `/v2/ventas`. The cost is bounded (body read + `ResponseWriter` wrap, ~tens of microseconds) and paid only when the response is non-2xx.

### Neutral

- The schema migration `000004_failed_intents` is fully reversible via `DROP TABLE`. Rolling back v1 of this feature requires no data backfill.
- No changes to the existing auth, idempotency, or logging middleware — the capture sits alongside them, not inside them.

## Follow-up tickets (out of scope for v1)

The following items were deliberately deferred to keep v1 focused:

- Wire `FAILED_INTENT_BODY_CAP_BYTES` and `FAILED_INTENT_RETENTION_DAYS` into `internal/platform/config/config.go` so operators can tune them without recompiling.
- Make `Idempotency-Key` a required header on `POST /v2/ventas` and `PATCH /v2/ventas/*` with a 7-day TTL, so every intent is keyed and deduplication is guaranteed across replays.
- `GET /v2/me/failed-intents` — self-service endpoint for authenticated end-users to inspect their own failed requests without requiring an admin credential.
- Prometheus counter `failedintent_captured_total{method, path, status_code}` plus an alert when `count(status='new') > N` exceeds a threshold, indicating an elevated failure rate.
- `failed_intent_replays` sub-table to audit each replay attempt independently (replayed_at, replayed_by, replay_status_code, replay_error_code), decoupled from the parent intent's status column.
- `failed_intents:ver_raw` permission to support a redaction-by-default model where `failed_intents:ver` returns a scrubbed body and only `ver_raw` returns the original.
