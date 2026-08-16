# Task 13 Report — Composition root (fx) wiring: LLM client, generator, narrativa repo, worker

## Status

COMPLETE. All pre-commit hooks pass. Commit `3c87f51`.

## Changes

### New file: `cmd/api/llm_wiring.go`

Single provider `provideLLMClient(cfg *config.Config) platformllm.Client` — mirrors `provideMeilisearchClient`. Returns the disabled stub when `LLM_ENABLED=false` (default).

### Modified: `cmd/api/analytics_wiring.go`

- Added imports: `analyticsllm` (alias for `internal/analytics/infra/llm`) and `platformllm` (alias for `internal/platform/llm`), per `.golangci.yml` importas rules.
- Extended `provideAnalyticsService` with two new params (`narrativaRepo analyticsoutbound.NarrativaRepo`, `cfg *config.Config`) and chained `.WithNarrativa(narrativaRepo, cfg.LLM.Enabled)`.
- Added four new functions:
  - `provideAnalyticsNarrativeGenerator(client, cfg) analyticsoutbound.NarrativeGenerator`
  - `provideAnalyticsNarrativaRepo(r *analyticsfb.Repo) analyticsoutbound.NarrativaRepo`
  - `provideAnalyticsNarrativaWorker(svc, repo, gen, clock, cfg, logger) *analyticsapp.NarrativaWorker`
  - `registerAnalyticsNarrativaWorkerLifecycle(lc, w)`

### Modified: `cmd/api/main.go`

- Added to `fx.Provide(...)` analytics block: `provideAnalyticsNarrativaRepo`, `provideLLMClient`, `provideAnalyticsNarrativeGenerator`, `provideAnalyticsNarrativaWorker`.
- Added to `fx.Invoke(...)`: `registerAnalyticsNarrativaWorkerLifecycle` (after `registerAnalyticsRefreshWorkerLifecycle`).

## Verification

Commands run:

```
go build ./...                                          # clean (no output)
go vet ./cmd/... ./internal/analytics/...              # clean (no output)
golangci-lint run ./cmd/... ./internal/analytics/...   # 0 issues
go test ./cmd/api/...                                   # ok
```

**Runtime fx graph check:** built binary, sourced `.env`, ran `./msp-api-final serve` for 4 seconds, then killed. Observed log lines (in order):

```
llm.disabled: LLM features degraded; set LLM_ENABLED=true to activate
lifecycle: starting  component=analytics-narrativa-worker
narrativa_worker.disabled
lifecycle: started   component=analytics-narrativa-worker
...
lifecycle: stopping  component=analytics-narrativa-worker
lifecycle: stopped   component=analytics-narrativa-worker
```

No fx "missing type" or "provide error" messages. The app reached the HTTP server start phase cleanly, then stopped when killed. The disabled-by-default invariant holds: `narrativa_worker.disabled` confirms no goroutine was launched and no LLM call was made.

## Notes / Concerns

- `NewNarrativaWorker` accepts the `pulsoLoader` interface (unexported) as its first param. `*Service` satisfies it via `candidatoYPulso`. The fx provider passes `*analyticsapp.Service` directly — this compiles and wires correctly because `*Service` implements `pulsoLoader` at the concrete level and the fx provider is in package `main`, not `app`. No lint issue was flagged.
- No forced LLM enablement anywhere. All three code paths (build, test, runtime) confirm `LLM_ENABLED=false` is safe to ship.

## Commit

`3c87f51 feat(analytics): wiring fx del cliente LLM, generador y worker de narrativa`
