# Task 12 Report — Read-path narrativa wiring

## Status
DONE. Build clean, all tests green, lint 0 issues.

## Changes

### `internal/analytics/app/service.go`
- Added `narrativaRepo outbound.NarrativaRepo` and `llmEnabled bool` fields to `Service`.
- Added `WithNarrativa(repo outbound.NarrativaRepo, enabled bool) *Service` chaining setter (mirrors `WithLogger`). Default zero values preserve current behavior — all existing `NewService` callers and tests unchanged.

### `internal/analytics/app/narrativa_read.go` (new)
- `aplicarNarrativa(ctx, clienteID, *comp)`: serves cached narrativa on fresh hit; lazily enqueues on miss/stale when LLM enabled; all failures Warn-log and degrade silently.
- `etiquetasDe([]string) []string`: resolves catalog codes to Spanish labels via `EtiquetaDe`; drops unknowns; returns nil for empty input.

### `internal/analytics/app/pulso_query.go`
- `ObtenerPulsoCliente`: added `s.aplicarNarrativa(ctx, c.ClienteID(), &comp)` between `computePulso` and the `ToClientePulsoContract` return.
- `ObtenerPulsosClientes`: untouched.

### `internal/analytics/app/export_test.go`
- Added `ExportAplicarNarrativa(ctx, s, clienteID, comp)` (ctx first, per revive linter rule).

### `internal/analytics/app/narrativa_read_test.go` (new)
10 test cases:
1. `TestAplicarNarrativa_NoRepo` — no-op when repo nil
2. `TestAplicarNarrativa_FreshHit` — serves Narrativa + resolves Spanish RasgosIA
3. `TestAplicarNarrativa_UnknownCodeDropped` — unknown code silently dropped
4. `TestAplicarNarrativa_NegativeCacheHit` — matching hash, empty Texto → stays empty, no enqueue
5. `TestAplicarNarrativa_StaleEnabled` — stale hash + enabled → enqueues with CURRENT hash
6. `TestAplicarNarrativa_MissEnabled` — empty repo + enabled → enqueues
7. `TestAplicarNarrativa_MissDisabled` — empty repo + disabled → no enqueue
8. `TestObtenerPulsoCliente_NarrativaEndToEnd` — full roundtrip: miss→enqueue; seed row; second call→serve
9. `TestObtenerPulsosClientes_NeverEnqueues` — LIST path: 0 enqueues, empty narrativa fields

## Verification
```
go build ./...           → clean
go test ./internal/analytics/... → all ok (9 packages)
golangci-lint run ./internal/analytics/... → 0 issues
```

## Concerns
None. The negative-cache path (row with matching hash but empty Texto) correctly halts re-enqueue — the hash match is the gate, not the content of Texto.
