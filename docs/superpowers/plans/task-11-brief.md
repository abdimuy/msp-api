# Task 11 — NarrativaWorker (background generation, serialized, disabled-by-default)

## Where this fits
A background worker that drains the `MSP_AN_NARRATIVA_PENDIENTE` queue: for each queued client it recomputes the pulso, generates the narrativa via the LLM, validates it, and caches the result. Mirrors the existing `RefreshWorker` lifecycle EXACTLY. When the LLM is disabled (default), it is a no-op. It is serialized (one client at a time — local CPU is the constraint). NOTHING calls a real model: tests use the fake generator (Task 8) + in-memory repo (Task 10).

## Read first
- `internal/analytics/app/refresh_worker.go` — copy its lifecycle pattern VERBATIM (struct fields mu/running/cancel/done, `NewRefreshWorker`, `Start`/`Stop`/`loop`/`tick`, `RefreshWorkerConfig.applyDefaults`). Your worker is the same shape.
- `internal/analytics/app/refresh_worker_test.go` — copy its test style (call `tick(ctx)` directly; no goroutine-timing flakiness).
- `internal/analytics/ports/outbound/narrative_generator.go` + `narrativa_repo.go` (Task 6).
- `internal/analytics/app/narrativa_validate.go` (Task 9) — `ValidarNarrativa`.
- `internal/analytics/app/narrativa_hash.go` (Task 5) — `NarrativaInputHash`.
- `internal/analytics/app/narrativa_input.go` + `pulso_query.go` `candidatoYPulso` (Task 10) — `buildNarrativeInput`, `candidatoYPulso`.
- `internal/analytics/app/rasgos_catalogo.go` (Task 4) — `CatalogoRasgos`.
- `internal/platform/llm` (Task 3) — `IsTransient`, `ErrLLMDisabled`.
- `internal/analytics/infra/narrativamem` (Task 10) — in-memory repo fake for tests.

## File — `internal/analytics/app/narrativa_worker.go`

### Dependency seam (for testability)
The worker must NOT require a fully-wired `*Service` in unit tests. Depend on a minimal interface that `*Service` already satisfies via the Task-10 method:
```go
// pulsoLoader loads a candidate and its computed pulso. *Service satisfies it.
type pulsoLoader interface {
	candidatoYPulso(ctx context.Context, clienteID int) (*domain.WinbackCandidato, analytics.PulsoComputado, error)
}
```

### Config + struct
```go
type NarrativaWorkerConfig struct {
	Interval  time.Duration // default 1m
	BatchSize int           // default 10
	Model     string        // persisted in NARRATIVA.MODELO (from config.LLM.Model)
	Enabled   bool          // from config.LLM.Enabled; false ⇒ worker no-op
}
func (c *NarrativaWorkerConfig) applyDefaults() // Interval<=0→time.Minute; BatchSize<=0→10

type NarrativaWorker struct {
	loader pulsoLoader
	repo   outbound.NarrativaRepo
	gen    outbound.NarrativeGenerator
	clock  outbound.Clock
	cfg    NarrativaWorkerConfig
	logger *slog.Logger
	// mu/running/cancel/done — same as RefreshWorker
}
func NewNarrativaWorker(loader pulsoLoader, repo outbound.NarrativaRepo, gen outbound.NarrativeGenerator, clock outbound.Clock, cfg NarrativaWorkerConfig, logger *slog.Logger) *NarrativaWorker
```
`var _ lifecycle.Hooks = (*NarrativaWorker)(nil)` if RefreshWorker uses that idiom.

### Start/Stop/loop — copy RefreshWorker
EXCEPT: `Start` returns nil WITHOUT launching the goroutine when `!w.cfg.Enabled` (log once at info: "narrativa_worker.disabled"). Otherwise identical to RefreshWorker (ticker, no immediate tick).

### tick(ctx) — serialized drain
```
pend, err := w.repo.ListarPendientes(ctx, w.cfg.BatchSize)   // err → log + return
for _, p := range pend {                                      // SERIAL, no goroutines
    w.procesarUno(ctx, p.ClienteID)
}
```
### procesarUno(ctx, clienteID) — one client
1. `c, comp, err := w.loader.candidatoYPulso(ctx, clienteID)`. On error: if it's a not-found error → log + `BorrarPendiente` (clean the queue, candidate gone) and return; on other error → log + return (leave in queue, retry next tick).
2. `hash := NarrativaInputHash(comp)`.
3. `in := buildNarrativeInput(c, comp, CatalogoRasgos)`.
4. `out, err := w.gen.Generar(ctx, in)`. On error:
   - log; if `llm.IsTransient(err)` → return (LEAVE in queue, retry next tick); else (permanent, incl `errors.Is(err, llm.ErrLLMDisabled)`) → `BorrarPendiente` + return (drop; read-path re-enqueues on next view if still missing). Do NOT upsert on any Generar error.
5. `v := ValidarNarrativa(out, comp)`.
6. `n := domain.Narrativa{ClienteID: clienteID, Texto: v.Texto, Rasgos: v.Rasgos, InputHash: hash, Modelo: w.cfg.Model, GeneradaEn: w.clock.Now().UTC()}`. Persist exactly what the validator returns — no special-casing. On a passing check `v.Texto` is the model paragraph and `v.Rasgos` the validated codes; on fallback `v.Texto==""` and `v.Rasgos` empty (a negative cache keyed by InputHash that prevents pointless re-generation until the facts change). Always upsert (even the empty fallback row).
7. `if err := w.repo.UpsertNarrativa(ctx, n); err != nil { log; return }` (leave in queue → retry).
8. `w.repo.BorrarPendiente(ctx, clienteID)` (log on error, non-fatal).

Errors never kill the loop (log + continue), exactly like RefreshWorker.tick.

## Tests — `internal/analytics/app/narrativa_worker_test.go`
Use a **fake `pulsoLoader`** (returns a canned `*domain.WinbackCandidato` + `analytics.PulsoComputado`; build the candidate with whatever constructor/test-builder existing app tests use), `infra/narrativamem.New()` as the repo, and `llmfake.Generator` as the generator. Drive `w.tick(ctx)` directly. NOTE: the `narrativamem` fake orders `ListarPendientes` by clienteID (NOT by enqueue time like the real Firebird adapter) — do NOT assert FIFO/enqueue-order semantics in worker tests; the worker processes the whole batch regardless of order. Cover:
- **Generates + validates + caches:** enqueue clienteID; loader returns low-risk comp; fake gen returns `{narrativa: <valid>, rasgos: ["loyal_but_stagnant","NOT_A_CODE","loyal_but_stagnant"]}` → after tick: repo has a narrativa row with Texto=valid, Rasgos=["loyal_but_stagnant"] (invalid dropped + deduped), InputHash==NarrativaInputHash(comp), Modelo==cfg.Model; queue empty.
- **Disabled is a no-op:** cfg.Enabled=false → `Start` does not launch a goroutine (assert it returns nil and a subsequent tick, if called, would still be inert — OR simply assert Start returns nil and running flag is false). Keep it simple and deterministic.
- **Transient gen error → left in queue:** fake gen returns a transient error (wrap so `llm.IsTransient` is true) → after tick: no narrativa row, pendiente STILL present.
- **Permanent gen error → dropped:** fake gen returns `llm.ErrLLMDisabled` (or a permanent error) → after tick: no narrativa row, pendiente REMOVED.
- **Fallback path:** loader returns comp with `TierRiesgo="CRITICO"`; fake gen returns a contradictory "es un buen pagador..." narrativa → after tick: row persisted with EMPTY Texto and empty Rasgos (negative cache), InputHash==hash, queue empty.
- **Candidate not found:** loader returns a not-found error → after tick: pendiente removed, no row.
Pristine output, no flakiness.

## Constraints
- CLAUDE.md §2: `app` may import `domain`, `ports/outbound`, root `analytics`, `platform/llm`, `platform/lifecycle`, stdlib. §3: English identifiers/comments; log keys snake_case English.
- Serialized — NO per-client goroutines. Lifecycle identical to RefreshWorker (idempotent Start/Stop, drains on Stop).
- Disabled by default ⇒ no goroutine, no model call, zero cost.

## Verification
- `go build ./...`
- `go test ./internal/analytics/...`
- `golangci-lint run ./internal/analytics/...`

## Commit
`feat(analytics): worker de fondo para generar narrativa IA (no-op si LLM apagado)`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-11-report.md`. Reply ≤15 lines: status, commit SHA+subject, one-line test summary, concerns, report path.
