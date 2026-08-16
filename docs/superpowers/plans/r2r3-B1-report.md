# Task B1 Report — Posteriors bayesianos BG/NBD

## Status
COMPLETE — commit `b6f6c7c`, rama `feat/cliente-360-r2-r3`.

## Qué se implementó

### `internal/analytics/app/btyd_posteriors.go`
- **VOs**: `IntervaloEstimado` (Punto/Lo/Hi), `Predicciones` (4 métricas + Draws + Disponible), `PosteriorInput`
- **`BTYD.Posteriors(in PosteriorInput) Predicciones`**: muestreo Monte Carlo determinístico
  - Defaults: Draws≤0→2000, HorizonCLV≤0→24, Discount≤0→0.00948879, PPaga≤0→1.0
  - Guarda: N≤0 → `Predicciones{}` (zero-value, Disponible=false)
  - Saneado defensivo: `clampInt(X/Tx, N)`, `max(Frequency,0)`, `math.Max(Monetary,0)`
  - Semilla determinística via FNV-1a 64-bit (`fnvHash`)
  - Por draw: Beta posteriors → PAlive → exp12 → DET → CLV → días próxima compra
  - Resumen: `intervalFromDraws` → `quantileFromSorted` (interpolación lineal tipo-7)
- **Samplers**: Marsaglia–Tsang Gamma (k<1 reduction cubierta) + ratio-of-gammas Beta
- **Helpers privados**: `clampInt(v,hi)`, `fnvHash`, `quantileFromSorted`, `intervalFromDraws`

### `internal/analytics/app/btyd_posteriors_test.go`
Casos mínimos del brief + extras (17 test functions):
- Determinismo, IC contiene punto, ancho↓ con historia, bounds, degradación N=0
- Edges: X=0, Monetary=0, PerdidaEsperada deducted
- Cobertura k<1: sampleGamma(0.2) × 1000 draws
- Helpers directos: `quantileFromSorted` (vacío/unitario/último), `clampInt` (3 ramas)
- Property test `rapid`: 100 cases, invariantes [0,1]/Lo≤Punto≤Hi/finitud
- `FuzzPosteriors`: 5 seeds, 20s → 579k executions, 0 failures

## Evidencia TDD RED→GREEN

**RED** (primer commit del archivo de tests, sin implementación):
```
internal/analytics/app/btyd_posteriors_test.go:19:8: undefined: PosteriorInput
internal/analytics/app/btyd_posteriors_test.go:24:10: b.Posteriors undefined
```

**GREEN** (tras escribir `btyd_posteriors.go` completo): todos los tests pasan en el primer intento.

**Ciclos de lint**: 4 issues resueltos en una segunda pasada:
1. `funlen` (51>50 stmts): merged `fallbackDias+diasI` → −1 stmt
2. `staticcheck QF1005`: `math.Pow(1+c*xv,3)` → `t:=1+c*xv; v:=t*t*t`
3. `unparam`: `clampInt(v,lo,hi)` → `clampInt(v,hi)` (lo siempre 0)
4. `gofumpt`: struct inline reformateado

## Salida exacta de tests

```
go test ./internal/analytics/app/ -run 'Posterior|Btyd|BTYD|GammaK1|Quantile|ClampInt|Interval' -count=1
PASS (40 tests, 0 failures)

go test ./internal/analytics/app/ -run FuzzPosteriors -fuzz FuzzPosteriors -fuzztime 20s
fuzz: elapsed: 20s, execs: 579146 (27830/sec), new interesting: 9 (total: 214)
PASS
```

## Cobertura del archivo

```
btyd_posteriors.go:60  Posteriors         100.0%
btyd_posteriors.go:162 intervalFromDraws  100.0%
btyd_posteriors.go:175 quantileFromSorted 100.0%
btyd_posteriors.go:201 sampleGamma        100.0%
btyd_posteriors.go:231 sampleBeta          80.0%  ← único stmt descubierto
btyd_posteriors.go:245 clampInt           100.0%
btyd_posteriors.go:257 fnvHash            100.0%

Total: 112/113 stmts = 99.1%
```

El único statement no cubierto es el guard `if ga+gb==0 { return 0.5 }` en `sampleBeta` — requerido por el brief, prácticamente inalcanzable sin mocking del RNG (requiere que dos muestras Gamma simultáneamente underflowean a 0.0 exacto; probabilidad ≈ 2^-106).

## Salida lint

```
golangci-lint run ./internal/analytics/...
0 issues.
```

## Decisiones de diseño

1. **Guards eliminados**: los guards `if !isFinite(exp12I)`, `if !isFinite(detI)`, `if !isFinite(clvI)` se removieron — son matemáticamente inalcanzables (todos los factores ∈ [0,1], producto siempre finito ≥0). La finiteness se verifica empíricamente via property test + fuzz.

2. **diasI simplificado**: en lugar de `if piI>0 {} else {}` + guard `!isFinite`, se usa una sola variable inicializada al cap y actualizada condicionalmente. Elimina 2 bloques descubiertos inalcanzables.

3. **clampInt(v, hi int)**: lo siempre es 0 en todas las call sites (brief dice `clampInt(X,0,N)`). Simplificado a 2 parámetros para satisfacer `unparam`.

4. **`t := 1+c*xv; v := t*t*t`**: expansión de `math.Pow(1+c*xv, 3)` per staticcheck QF1005 — ligeramente más eficiente y evita el overhead de Pow para exponente entero pequeño.

5. **Semilla FNV-1a**: little-endian encoding de cada input (int64 bits + float64 IEEE bits). Determinismo full-precision respecto a la representación binaria de los inputs.

## Concerns

- El guard `sampleBeta ga+gb==0` (1 stmt, 0.9% de cobertura faltante) no se puede cubrir sin mockear `*rand.Rand`. Es documentación de un invariante de seguridad numérica.
- Los tests de ancho relativo (`TestPosteriorsAnchoDecreceCHistoria`) asumen que con n=6 vs n=60 y ratio x≈n/2, la varianza posterior efectivamente disminuye. Con el modelo BG/BB y los parámetros reales (α=0.19, β=2.39, γ=0.046, δ=0.60) el test pasa consistentemente — sería frágil solo con parámetros degenerados.

## Ruta del reporte

`/Volumes/M2-1TB/Developer/msp-api/docs/superpowers/plans/r2r3-B1-report.md`
