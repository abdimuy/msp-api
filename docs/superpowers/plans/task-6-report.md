# Task 6 Report — Outbound ports: NarrativeGenerator + NarrativaRepo

## Status
DONE. Both port files created, build clean, lint clean, commit pushed through all pre-commit hooks.

## Commit
`36581be feat(analytics): puertos NarrativeGenerator y NarrativaRepo para lectura IA`

## Files created
- `internal/analytics/ports/outbound/narrative_generator.go` — `NarrativeInput`, `NarrativeOutput`, `NarrativeGenerator`
- `internal/analytics/ports/outbound/narrativa_repo.go` — `NarrativaRow`, `PendienteRow`, `NarrativaRepo`

## Build + lint
`go build ./...` → 0 errors. `golangci-lint run ./internal/analytics/...` → 0 issues. All 8 lefthook pre-commit checks passed.

## Field-name verification
Every field in `NarrativeInput` was confirmed against source types before writing:
- `ClienteID`, `Nombre`, `Zona`, `Frecuencia`, `Monetary`, `Saldo`, `CadenciaDias` — from `WinbackCandidato` accessor methods (confirmed in `analytics_contracts_mapper.go` lines 19-27, 97).
- `Segmento`, `TierRiesgo`, `EstadoPago`, `BandaCredito`, `ScoreCredito`, `BandaRecompra`, `ScoreRecompra`, `BandaCLV`, `RecenciaDias`, `DiasAtrasoProm`, `PctPagosATiempo`, `MontoCLV`, `CreditoDrivers`, `RecompraDrivers`, `CLVDrivers`, `CreditoResumen`, `RecompraResumen`, `CLVResumen` — all present on `PulsoComputado` struct (lines 51-70 of `analytics_contracts_mapper.go`).

No field-name corrections needed. Brief matched source types exactly.

## Import compliance (CLAUDE.md §2)
- `narrative_generator.go` imports: `context`, `github.com/shopspring/decimal`, `domain` — no `app` or `infra`.
- `narrativa_repo.go` imports: `context`, `domain` — no `app` or `infra`.
No cycles introduced.

## Concerns
None. Pure interface/struct declarations; no implementations. Tasks 7 (NarrativaRepo Firebird adapter) and Task 8 (NarrativeGenerator LLM adapter + fake) can now depend on these port contracts.
