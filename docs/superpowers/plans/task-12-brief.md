# Task 12 — Read-path narrativa wiring (lazy enqueue-on-view, cache serve)

## Where this fits
The single-client read-path serves the cached narrativa when fresh, and lazily enqueues generation when missing/stale. The LIST path is NEVER touched. Degrades to nothing when the narrativa repo is absent or the LLM is disabled. Uses the helpers from Task 10 and the hash from Task 5; the worker (Task 11) drains what this enqueues.

## Read first
- `internal/analytics/app/service.go` — `Service` struct + `NewService` + the `WithLogger` chaining-setter pattern (you'll add `WithNarrativa` the same way).
- `internal/analytics/app/pulso_query.go` — `ObtenerPulsoCliente` (now uses `s.computePulso` after Task 10) and `ObtenerPulsosClientes`.
- `internal/analytics/app/narrativa_hash.go` (Task 5) — `NarrativaInputHash`.
- `internal/analytics/app/rasgos_catalogo.go` (Task 4) — `EtiquetaDe`.
- `internal/analytics/ports/outbound/narrativa_repo.go` (Task 6) — `NarrativaRepo`, `NarrativaRow`.
- `internal/analytics/infra/narrativamem` (Task 10) — in-memory repo fake for tests.

## Changes — all in `app`

### 1. `Service` struct + setter (service.go)
Add two fields:
```go
	narrativaRepo outbound.NarrativaRepo // nil ⇒ read-path serves no AI narrativa
	llmEnabled    bool                   // false ⇒ never enqueue (read stays cache-only)
```
Add a chaining setter mirroring `WithLogger` (default zero values keep current behavior — no change to existing `NewService` callers or tests):
```go
// WithNarrativa enables read-time narrativa serve + lazy enqueue. When repo is
// nil the read-path serves no AI narrativa; when enabled is false it serves the
// cache but never enqueues new generation. Returns s for chaining.
func (s *Service) WithNarrativa(repo outbound.NarrativaRepo, enabled bool) *Service {
	s.narrativaRepo = repo
	s.llmEnabled = enabled
	return s
}
```

### 2. `aplicarNarrativa` helper (pulso_query.go or a new narrativa_read.go in app)
```go
// aplicarNarrativa serves the cached narrativa for clienteID into comp when the
// cache is fresh (InputHash matches the current facts), or lazily enqueues
// generation on miss/stale when the LLM is enabled. All failures degrade
// silently — the ficha simply shows no AI reading. Never returns an error.
func (s *Service) aplicarNarrativa(ctx context.Context, clienteID int, comp *analytics.PulsoComputado)
```
Logic:
- If `s.narrativaRepo == nil` → return (no-op).
- `hash := NarrativaInputHash(*comp)`.
- `row, err := s.narrativaRepo.GetNarrativa(ctx, clienteID)`. On err → `logger.WarnContext` + return (degrade).
- If `row != nil && row.InputHash == hash` (fresh hit): set `comp.Narrativa = row.Texto`; `comp.RasgosIA = etiquetasDe(row.Rasgos)`; return. (When the row is a negative-cache fallback, `row.Texto==""` and `row.Rasgos` empty → comp stays effectively empty, which is correct: no IA section, no re-enqueue.)
- Else (miss or stale): if `s.llmEnabled` → `s.narrativaRepo.Encolar(ctx, clienteID, hash)` (Warn-log on error, non-fatal). Do NOT block; comp's narrativa fields stay empty this view.

```go
// etiquetasDe resolves validated trait codes to their Spanish display labels,
// dropping any code no longer in the catalog.
func etiquetasDe(codes []string) []string // uses EtiquetaDe; skips "" results; returns nil for empty input
```

### 3. Call it from `ObtenerPulsoCliente` ONLY
After `comp := s.computePulso(c, now, p90)` and before `return analytics.ToClientePulsoContract(c, comp), nil`:
```go
	s.aplicarNarrativa(ctx, c.ClienteID(), &comp)
```
**Do NOT add anything to `ObtenerPulsosClientes`** — the LIST path must never serve or enqueue narrativa. Leave it exactly as is.

## Tests — `internal/analytics/app/narrativa_read_test.go`
Unit-test `aplicarNarrativa` directly (craft a `PulsoComputado` + clienteID; use `narrativamem.New()`):
- **No repo:** Service without `WithNarrativa` → `aplicarNarrativa` leaves `comp.Narrativa==""`, `comp.RasgosIA==nil`, and (obviously) doesn't panic.
- **Fresh hit:** seed repo with a row whose `InputHash == NarrativaInputHash(comp)`, `Texto="..."`, `Rasgos=["loyal_but_stagnant","churn_risk"]` → comp.Narrativa set; comp.RasgosIA == the Spanish labels (["Leal pero estancado","Riesgo de fuga"]).
- **Unknown code dropped:** row.Rasgos `["loyal_but_stagnant","gone_from_catalog"]` → RasgosIA == ["Leal pero estancado"].
- **Negative-cache hit:** row with matching hash but `Texto=""`, `Rasgos=[]` → comp stays empty AND no enqueue happens (assert queue empty).
- **Stale + enabled:** row with a DIFFERENT InputHash, `WithNarrativa(repo, true)` → comp stays empty AND repo queue now contains clienteID with the CURRENT hash.
- **Miss + enabled:** empty repo, enabled → queue contains clienteID with current hash.
- **Miss + disabled:** empty repo, `WithNarrativa(repo, false)` → queue stays empty, comp empty.
And one **end-to-end through `ObtenerPulsoCliente`** (build a Service with a fake `WinbackRepo` returning a candidate — reuse the existing service_test fakes/builders, + `WithNarrativa(narrativamem.New(), true)`): first call → miss → assert the queue got the client with some hash H; seed a narrativa row {InputHash:H, Texto:"lectura X", Rasgos:["cash_reliable"]}; second call → assert the returned `ClientePulsoContract.Narrativa=="lectura X"` and `RasgosIA==["Contado confiable"]`.
And assert **`ObtenerPulsosClientes` never enqueues**: with `WithNarrativa(repo,true)`, call the LIST for the same ids → repo queue stays empty and contract narrativa fields are empty.
Pristine output.

## Constraints
- CLAUDE.md §2/§3. LIST path untouched. All narrativa failures degrade silently (Warn-log, never error out the pulso read).
- Default (no `WithNarrativa`) preserves existing behavior exactly — existing tests must stay green.

## Verification
- `go build ./...`
- `go test ./internal/analytics/...`
- `golangci-lint run ./internal/analytics/...`

## Commit
`feat(analytics): read-path sirve narrativa cacheada y encola generación perezosa`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-12-report.md`. Reply ≤15 lines: status, commit SHA+subject, one-line test summary, concerns, report path.
