# Task B3 — Peer benchmark: percentiles por cohorte comparable

## Dónde encaja
Repo **msp-api**, módulo `analytics` + consumo en `clientes`. Agrega un endpoint
`GET /v2/clientes/{id}/benchmark?cohort_by=zona|segmento|antiguedad` que ubica al cliente vs sus pares
comparables: por cada una de **4 métricas** devuelve percentil + mediana + p25/p75 + N + guardia de
muestra pequeña. Espeja el seam que B2 ya estableció para "predicciones" (contrato analytics → puerto
outbound clientes → adapter en cmd/api → clientesapp → clienteshttp). Lee como plantilla los archivos
de B2: `internal/analytics/app/predicciones_query.go`, `analytics_contracts.go`,
`internal/clientes/ports/outbound/analytics_client.go`, `cmd/api/clientes_wiring.go`,
`internal/clientes/app/obtener_predicciones.go`, y en `clienteshttp` el endpoint `obtener-predicciones`.

## Decisión de arquitectura (NO negociable — leer antes de codear)
`MSP_AN_WINBACK_CANDIDATOS` guarda features crudos (ZONA, PCT_PAGOS_A_TIEMPO, FECHA_PRIMER_VENTA,
PAGOS_90D, SALDO, MONETARY_V_PROM, VENTAS_MESES_DISTINTOS…) **pero NO los scores computados** (CLV,
score_credito, score_recompra, segmento) — esos se calculan en Go en read-time. Por **CLAUDE.md §1
(nada de lógica en BD)**, los percentiles de las métricas computadas se calculan **en Go**, nunca en SQL.
Por eso la **cohorte se acota a la ZONA del cliente**: se cargan TODOS los candidatos de esa zona vía
SQL (`WHERE ZONA = ?`, sin paginar — cientos/miles, no 60k), se computan las 4 métricas en Go para cada
miembro, y se calculan percentiles en Go. Los sub-modos `segmento` y `antiguedad` son **sub-filtros en
Go dentro de la zona**. Documenta en el reporte que v1 es **zona-scoped** (cohortes globales quedan para
la materialización diferida `MSP_AN_PERCENTILES`, fuera de alcance).

## Las 4 métricas (todas "mayor = mejor", así que percentil alto = bien)
Por cada candidato (target y cada par), usando `now := s.clock.Now()` y **PAGOS_90D materializado**
(`c.Pagos90D()`) para no hacer query live por cohorte:
1. **puntualidad** = `c.PctPagosATiempo().InexactFloat64()`. Aplica cuando el cliente tiene historial de
   pago → criterio: `pct > 0` (clientes sin pagos se EXCLUYEN de esta distribución). Documenta el criterio.
2. **CLV** = `computeCLVConRazones(c, now, s.btyd, s.scorecard, s.clvParams, c.Pagos90D())` → si el
   último bool `aplica` es true, valor = `monto.Decimal().InexactFloat64()`; si no, EXCLUIR de la distribución.
3. **credito** = `computeCreditoScore(c, now, s.scorecard, c.Pagos90D())` → si `aplica`, valor = `score.Int()` (float64); si no, EXCLUIR.
4. **recompra** = `computeRecompraScore(c, now, s.recompraScorecard, s.btyd)` → si `aplica`, valor = `score.Int()` (float64); si no, EXCLUIR.

Cada métrica tiene su propio N (miembros con valor aplicable). Si el **target** no aplica para una
métrica → esa métrica sale con `Aplica:false` (sin percentil).

## Sub-filtros de cohorte (dentro de la zona del target)
- `cohort_by=zona` (default, y también cuando viene vacío o inválido): todos los pares de la zona.
- `cohort_by=segmento`: pares cuyo `computeSegmentoScore(c, now)` (primer return, el `Segmento`) == el del target.
- `cohort_by=antiguedad`: pares cuyo `FechaPrimerVenta` está dentro de **±6 meses** del target
  (usa diferencia de índice de mes; declara `const ventanaAntiguedadMeses = 6`).
La cohorte **excluye al target mismo** (son los pares). Guardia: `const benchmarkMuestraMinima = 30`.

## Parte 1 — analytics

### 1a. `internal/analytics/app/percentile.go` (NUEVO, puro)
```go
// percentilEnCohorte returns the percentile rank of valor within cohorte (0..100,
// = 100 * count(v <= valor)/n), plus the cohorte median, p25, p75, and n.
// cohorte need not be sorted; n == 0 → all zeros.
func percentilEnCohorte(valor float64, cohorte []float64) (percentil, mediana, p25, p75 float64, n int)
```
Quantiles por orden con interpolación lineal (puedes reutilizar el helper de cuantiles de
`btyd_posteriors.go` si está exportado dentro del paquete, o declarar uno local; no dupliques sin razón).

### 1b. Contrato en `analytics_contracts.go` (paquete raíz)
```go
type MetricaBenchmark struct {
    Aplica         bool    // el target tiene valor para esta métrica
    Valor          float64 // valor del target
    Percentil      float64 // 0..100 (solo si Aplica && !MuestraPequena)
    Mediana        float64
    P25            float64
    P75            float64
    N              int     // pares con valor aplicable para esta métrica
    MuestraPequena bool    // N < benchmarkMuestraMinima
}
type BenchmarkContract struct {
    Disponible  bool   // target encontrado y con zona
    CohortBy    string // "zona" | "segmento" | "antiguedad"
    Zona        string
    N           int    // tamaño de la cohorte base (tras sub-filtro)
    Puntualidad MetricaBenchmark
    CLV         MetricaBenchmark // Valor/Mediana/P25/P75 en pesos
    Credito     MetricaBenchmark
    Recompra    MetricaBenchmark
}
```

### 1c. Repo: nuevo método en `WinbackRepo` (outbound/repos.go) + impl en analyticsfb
```go
// ListCandidatosByZona returns ALL materialized candidatos in the given zona
// (unpaginated; used to build the peer-benchmark cohort).
ListCandidatosByZona(ctx context.Context, zona string) ([]*domain.WinbackCandidato, error)
```
- Actualiza el comentario `//nolint:interfacebloat // seven methods…` al nuevo conteo (ocho).
- Impl en `analyticsfb/repo.go`: `selectCandidatoBase + " WHERE ZONA = ?"`, escanea con el rowmapper
  existente (`candidatoRowRaw` / `candidatoCols`). Sin paginar.
- **Test de integración** (`repo_test.go`, `fbtestutil.WithTestTransaction`, gate `FB_DATABASE`,
  rollback-only, ≥80%): inserta 2–3 candidatos en una zona de prueba + 1 en otra, verifica que
  `ListCandidatosByZona` devuelve solo los de la zona pedida. NO dejes datos (rollback).

### 1d. Service: `internal/analytics/app/benchmark_query.go` (NUEVO)
`func (s *Service) ObtenerBenchmark(ctx context.Context, clienteID int, cohortBy string) (analytics.BenchmarkContract, error)`
1. Normaliza `cohortBy`: si no es uno de {"zona","segmento","antiguedad"} → "zona".
2. `target, err := s.repo.GetCandidato(ctx, clienteID)`; not-found (`errors.Is(err, domain.ErrWinbackCandidatoNotFound)`) → `return analytics.BenchmarkContract{Disponible:false, CohortBy: cohortBy}, nil`; otro error → envuelve `apperror.NewInternal("benchmark_cliente_failed","error al obtener benchmark del cliente")…`.
3. `zona := target.Zona()`; si `zona == ""` → `Disponible:false` (no se puede comparar sin zona).
4. `pares, err := s.repo.ListCandidatosByZona(ctx, zona)` (envuelve error). Quita al target de la lista (por ClienteID).
5. `now := s.clock.Now()`. Aplica el sub-filtro de cohorte según `cohortBy` (segmento/antiguedad) sobre `pares`.
6. Para cada métrica: arma el slice de valores de los pares (excluyendo no-aplica), computa el valor del
   target (con su aplicabilidad), y si el target aplica: `percentil,mediana,p25,p75,n := percentilEnCohorte(valorTarget, valoresPares)`. Si `n < benchmarkMuestraMinima` → `MuestraPequena:true` y deja percentil/cuantiles en 0. Llena `MetricaBenchmark`.
7. `BenchmarkContract{Disponible:true, CohortBy:cohortBy, Zona:zona, N:len(paresFiltrados), …}`.
   Mapper app→contrato puede ser inline (todo es paquete app + tipo raíz, sin ciclo).

## Parte 2 — clientes (mirror de B2)
- `outbound/analytics_client.go`: `ObtenerBenchmark(ctx, clienteID int, cohortBy string) (analytics.BenchmarkContract, error)`.
- `cmd/api/clientes_wiring.go`: método en `clientesAnalyticsAdapter` que delega.
- `internal/clientes/app/obtener_benchmark.go` (NUEVO): passthrough fino en el `Service`.

## Parte 3 — clienteshttp (mirror de obtener-predicciones)
- `dto.go`:
  ```go
  type ObtenerBenchmarkInput struct {
      ID       int    `path:"id"        doc:"ID de Microsip del cliente"`
      CohortBy string `query:"cohort_by" doc:"Grupo de pares: zona (default), segmento, antiguedad"`
  }
  type ObtenerBenchmarkOutput struct{ Body BenchmarkDTO }
  type MetricaDTO struct {
      Aplica bool `json:"aplica"`; Valor float64 `json:"valor"`; Percentil float64 `json:"percentil"`
      Mediana float64 `json:"mediana"`; P25 float64 `json:"p25"`; P75 float64 `json:"p75"`
      N int `json:"n"`; MuestraPequena bool `json:"muestra_pequena"`
  }
  // CLV: valores monetarios como strings (2 decimales); percentil numérico.
  type MetricaMoneyDTO struct {
      Aplica bool `json:"aplica"`; Valor string `json:"valor"`; Percentil float64 `json:"percentil"`
      Mediana string `json:"mediana"`; P25 string `json:"p25"`; P75 string `json:"p75"`
      N int `json:"n"`; MuestraPequena bool `json:"muestra_pequena"`
  }
  type BenchmarkDTO struct {
      Disponible bool `json:"disponible"`; CohortBy string `json:"cohort_by"`; Zona string `json:"zona"`; N int `json:"n"`
      Puntualidad MetricaDTO `json:"puntualidad"`; CLV MetricaMoneyDTO `json:"clv"`
      Credito MetricaDTO `json:"credito"`; Recompra MetricaDTO `json:"recompra"`
  }
  ```
- `dto_mapper.go`: `benchmarkToDTO(c analytics.BenchmarkContract) BenchmarkDTO`; CLV con `decimal.NewFromFloat(...).StringFixed(moneyScale)`.
  **De paso (B2 minor):** corrige el header de sección duplicado en este archivo —hoy "Endpoint 4:
  venta detalle" aparece dos veces; renombra el primero (el que ahora precede a la sección de
  predicciones) a algo correcto, p.ej. "Endpoint: predicciones". No cambies código, solo el comentario.
- `handlers_clientes.go`: handler `ObtenerBenchmark` (espejo de `ObtenerPredicciones`):
  `currentUserOrError` → `requirePerm(cu, auth.PermClientesLeer)` → `h.svc.ObtenerBenchmark(ctx, input.ID, input.CohortBy)` → `benchmarkToDTO`. Agrega la aserción de tipo del handler.
- `routes.go`: `huma.Register` OperationID "obtener-benchmark", GET `/clientes/{id}/benchmark`, copia
  seguridad/tags de `obtener-predicciones`.

## Tests (mandato: robustos por capa)
- **percentile.go** (`percentile_test.go`, ≥99% + property `pgregory.net/rapid`): distribución conocida
  → percentil/mediana/p25/p75 exactos; vacío → n=0; un elemento; property: monotonía (mayor valor →
  percentil ≥), bounds [0,100], no panics.
- **benchmark_query** (`benchmark_query_test.go`, Service real + fake repo, ≥90%): construye una cohorte
  de ≥35 candidatos en una zona con **PctPagosATiempo** controlados para afirmar el percentil EXACTO de
  la métrica puntualidad (es la métrica directamente controlable); afirma la guardia (cohorte de <30 →
  `MuestraPequena:true`, sin percentil); afirma que `cohort_by=segmento` y `=antiguedad` reducen N de
  forma esperada; target not-found → `Disponible:false` sin error; `cohort_by` inválido/ vacío → "zona".
  Usa el patrón de `predicciones_query_test.go` (NewService carga btyd/scorecards embebidos; fake repo).
- **clientes app** (`obtener_benchmark_test.go`, fake AnalyticsClient, ≥90%): delega y propaga error.
- **clienteshttp handler** (≥70%): sin permiso → error; con permiso → DTO correcto (CLV como strings,
  muestra_pequena serializado, default cohort_by). Extiende los fakes existentes para el método nuevo.
- Output de tests pristino. Reporta cobertura por archivo nuevo.

## Verificación (pega salida en el reporte)
- `go build ./...` + `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`
- `go test ./internal/analytics/... ./internal/clientes/... -count=1`
- Integración: `make test-firebird-all` (o el target que corra los tests con `FB_DATABASE`) para el
  test de `ListCandidatosByZona` — si no tienes la BD local, NO inventes; deja el test escrito y
  reporta que requiere `FB_DATABASE` (lo corro yo).
- `golangci-lint run ./internal/analytics/... ./internal/clientes/... ./cmd/...` → 0 issues.

## Reporte
`docs/superpowers/plans/r2r3-B3-report.md` (archivos, decisiones —especialmente la cohorte zona-scoped
y los criterios de aplicabilidad por métrica—, salida de build/test/cobertura/lint, concerns). Devuelve
solo (≤15 líneas): Status, commit(s) SHA+subject, resumen de tests en 1 línea, concerns, ruta del reporte.

## Commit
Rama `feat/cliente-360-r2-r3`. Mensaje:
`feat(clientes): benchmark de pares por cohorte (zona/segmento/antiguedad)`.
Sin `--no-verify`, sin footer de Claude. Puedes hacer 1–2 commits (analytics; luego clientes+http).
