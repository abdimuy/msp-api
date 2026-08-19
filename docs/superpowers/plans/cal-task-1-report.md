# Task 1 Report — Ponderado de cobranza por CALENDARIO

## Status: DONE

---

## What was implemented

### TDD sequence

**RED:** Wrote `domain/calendario_test.go` first — 28 VencimientosVencidos cases (V-S1..V-S8, V-M1..V-M6, V-Q1..V-Q9, V-Z1..V-Z2) and 27 AplicaEnVentana cases (A-S1..A-S8, A-M1..A-M5, A-Q1..A-Q11). Build failed immediately with `undefined: rutasdomain.VencimientosVencidos`.

**GREEN:** Implemented `domain/calendario.go` — pure functions, no I/O:
- `dateUTC`, `daysBetween`, `ultimoDiaDeMes` (private helpers)
- `VencimientosVencidos(frec, fechaCargo, fechaInicio, graceDias)` — SEMANAL: `floor(d/7)` for `d≥0`, else 0; QUINCENAL: iterates day-15 + last-day-of-month candidates per month; MENSUAL: iterates day-1 candidates per month. All use strict `>cargo` + `v+grace<inicio` (strict) per spec decisions D1/D3/D5.
- `AplicaEnVentana(frec, fechaCargo, desde, hasta)` — SEMANAL: offset arithmetic (multiple of 7 in `[max(lo,7), hi]`); QUINCENAL/MENSUAL: enumerates calendar dates in month range. Inclusive on both ends (D2), no grace (D5).
- Both functions treat unknown frecuencia as SEMANAL (matches `CadenciaDias` default).

All 55 matrix cases passed on first run.

### Integration edits

**B2 — `domain/cobranza.go`:** Added `AplicaPonderado bool` field to `VentaCobranza`. Adjusted `FechaUltPago` comment (retained for compatibility; no longer drives ponderado).

**B3 — `app/cobranza_semanal.go`:** New `enrichVentas` signature `(ventas, fechaInicio, now)`. Replaced `windowDays/cadencia` plazos calculation with `VencimientosVencidos` (integer, grace 0 for semanal, 2 for quincenal/mensual). Added `v.AplicaPonderado = AplicaEnVentana(...)`. Updated `DesglosePorZona` call.

**B4 — `app/listar_rutas.go`:** Deleted `ventaAplicaEnVentana` entirely. Updated `calcReporteZona` signature (removed `fechaInicio, now` params — no longer needed). Replaced `v.Frecuencia == Semanal || ventaAplicaEnVentana(...)` with `v.AplicaPonderado`. Updated `enrichVentas` call.

**B5 — `infra/rutashttp/cobranza_dto.go` + `cobranza_handlers.go`:** Added `AplicaPonderado bool json:"aplica_ponderado"` to `VentaCobranzaDTO`. Wired `AplicaPonderado: v.AplicaPonderado` in `toVentaCobranzaDTOs`.

**App tests:** Added `TestService_ListarRutas_PonderadoDenominador` — exercises a 3-venta mix (SEMANAL applies, QUINCENAL applies, MENSUAL outside window) and asserts denominator=2, ponderado=50%. Calls `enrichVentas` + `calcReporteZona` directly (package-internal).

---

## Verification commands and output

```
$ go build ./...
(clean, no output)

$ go test ./internal/rutas/...
ok  github.com/abdimuy/msp-api/internal/rutas/app           0.42s
ok  github.com/abdimuy/msp-api/internal/rutas/domain        0.66s
ok  github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb 0.67s
?   ...rutasfirestore  [no test files]
?   ...rutashttp       [no test files]
?   ...outbound        [no test files]

$ golangci-lint run ./internal/rutas/...
0 issues.
```

Pre-commit hook (lefthook) also passed clean: no-debug, secrets-check, format-check, mod-tidy, vet, build-check, lint-staged all green.

---

## Files changed

| File | Change |
|------|--------|
| `internal/rutas/domain/calendario.go` | NEW — pure calendar logic |
| `internal/rutas/domain/calendario_test.go` | NEW — 55-case matrix |
| `internal/rutas/domain/cobranza.go` | Added `AplicaPonderado bool` to VentaCobranza |
| `internal/rutas/app/cobranza_semanal.go` | enrichVentas new signature + calendar logic |
| `internal/rutas/app/listar_rutas.go` | Remove ventaAplicaEnVentana; calcReporteZona simplified; use v.AplicaPonderado |
| `internal/rutas/app/listar_rutas_test.go` | New denominator test; intPtr preserved |
| `internal/rutas/infra/rutashttp/cobranza_dto.go` | Added aplica_ponderado field |
| `internal/rutas/infra/rutashttp/cobranza_handlers.go` | Wired AplicaPonderado in mapper |

---

## Self-review

- `CalcAporte` (domain/aporte.go) was NOT touched. Its 5 tests remain green.
- `aporte_test.go` was NOT touched.
- No database migrations — all logic is in Go.
- The linter initially flagged `exhaustive` (missing Semanal case in switch) and `gofumpt` formatting. Both fixed: Semanal now has its own explicit `case` with the same body as `default`.
- The `graceDias` parameter is passed as 0 for SEMANAL but the semanal path ignores it (consistent with spec decision D5).
- `daysBetween` in `VencimientosVencidos` semanal uses the raw `fechaCargo`/`fechaInicio` args (not the pre-truncated `cargo`/`inicio`) because `dateUTC` is idempotent and the intermediate `cargo` variable would otherwise go unused — the compiler/linter accepts this since `cargo` is still used in `contarVencidosQuincenal`/`contarVencidosMensual` calls via the switch.

## Concerns

None. All spec decisions respected. The implementation is deterministic and doesn't depend on the current time (only fechaInicio and FechaCargo from domain inputs).
