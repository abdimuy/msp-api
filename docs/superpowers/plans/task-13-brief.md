# Task 13 — Composition root wiring (fx): LLM client, generator, narrativa repo, worker

## Where this fits
Wires the Fase-2 pieces into the running app via uber/fx. With `LLM_ENABLED=false` (default) the platform factory returns the disabled client and the worker is a no-op — so this wiring is safe to ship without a model. Mirrors the existing analytics + meilisearch wiring exactly.

## Read first
- `cmd/api/analytics_wiring.go` — existing analytics providers (`provideAnalyticsRepo`, `provideAnalyticsService`, `provideAnalyticsRefreshWorker`, `registerAnalyticsRefreshWorkerLifecycle`). Mirror these shapes.
- `cmd/api/search_wiring.go` — `provideMeilisearchClient(cfg *config.Config) (platformmeili.Client, error)` — the template for the LLM client provider.
- `cmd/api/main.go` — the `fx.Provide(...)` list (analytics section ~lines 144-151, meilisearch ~152-153) and the `fx.Invoke(...)` list (lifecycle registrations ~lines 191-192). You will add providers + one invoke here.
- `internal/platform/llm` (Task 3) — `NewClient(cfg config.LLM) Client`.
- `internal/analytics/infra/llm` (Task 8) — `NewGenerator(client, model) *Generator` (note the package alias; this infra package is `llm`, platform is also `llm` — alias both like Task 8 did, e.g. `platformllm` + `analyticsllm`).
- `internal/analytics/app` — `Service.WithNarrativa(repo, enabled)` (Task 12), `NewNarrativaWorker(...)` + `NarrativaWorkerConfig` (Task 11).
- `internal/analytics/infra/analyticsfb` — `Repo` now also implements `outbound.NarrativaRepo` (Task 7).

## Changes — `cmd/api/analytics_wiring.go` (+ a new `cmd/api/llm_wiring.go` if cleaner)

1. **LLM client provider** (mirror meilisearch):
```go
func provideLLMClient(cfg *config.Config) platformllm.Client {
	return platformllm.NewClient(cfg.LLM)
}
```
(`NewClient` returns `Client` with no error — disabled stub when `!cfg.LLM.Enabled`.)

2. **NarrativeGenerator provider:**
```go
func provideAnalyticsNarrativeGenerator(client platformllm.Client, cfg *config.Config) analyticsoutbound.NarrativeGenerator {
	return analyticsllm.NewGenerator(client, cfg.LLM.Model)
}
```

3. **NarrativaRepo provider** (expose the concrete Repo as the new port, like `provideAnalyticsWinbackRepo`):
```go
func provideAnalyticsNarrativaRepo(r *analyticsfb.Repo) analyticsoutbound.NarrativaRepo {
	return r
}
```

4. **Service provider — add narrativa:** extend `provideAnalyticsService` params with `narrativaRepo analyticsoutbound.NarrativaRepo, cfg *config.Config` and chain the setter:
```go
return analyticsapp.NewService(repo, micro, clock, txRunner).
	WithNarrativa(narrativaRepo, cfg.LLM.Enabled)
```
(Keep the existing `WithLogger` chaining if it's already there; if not, don't add it.)

5. **Narrativa worker provider:**
```go
func provideAnalyticsNarrativaWorker(
	svc *analyticsapp.Service,
	repo analyticsoutbound.NarrativaRepo,
	gen analyticsoutbound.NarrativeGenerator,
	clock analyticsoutbound.Clock,
	cfg *config.Config,
	logger *slog.Logger,
) *analyticsapp.NarrativaWorker {
	return analyticsapp.NewNarrativaWorker(svc, repo, gen, clock,
		analyticsapp.NarrativaWorkerConfig{Model: cfg.LLM.Model, Enabled: cfg.LLM.Enabled}, logger)
}
```
(`*Service` satisfies the worker's loader seam. If lint flags the unexported loader param type on the exported `NewNarrativaWorker`, the fix belongs in Task 11's code — note it in your report rather than working around it here.)

6. **Lifecycle registration:**
```go
func registerAnalyticsNarrativaWorkerLifecycle(lc fx.Lifecycle, w *analyticsapp.NarrativaWorker) {
	lifecycle.Append(lc, "analytics-narrativa-worker", w)
}
```

## Changes — `cmd/api/main.go`
- Add to the analytics `fx.Provide(...)` block: `provideLLMClient`, `provideAnalyticsNarrativeGenerator`, `provideAnalyticsNarrativaRepo`, `provideAnalyticsNarrativaWorker` (and ensure `provideAnalyticsService` still resolves with its new params — fx wires by type).
- Add to the `fx.Invoke(...)` block: `registerAnalyticsNarrativaWorkerLifecycle`.
- Add necessary imports (`platformllm`, `analyticsllm`) with the chosen aliases.

## Constraints
- CLAUDE.md §2/§3. Mirror existing provider/Invoke conventions exactly (doc comments included).
- Disabled-by-default must hold: with `LLM_ENABLED=false`, the app starts, the worker is a no-op, and `ObtenerPulsoCliente` serves cache-only (never enqueues). Do not force-enable anything.

## Verification — this is the integration moment for the whole backend graph
- `go build ./...`
- `go vet ./...`
- `golangci-lint run ./cmd/... ./internal/analytics/...`
- **fx graph resolves:** run the app's fx dependency check if one exists (e.g. `go test ./cmd/api/...` if there's a wiring test, or `fx.ValidateApp` if used). If there's no such test, at minimum confirm `go build ./cmd/api/...` links and, if feasible, run the binary with `LLM_ENABLED=false` and the rest of the env unset enough that it fails only on a missing DB (NOT on an fx provide/missing-type error). Do NOT start a real server against prod; just confirm fx wiring resolves (no "missing type" panic). Report exactly what you ran.

## Commit
`feat(analytics): wiring fx del cliente LLM, generador y worker de narrativa`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-13-report.md` (state how you confirmed the fx graph resolves). Reply ≤15 lines: status, commit SHA+subject, one-line verification summary, concerns, report path.
