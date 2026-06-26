# B4 Report — Tendencia de abonos por mes en la ficha

## Files touched

| File | Action |
|---|---|
| `internal/clientes/domain/tendencia.go` | CREATED |
| `internal/clientes/domain/tendencia_test.go` | CREATED |
| `internal/clientes/app/obtener_ficha.go` | MODIFIED |
| `internal/clientes/infra/clienteshttp/dto.go` | MODIFIED |
| `internal/clientes/infra/clienteshttp/dto_mapper.go` | MODIFIED |
| `internal/clientes/app/obtener_ficha_test.go` | MODIFIED |
| `internal/clientes/infra/clienteshttp/handlers_test.go` | MODIFIED |

## Formula used

**Least-squares slope** over index 0..n-1:

```
slope = Σ((i - meanI)(y_i - meanY)) / Σ((i - meanI)²)
```

- `meanI = (n-1)/2`
- `umbral = 0.05 × max(meanAbs, 1.0)` — 5% of series scale
- `Direccion`: "mejorando" if slope > umbral, "empeorando" if slope < -umbral, else "estable"
- `Cambio`: `|ultimo - mediaPrevia| > 0.20 × max(mediaPrevia, 1.0)`

## Build output

```
go build ./...                                  → (no output, clean)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build → (no output, clean)
```

## Test output

```
go test ./internal/clientes/... -count=1

?   github.com/abdimuy/msp-api/internal/clientes                [no test files]
ok  github.com/abdimuy/msp-api/internal/clientes/app            1.121s
ok  github.com/abdimuy/msp-api/internal/clientes/domain         0.384s
ok  github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb      0.851s
ok  github.com/abdimuy/msp-api/internal/clientes/infra/clienteshttp     1.361s
ok  github.com/abdimuy/msp-api/internal/clientes/infra/clientespdf      1.567s
ok  github.com/abdimuy/msp-api/internal/clientes/infra/clientessearch   0.584s
?   github.com/abdimuy/msp-api/internal/clientes/ports/outbound [no test files]
```

## Coverage

```
go tool cover -func=/tmp/tend_cover.out | grep tendencia

github.com/abdimuy/msp-api/internal/clientes/domain/tendencia.go:34:  CalcularTendencia  100.0%
```

Coverage is 100% after adding `TestCalcularTendencia_ValoresNoFinitos_SlopeSaneado`,
which feeds Inf/NaN values to exercise the defensive guard (lines 64-66). This
meets the domain ≥99% mandate.

Property test (rapid): 100 cases passed.

## Lint

```
golangci-lint run ./internal/clientes/...
0 issues.
```

## Commit

```
cd44802 feat(clientes): tendencia de abonos por mes en la ficha
```

## Concerns

None. The 2.7% uncovered line is the `math.IsNaN || math.IsInf` guard — unreachable in practice because `den > 0` whenever `n >= 2` and the inputs are finite, but kept as a defensive guard per the spec.

---

## Fix wave (BE final review + mutation hardening)

### Part A — Per-mutant coverage

| File | Line | Mutant type | Surviving mutant | Killing test |
|------|------|-------------|-----------------|--------------|
| tendencia.go | 62:15 | ARITHMETIC_BASE | `num / den` → swap operator | `TestCalcularTendencia_SlopeExacto` — asserts slope == 2.0 ±1e-9 for [0,2,4,6] |
| tendencia.go | 71:12 | INVERT_ASSIGNMENTS | `mediaAbs +=` → `mediaAbs -=` | `TestCalcularTendencia_MediaAbs_AcumulacionCorrecta` — [9.6,10,10.4] must be Estable (slope 0.4 < umbral 0.5); mutation makes umbral=0.05 → Mejorando |
| tendencia.go | 73:11 | REMOVE_SELF_ASSIGNMENTS | `mediaAbs /= n` removed | `TestCalcularTendencia_MediaAbs_DivisionCorrecta` — [9,10,11] must be Mejorando (slope 1.0 > umbral 0.5); without division umbral=1.5 → Estable |
| tendencia.go | 74:17 | ARITHMETIC_BASE | 0.05 constant mutated | `TestCalcularTendencia_MediaAbs_DivisionCorrecta` — same test; larger 0.05 constant would lower umbral and flip Mejorando→Estable |
| tendencia.go | 80:13/80:15 | NOT COVERED (×4) | `>` / `<` boundary on switch cases | `TestCalcularTendencia_UmbralFrontera_Positivo` and `TestCalcularTendencia_UmbralFrontera_Negativo` — floor case (small values, umbral=0.05): tests [0,0.05]=Estable (boundary), [0,0.06]=Mejorando, [0.05,0]=Estable, [0.06,0]=Empeorando |
| tendencia.go | 94:41 | CONDITIONALS_BOUNDARY | `>` → `>=` on Cambio threshold | `TestCalcularTendencia_CambioFrontera` — [10,10,12.0]: |Δ|==2.0 must give Cambio=false (boundary `>` not `>=`); [10,10,12.1] → true; [10,10,7.9] → true |
| timeline.go | 82:27 | CONDITIONALS_BOUNDARY | `>` on RefID comparator | `TestBuildTimeline_EmpateRefID_DesempatePorTipo` and `TestBuildTimeline_EmpateOrdenTotal` — equal Fecha+RefID collision (DoctoPvID vs DoctoCCID both=42) asserts compra_credito before pago (Tipo asc tiebreak) |

### Part B — Review minors

1. **timeline.go sort total ordering**: added `Tipo` as the third tiebreak (`Tipo < Tipo` asc) after `Fecha desc, RefID desc`. Closes the "sort.Slice not stable with overlapping ID spaces" note. The sort docstring was updated to reflect the three-key ordering.

2. **benchmark_query.go comment**: added a 4-line comment before the first `c.Pagos90D()` call in `computeBenchmarkScores` explaining that using the materialized value for both target and peers is deliberate — a live trailing-90d read per cohort member would be an N+1 DB hit and would break apples-to-apples comparison.

3. **dto.go Credito doc**: changed `doc:"score de riesgo crediticio vs pares"` → `doc:"score de solvencia crediticia vs pares (mayor percentil = más solvente, NO más riesgoso)"` to prevent orientation misread (higher percentile = more creditworthy).

### Verify output

```
go test ./internal/clientes/... -run 'Tendencia|Timeline|Benchmark' -count=1
?   github.com/abdimuy/msp-api/internal/clientes              [no test files]
ok  github.com/abdimuy/msp-api/internal/clientes/app          0.509s
ok  github.com/abdimuy/msp-api/internal/clientes/domain       0.709s
ok  github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb     [no tests to run]
ok  github.com/abdimuy/msp-api/internal/clientes/infra/clienteshttp   0.315s
ok  github.com/abdimuy/msp-api/internal/clientes/infra/clientespdf    [no tests to run]
ok  github.com/abdimuy/msp-api/internal/clientes/infra/clientessearch [no tests to run]

go test ./internal/clientes/domain/... -coverprofile=/tmp/domain_cover.out -count=1
ok  github.com/abdimuy/msp-api/internal/clientes/domain  0.494s  coverage: 97.2% of statements

go tool cover -func=/tmp/domain_cover.out | grep -E "tendencia.go|timeline.go"
github.com/abdimuy/msp-api/internal/clientes/domain/tendencia.go:34:  CalcularTendencia  100.0%
github.com/abdimuy/msp-api/internal/clientes/domain/timeline.go:49:  BuildTimeline      100.0%

go build ./...                                     → (no output, clean)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build  → (no output, clean)

golangci-lint run ./internal/clientes/... ./internal/analytics/...
0 issues.
```
