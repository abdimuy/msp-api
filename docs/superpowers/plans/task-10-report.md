# Task 10 Report — Pulso refactor + narrativa helpers + in-memory repo fake

## Status: DONE

Commit `3ac7def` — `refactor(analytics): extrae computePulso y agrega helpers + fake de repo para narrativa`

## Changes delivered

### 1. `computePulso` extracted (`internal/analytics/app/pulso_query.go`)
Moved the ~25-line scoring assembly block from both `ObtenerPulsoCliente` and
`ObtenerPulsosClientes` into `func (s *Service) computePulso(c, now, p90)`. Both
call sites now read:
```go
p90 := pagos90dFor(live, ok, c)
comp := s.computePulso(c, now, p90)
```
Zero change to any computed value.

### 2. `candidatoYPulso` helper added (`pulso_query.go`)
`func (s *Service) candidatoYPulso(ctx, clienteID) (*WinbackCandidato, PulsoComputado, error)`
mirrors ObtenerPulsoCliente's load path without the contract projection. Reuses
the same apperror wrapping pattern. Exposed via `ExportCandidatoYPulso` in
`export_test.go` (satisfies the `unused` linter before Task 11 wires the worker).

### 3. `buildNarrativeInput` helper (`internal/analytics/app/narrativa_input.go`)
Maps every field of `outbound.NarrativeInput` from `c` accessors and `comp`
fields, including `Catalogo = catalogo`. Exposed via `ExportBuildNarrativeInput`
in `export_test.go`.

### 4. In-memory `NarrativaRepo` fake (`internal/analytics/infra/narrativamem/repo.go`)
Package `narrativamem`. `Repo` implements `outbound.NarrativaRepo` with
`sync.Mutex + map[int]*NarrativaRow + map[int]PendienteRow`. Contract:
- `GetNarrativa` miss → `(nil, nil)` (`//nolint:nilnil` per existing pattern in `analyticsfb`)
- `UpsertNarrativa` → replace-or-insert per CLIENTE_ID
- `Encolar` → idempotent overwrite per CLIENTE_ID
- `ListarPendientes` → deterministic ascending order, respects `limit > 0`
- `BorrarPendiente` → remove; no-op if absent
- `GetNarrativa` returns a deep copy (Rasgos slice is also copied) to prevent
  mutation of internal state.
- Compile assertion: `var _ outbound.NarrativaRepo = (*Repo)(nil)`
- Inspection helpers: `NarrativaCount()`, `PendientesCount()`

## Tests

### `narrativa_input_test.go` (package `app_test`)
Builds a candidate + fully-populated `PulsoComputado`, calls
`ExportBuildNarrativeInput(c, comp, CatalogoRasgos)`, asserts:
- identity fields from candidate (ClienteID, Nombre, Zona)
- bands from comp (BandaCredito, BandaRecompra, BandaCLV, Segmento, TierRiesgo, EstadoPago)
- decimal magnitudes (Monetary, MontoCLV, Saldo, PctPagosATiempo)
- integer fields (Frecuencia, RecenciaDias, CadenciaDias, DiasAtrasoProm)
- drivers slices (CreditoDrivers, RecompraDrivers, CLVDrivers)
- titulars (CreditoResumen, RecompraResumen, CLVResumen)
- `Catalogo == CatalogoRasgos`

### `narrativamem/repo_test.go` (package `narrativamem_test`)
8 tests covering:
- Get miss → nil
- Upsert + Get round-trip
- Upsert twice → one row (second overwrites)
- Encolar twice → one pending row (second overwrites inputHash)
- ListarPendientes limit caps
- ListarPendientes deterministic ascending order (5 elements inserted out of order)
- BorrarPendiente removes the row
- BorrarPendiente no-op on absent ID
- GetNarrativa returns a copy (mutation test)

## Parity proof (refactor safety net)

All pre-existing pulso tests passed unchanged after the refactor:

```
ok  github.com/abdimuy/msp-api/internal/analytics/app   0.599s
```

Includes: `TestObtenerPulsoCliente_Found`, `TestObtenerPulsoCliente_NotFound`,
`TestObtenerPulsosClientes_MixedPresence`, `TestObtenerPulsosClientes_EmptyInput`,
`TestObtenerPulsosClientes_AllAbsent`, `TestObtenerPulsoCliente_Recompra_WithVHistory`,
`TestObtenerPulsoCliente_Recompra_NoVHistory`,
`TestObtenerPulsoCliente_ScoreMatchesComputeSegmentoScore`.

## Verification results

```
go build ./...                          → clean
go test ./internal/analytics/...       → all 9 packages pass
golangci-lint run ./internal/analytics/... → 0 issues
lefthook pre-commit                    → all hooks pass
```

## Lint issues resolved
- `gofumpt`: reformatted struct literal alignment in `repo.go`
- `intrange`: replaced C-style for loop with `range len(rows)-1` in test
- `misspell`: renamed `narrativas` field to `rows`, added `//nolint:misspell` file header
- `nilnil`: added `//nolint:nilnil` directive matching `analyticsfb/narrativa_repo.go` pattern
- `unused`: exposed `candidatoYPulso` via `ExportCandidatoYPulso` in `export_test.go`

## Files changed
- `internal/analytics/app/pulso_query.go` (refactor: extract computePulso + add candidatoYPulso)
- `internal/analytics/app/narrativa_input.go` (new)
- `internal/analytics/app/narrativa_input_test.go` (new)
- `internal/analytics/app/export_test.go` (added ExportBuildNarrativeInput + ExportCandidatoYPulso)
- `internal/analytics/infra/narrativamem/repo.go` (new)
- `internal/analytics/infra/narrativamem/repo_test.go` (new)
