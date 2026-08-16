# Task B1 — Posteriors bayesianos del BG/NBD (pura, en analytics/app)

## Dónde encaja
Repo **msp-api**, módulo `analytics`. Ya existe el motor de punto (cerrado) en
`internal/analytics/app/btyd.go`: `BTYD` (value object) con `PAlive(x,tx,n)`,
`ExpectedPurchases(m,x,tx,n)`, `DET(x,tx,n,H,d)`, `ExpectedAvgProfit(frequency,monetary)`. Los
hiperparámetros fijos (α,β,γ,δ) y Gamma-Gamma (p,q,v) están embebidos (`btyd_params.json`,
`clv_params.json`). **Period = month.** Esta tarea agrega una **capa de incertidumbre bayesiana**:
intervalos creíbles (p5/p95) alrededor de PAlive, compras esperadas a 12m, CLV, y "próxima compra".
Es **código puro** — sin I/O, sin `time.Now()`, determinístico. NO modifiques `btyd.go`; crea un
archivo nuevo. Lee `btyd.go` y `btyd_test.go` antes de empezar (reusa el estilo y los fixtures).

## Archivo a crear
`internal/analytics/app/btyd_posteriors.go` (+ `btyd_posteriors_test.go`).

## VOs y API (exactos)
```go
// IntervaloEstimado es un punto con su intervalo creíble [Lo, Hi].
type IntervaloEstimado struct {
    Punto float64 // mediana de los draws (mediana posterior)
    Lo    float64 // percentil 5
    Hi    float64 // percentil 95
}

// Predicciones agrupa las predicciones bayesianas de un cliente.
type Predicciones struct {
    Disponible          bool              // false si n<=0 (no se puede computar)
    PAlive              IntervaloEstimado // probabilidad de seguir activo, en [0,1]
    ComprasEsperadas12m IntervaloEstimado // compras repetidas esperadas próximos 12 meses, >=0
    CLV                 IntervaloEstimado // valor de vida en pesos
    ProximaCompraDias   IntervaloEstimado // offset en días hasta la próxima compra estimada
    Draws               int               // número de muestras usadas
}

// PosteriorInput son los datos del cliente + config para muestrear.
type PosteriorInput struct {
    X, Tx, N        int     // frecuencia repetida, recencia (mes del último), meses observados
    Frequency       int     // para Gamma-Gamma (típicamente == X)
    Monetary        float64 // ticket promedio observado (pesos)
    Draws           int     // <=0 => 2000
    Margin          float64 // margen para CLV (el servicio pasa 0.528); si <=0, CLV usa 0
    PPaga           float64 // prob. de pago para CLV; <=0 => 1.0
    PerdidaEsperada float64 // pérdida esperada en pesos a restar del CLV; default 0
    HorizonCLV      int     // <=0 => b.HorizonMonths()
    Discount        float64 // <=0 => b.MonthlyDiscount()
}

// Método sobre el value object existente:
func (b BTYD) Posteriors(in PosteriorInput) Predicciones
```

## Modelo de muestreo (impleméntalo EXACTAMENTE así)
Defaults: `Draws<=0 → 2000`; `HorizonCLV<=0 → b.HorizonMonths()` (24); `Discount<=0 → b.MonthlyDiscount()`;
`PPaga<=0 → 1.0`; `PerdidaEsperada` se usa tal cual.

Guarda de validez: si `in.N <= 0` → devuelve `Predicciones{Disponible:false, Draws:0}` (zero-value).
Saneo defensivo de índices (no panics): `x = clampInt(in.X,0,in.N)`, `tx = clampInt(in.Tx,0,in.N)`,
`n = in.N`. Usa `frequency = max(in.Frequency,0)`, `monetary = max(in.Monetary,0)`.

**Semilla determinística:** `seed := fnvHash(x, tx, n, frequency, monetary, draws)` (FNV-1a 64-bit
sobre los bytes de los inputs; usa `hash/fnv`). `rng := rand.New(rand.NewSource(int64(seed)))`
(paquete `math/rand`; añade `//nolint:gosec // simulación determinística, no es criptografía`).

Para cada draw i = 1..Draws:
1. `pi_i  = sampleBeta(rng, α + x, β + max(n−x,0))`           // prob. de compra mensual (Beta posterior)
2. `th_i  = sampleBeta(rng, γ + 1, δ + max(tx,0))`            // prob. de abandono mensual (Beta posterior)
3. `gap   = max(n − tx, 0)`; `alive_i = pow(1−th_i, gap)`     // P(sigue activo) ∈ [0,1] (clamp01)
4. `surv12 = Σ_{m=1..12} pow(1−th_i, m)`; `exp12_i = alive_i · pi_i · surv12`   // compras 12m, >=0
5. `survH  = Σ_{m=1..H} pow(1−th_i, m) / pow(1+Discount, m)`; `det_i = alive_i · pi_i · survH`  // DET descontado
6. Monetario (ruido multiplicativo Gamma de media 1, se aprieta con la frecuencia):
   `k = p·frequency + q` (siempre ≥ q > 1); `mNoise_i = sampleGamma(rng, k, 1) / k`;
   `eM = b.ExpectedAvgProfit(frequency, monetary)`; `avg_i = eM · mNoise_i`
7. `clv_i = Margin · avg_i · det_i · PPaga − PerdidaEsperada`
8. `nextDias_i = (1.0 / pi_i) · 30.44` si `pi_i > 0`, si no usa un tope grande (p.ej. `n·30.44` o 3650)

Acumula los 8 arreglos (en realidad necesitas: alive, exp12, clv, nextDias). Para cada métrica:
`Punto = quantile(.50)`, `Lo = quantile(.05)`, `Hi = quantile(.95)` con un `quantile` por orden
(ordena copia del slice, índice = round(q·(len−1)), interpolación lineal opcional pero simple basta;
sé consistente). PAlive: clamp cada draw a [0,1]. ComprasEsperadas/CLV/Dias: garantiza finitud.

`Disponible = true` cuando se computa. `Draws` = número efectivo de draws.

### Samplers (impleméntalos; stdlib `math/rand` no trae Gamma/Beta)
- **Gamma(shape k>0, scale=1)** — Marsaglia–Tsang:
  - Si `k < 1`: `g := sampleGamma(rng, k+1, 1); u := rng.Float64(); return g · pow(u, 1/k)`.
  - Si `k ≥ 1`: `d = k − 1.0/3; c = 1/sqrt(9d)`; loop:
    `x = rng.NormFloat64(); v = pow(1+c·x, 3)`; si `v ≤ 0` continúa; `u = rng.Float64()`;
    si `u < 1 − 0.0331·x⁴` → `return d·v`;
    si `log(u) < 0.5·x² + d·(1 − v + log(v))` → `return d·v`.
  - **Importante:** α=0.19 y γ=0.046 (<1), así que la rama `k<1` SE EJERCITA — cúbrela en tests.
- **Beta(a,b):** `ga = sampleGamma(rng,a,1); gb = sampleGamma(rng,b,1); if ga+gb==0 {return 0.5}; return ga/(ga+gb)`.

Reusa helpers numéricos existentes de `btyd.go` donde apliquen (`isFinite`, `clamp01Finite`); si
necesitas `clampInt`/`maxInt`/`pow` decláralos privados aquí. NO dupliques lógica del closed-form.

## Tests (gate ≥99% líneas + property `pgregory.net/rapid` + fuzz `FuzzPosteriors`)
Reusa fixtures de `btyd_test.go` (cómo construye `BTYD` vía `LoadBTYD()`). Casos mínimos:
- **IC contiene el punto:** `Lo ≤ Punto ≤ Hi` para las 4 métricas (por construcción, pero afírmalo).
- **Ancho ↓ con más historia:** fijando ratio `x≈n/2`, comparar n pequeño (p.ej. 6) vs n grande
  (p.ej. 60): el ancho RELATIVO `(Hi−Lo)/Punto` de `ComprasEsperadas12m` debe disminuir.
- **Determinismo:** dos llamadas con el mismo `PosteriorInput` → `Predicciones` idénticas (todos los floats).
- **Bounds:** `PAlive` ∈ [0,1]; `ComprasEsperadas12m.Lo ≥ 0`; `CLV` finito; `ProximaCompraDias ≥ 0`.
- **Degradación:** `N=0` → `Disponible=false`, zero-value.
- **Edge:** `X=0` (sin recompra) computa sin panic; `Monetary=0` → `CLV.Punto` finito (≈ −PerdidaEsperada o ~0).
- **Cobertura de la rama `k<1`** del Gamma (inputs con α/γ<1 ya lo hacen; añade aserción que el sampler
  produce valores > 0 finitos para shape 0.2).
- **Property (rapid):** inputs aleatorios saneados (X∈[0,N], Tx∈[0,N], N∈[1,200], Monetary∈[0,1e6],
  Draws fijo pequeño p.ej. 200 para velocidad) → sin panic, invariantes (bounds, Lo≤Punto≤Hi, finitud).
- **Fuzz:** `FuzzPosteriors(f)` sembrando varios `(x,tx,n,freq,monetary)`; el cuerpo sanea y verifica
  no-panic + invariantes. Usa `Draws` chico para que el fuzz sea rápido.

Mantén el output de tests **pristino** (sin prints sueltos). Si un test es lento por Draws=2000,
usa Draws menor en los property/fuzz.

## Restricciones (CLAUDE.md)
- Código y comentarios **en inglés**; identificadores camelCase/Pascal. (Los nombres de dominio en
  español como `Predicciones`/`ComprasEsperadas12m` se mantienen — son vocabulario del proyecto; añade
  `//nolint:misspell` arriba del archivo como hace `btyd.go`.)
- Puro: sin `time.Now()`, sin I/O, sin estado mutable global. RNG local sembrado por hash.
- `math/rand` con `//nolint:gosec`. Sin dependencias nuevas salvo `pgregory.net/rapid` (ya está en go.mod;
  verifica con `grep rapid go.mod`).

## Verificación (corre y pega salida en el reporte)
- `go build ./...`
- `go test ./internal/analytics/app/ -run 'Posterior|Btyd|BTYD' -count=1` (unit)
- `go test ./internal/analytics/app/ -run FuzzPosteriors -fuzz FuzzPosteriors -fuzztime 20s` (fuzz corto)
- Cobertura del archivo: `go test ./internal/analytics/app/ -run 'Posterior' -coverprofile=/tmp/b1.out`
  y reporta el % de `btyd_posteriors.go` (apunta a ≥99%).
- `golangci-lint run ./internal/analytics/...` → 0 issues sobre el archivo nuevo.

## Reporte
Escribe el reporte completo en `docs/superpowers/plans/r2r3-B1-report.md` (qué implementaste, evidencia
TDD RED→GREEN, salida exacta de los tests/cobertura/lint, decisiones de diseño numérico, concerns).
Devuelve solo: status, hash(es) de commit, resumen de tests en una línea, concerns, ruta del reporte.

## Commit
Rama actual `feat/cliente-360-r2-r3`, mensaje:
`feat(analytics): posteriors bayesianos BG/NBD con intervalos creíbles`.
Sin `--no-verify`, sin footer de Claude.

## TDD
Sigue TDD: escribe primero un test que falle (RED) para `Posteriors` (p.ej. determinismo o bounds),
confírmalo rojo, implementa hasta verde (GREEN), luego añade property/fuzz y el resto. Documenta RED/GREEN.
