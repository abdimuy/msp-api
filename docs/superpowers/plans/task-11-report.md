# Task 11 Report — NarrativaWorker

## Status
DONE. All verification gates green.

## Commit
`3c23eb1 feat(analytics): worker de fondo para generar narrativa IA (no-op si LLM apagado)`

## Files created
- `internal/analytics/app/narrativa_worker.go` — worker implementation
- `internal/analytics/app/narrativa_worker_test.go` — 6 test cases

## Test summary
6/6 pass: generate+validate+cache (invalid trait dropped + deduped), disabled no-op (Start returns nil, running=false), transient gen error → left in queue, permanent gen error (ErrLLMDisabled) → dropped, fallback path → empty Texto + empty Rasgos row persisted (negative cache), candidate not found → removed from queue.

## Verification
- `go build ./...` — clean
- `go test ./internal/analytics/...` — all packages green (9 packages)
- `golangci-lint run ./internal/analytics/...` — 0 issues (fixed gofumpt trailing blank line + testifylint `assert.Empty` for empty-string check)

## pulsoLoader interface choice
Kept **unexported** (`pulsoLoader`). The brief called for this; `revive` only flags unexported returns on exported functions, not unexported parameters. The constructor `NewNarrativaWorker` accepts the unexported `pulsoLoader` interface, which seals it to the `app` package (no external caller can implement it) while remaining fully testable via `fakePulsoLoader` in the internal test file (`package app`). No lint warning was raised.

## Architecture notes
- Lifecycle mirrors `RefreshWorker` verbatim: mu/running/cancel/done, idempotent Start/Stop, ticker-driven loop, no immediate first tick.
- `Start` no-ops (logs `narrativa_worker.disabled`, returns nil) when `Enabled=false` — no goroutine, zero model calls.
- `procesarUno` follows the brief exactly: not-found → BorrarPendiente + return; transient Generar error → leave in queue; permanent → BorrarPendiente no upsert; success (incl. fallback empty row) → Upsert then BorrarPendiente.
- Tests drive `tick` directly (same pattern as `coverage_internal_test.go`), no goroutine-timing flakiness.
