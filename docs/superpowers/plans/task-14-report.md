# Task 14 Report — clientes PulsoDTO + mapper: expose narrativa + rasgos_ia

## Status
DONE — commit `acf21e7` on branch `feat/analytics-narrativa-ia`.

## Commit
`feat(clientes): expone narrativa y rasgos_ia en el DTO de la ficha`
SHA: `acf21e7`

## Changes

### `internal/clientes/infra/clienteshttp/dto.go`
Added two fields to `PulsoDTO` after the Fase-R titular block:
```go
// ─── Lectura del analista (IA) — Fase 2 ──────────────────────────────────────
Narrativa string   `json:"narrativa"  doc:"Lectura del analista generada por IA (síntesis + acción interna); vacío si no disponible o LLM apagado"`
RasgosIA  []string `json:"rasgos_ia"  doc:"Rasgos conductuales asignados por IA (etiquetas en español); vacío si ninguno"`
```
JSON keys exactly `narrativa` / `rasgos_ia`. Doc strings Spanish neutral professional, matching existing style.

### `internal/clientes/infra/clienteshttp/dto_mapper.go`
Added two pure pass-through assignments in the `&PulsoDTO{...}` literal inside `toFichaDTO`:
```go
Narrativa: p.Narrativa,
RasgosIA:  p.RasgosIA,
```
No logic — straight contract → DTO assignment.

### `internal/clientes/infra/clienteshttp/handlers_test.go`
Added two new tests:
- `TestObtenerFicha_Narrativa_RasgosIA_Populated` — asserts that a contract with `Narrativa` and `RasgosIA` populated maps through to the ficha JSON with the exact values.
- `TestObtenerFicha_Narrativa_RasgosIA_Empty` — asserts that a contract with zero-value `Narrativa` (empty string) and nil `RasgosIA` yields empty fields in the DTO.

## OpenAPI snapshot
No golden/snapshot file exists in `internal/clientes/infra/clienteshttp/`. The `openapi_test.go` only validates registered paths and operationIDs — it does not snapshot the full schema. No regeneration step was needed.

## Verification
- `go build ./...` — clean
- `go test ./internal/clientes/...` — all packages pass (7 packages, 0 failures)
- `golangci-lint run ./internal/clientes/...` — 0 issues
- `lefthook` pre-commit hook — all checks passed (format, vet, build, lint-staged, secrets, mod-tidy)

## Constraints respected
- CLAUDE.md §2: no cross-module imports beyond `internal/analytics` contracts package (already imported).
- CLAUDE.md §3: Go identifiers in English (`Narrativa`, `RasgosIA`); `doc:` strings in Spanish.
- Pure pass-through mapping — no logic introduced.
