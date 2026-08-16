# Task 14 — clientes PulsoDTO + mapper: expose narrativa + rasgos_ia

## Where this fits
`clientes` owns the ficha HTTP DTO. The analytics contract `ClientePulsoContract` now carries `Narrativa string` + `RasgosIA []string` (Task 5). This task surfaces them in the ficha JSON. `clientes` only maps contract → DTO (CLAUDE.md §2 — it does not compute anything).

## Read first
- `internal/clientes/infra/clienteshttp/dto.go` — `PulsoDTO` (has the Fase-1 `CreditoResumen`/`RecompraResumen`/`CLVResumen` fields — add the new ones near them).
- `internal/clientes/infra/clienteshttp/dto_mapper.go` — `toFichaDTO`, the `dto.Pulso = &PulsoDTO{...}` block (~line 126) that maps from `p analytics.ClientePulsoContract`.
- `internal/analytics/analytics_contracts.go` — confirm `ClientePulsoContract.Narrativa` (string) + `RasgosIA` ([]string) exist (Task 5).

## Changes

### 1. `PulsoDTO` (dto.go) — add two fields after the Fase-R titular fields
```go
	// ─── Lectura del analista (IA) — Fase 2 ──────────────────────────────────────
	Narrativa string   `json:"narrativa"  doc:"Lectura del analista generada por IA (síntesis + acción interna); vacío si no disponible o LLM apagado"`
	RasgosIA  []string `json:"rasgos_ia"  doc:"Rasgos conductuales asignados por IA (etiquetas en español); vacío si ninguno"`
```
Match the existing field-tag/doc style exactly (the `doc:` annotations are Huma OpenAPI docs). The JSON keys MUST be `narrativa` and `rasgos_ia`.

### 2. `toFichaDTO` (dto_mapper.go) — map both from the contract
In the `&PulsoDTO{...}` literal, add:
```go
			Narrativa: p.Narrativa,
			RasgosIA:  p.RasgosIA,
```
(Where `p` is the `ClientePulsoContract`.) No transformation — pass through.

## Tests
- If there's an existing `PulsoDTO`/`toFichaDTO` mapper test, extend it to assert `Narrativa` and `RasgosIA` map through from a contract that has them set (and that they are empty when the contract has them empty).
- **OpenAPI snapshot:** `openapi_test.go` likely golden-tests the generated spec. Adding two documented fields will change it — run `go test ./internal/clientes/...`, and if a golden/snapshot assertion fails ONLY because of the two new fields, update the snapshot the way the test intends (look for an `-update` flag or a regenerate step in that test file; follow the repo's established way of regenerating it). Do NOT hand-edit the golden in a way that diverges from what the generator produces. Confirm the diff is limited to the two new fields.
Pristine output.

## Constraints
- CLAUDE.md §2: `clientes` maps the analytics contract — it must not import analytics `app`/`domain`/`infra`, only the contracts package (it already does). §3: English identifiers; `doc:` strings Spanish (neutral professional).
- Pure pass-through mapping; no logic.

## Verification
- `go build ./...`
- `go test ./internal/clientes/...`
- `golangci-lint run ./internal/clientes/...`

## Commit
`feat(clientes): expone narrativa y rasgos_ia en el DTO de la ficha`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-14-report.md` (note whether an OpenAPI snapshot was regenerated and that its diff is limited to the two new fields). Reply ≤15 lines: status, commit SHA+subject, one-line test summary, concerns, report path.
