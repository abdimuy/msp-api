# B3 — Benchmark de pares por cohorte: informe de implementación

## Tarea

`GET /v2/clientes/{id}/benchmark?cohort_by=zona|segmento|antiguedad`

Calcula los percentiles de 4 métricas de scoring (puntualidad, CLV, crédito, recompra) para un cliente vs su grupo de pares dentro de la misma zona. Tres modos de sub-filtro: `zona` (default), `segmento` (mismo RFM), `antiguedad` (±6 meses de FechaPrimerVenta).

## Archivos creados

| Archivo | Descripción |
|---------|-------------|
| `internal/analytics/app/percentile.go` | `percentilEnCohorte` (rank 0-100, mediana, P25, P75) |
| `internal/analytics/app/benchmark_query.go` | `ObtenerBenchmark`, `benchmarkSubFiltro`, `buildMetricaBenchmark`, `computeBenchmarkScores`, `benchmarkPeerVectors` |
| `internal/analytics/app/percentile_test.go` | Tests unitarios + property-based (rapid) de `percentilEnCohorte` |
| `internal/analytics/app/benchmark_query_test.go` | Tests de `ObtenerBenchmark` (percentil exacto, muestra pequeña, sub-filtros, degradación, errores) |
| `internal/clientes/app/obtener_benchmark.go` | Pass-through al puerto analítico |
| `internal/clientes/app/obtener_benchmark_test.go` | Tests de delegación/degradación/propagación de error |

## Archivos modificados

| Archivo | Qué cambió |
|---------|-----------|
| `internal/analytics/analytics_contracts.go` | Añade `MetricaBenchmark` y `BenchmarkContract` |
| `internal/analytics/ports/outbound/repos.go` | Añade `ListCandidatosByZona` a `WinbackRepo` |
| `internal/analytics/app/service_test.go` | Añade `ListCandidatosByZona` a `fakeWinbackRepo` |
| `internal/analytics/app/export_test.go` | Exporta `ExportPercentilEnCohorte` |
| `internal/analytics/infra/analyticsfb/repo.go` | Implementación Firebird de `ListCandidatosByZona` |
| `internal/analytics/infra/analyticsfb/repo_test.go` | Test de integración FB de `ListCandidatosByZona` |
| `internal/clientes/ports/outbound/analytics_client.go` | Añade `ObtenerBenchmark` a `AnalyticsClient` |
| `cmd/api/clientes_wiring.go` | Añade `ObtenerBenchmark` al adaptador cross-module |
| `internal/clientes/app/service_test.go` | Añade `ObtenerBenchmark` al fake |
| `internal/clientes/infra/clienteshttp/dto.go` | Añade `ObtenerBenchmarkInput/Output`, `MetricaDTO`, `MetricaMoneyDTO`, `BenchmarkDTO` |
| `internal/clientes/infra/clienteshttp/dto_mapper.go` | Añade `benchmarkToDTO`, `metricaToDTO`, `metricaMoneyToDTO`; elimina header duplicado |
| `internal/clientes/infra/clienteshttp/handlers_clientes.go` | Handler `ObtenerBenchmark` + assertion de compilación |
| `internal/clientes/infra/clienteshttp/routes.go` | Registra operación `obtener-benchmark` |
| `internal/clientes/infra/clienteshttp/handlers_test.go` | Añade `ObtenerBenchmark` a `fakeAnalytics` + 4 tests del handler |
| `internal/clientes/infra/clienteshttp/openapi_test.go` | Añade `ObtenerBenchmark` a `stubAnalytics` |
| Stubs en analytics/app/coverage_internal_test.go, analyticshttp/handlers_test.go, analyticshttp/openapi_test.go, clientes/app/reconciliar_directorio_integration_test.go | Añade `ListCandidatosByZona` / `ObtenerBenchmark` a fakes |

## Decisiones técnicas

- **No logic in DB**: `ListCandidatosByZona` devuelve rows crudas; toda la computación de percentil/cuantil es Go puro.
- **`benchmarkMuestraMinima = 30`**: cuando N < 30, `MuestraPequena=true` y el campo `Percentil` se omite (queda en 0).
- **`ventanaAntiguedadMeses = 6`**: sub-filtro de antigüedad usa `monthIndex` para comparar sin problemas de horario de verano.
- **Extracción de helpers**: `computeBenchmarkScores` y `benchmarkPeerVectors` reducen la complejidad ciclomática de `ObtenerBenchmark` de 17 a ≤15 (límite del linter).
- **CLV como string en la API**: `MetricaMoneyDTO` serializa valores monetarios como strings de 2 decimales (mismo patrón que predicciones).

## Resultado

```
go build ./...                         ✓ (nativo + cross-compile Windows)
go test ./internal/analytics/...       ✓ todos los paquetes verdes
go test ./internal/clientes/...        ✓ todos los paquetes verdes
golangci-lint run ...                  0 issues
```

## Fix pass (review findings #1/#2/#4)

### Finding #1 — Cuantiles en muestra pequeña no se zeroeaban

**Archivo:** `internal/analytics/app/benchmark_query.go`, `buildMetricaBenchmark`

El bloque anterior fijaba `Mediana`, `P25`, `P75` incondicionalmente (antes de verificar `muestraPequena`) y luego solo condicionaba `Percentil`. Ahora los cuatro campos (`Percentil`, `Mediana`, `P25`, `P75`) se asignan únicamente en el bloque `if !muestraPequena`. El struct se inicializa solo con `Aplica`, `Valor`, `N` y `MuestraPequena`.

**Test actualizado:** `internal/analytics/app/benchmark_query_test.go`, `TestObtenerBenchmark_MuestraPequena` — se agregaron tres aserciones `InDelta(0.0, ...)` para `Mediana`, `P25` y `P75`.

### Finding #2 — Sin test del cohort_by por defecto en el handler

**Archivo:** `internal/clientes/infra/clienteshttp/handlers_test.go`

Se añadió `TestObtenerBenchmark_DefaultCohortBy_200`: envía `GET /clientes/42/benchmark` sin parámetro `cohort_by`, usa `fakeAnalyticsWithBenchmark` con `BenchmarkContract{CohortBy: "zona"}` y afirma que la respuesta serializa `"cohort_by": "zona"`. Consistente con la estructura de los demás tests del handler.

### Finding #4 — Sin aserción de conteo exacto para zonaA en el test de integración FB

**Archivo:** `internal/analytics/infra/analyticsfb/repo_test.go`, `TestRepo_ListCandidatosByZona`

Se agregó `require.Len(t, resultA, 2, "zonaA must return exactly 2 candidatos")` justo después de `ListCandidatosByZona`. El test de zonaB ya tenía `require.Len(t, resultB, 1, ...)`.

### Salida de verificación

```
go test ./internal/analytics/app/ -run 'TestObtenerBenchmark|TestPercentil' -count=1
ok      github.com/abdimuy/msp-api/internal/analytics/app       0.443s

go test ./internal/clientes/infra/clienteshttp/ -run 'Benchmark' -count=1
ok      github.com/abdimuy/msp-api/internal/clientes/infra/clienteshttp 0.293s

go vet ./internal/analytics/infra/analyticsfb/
(sin salida — compila; FB_DATABASE requerido para correr el test de integración)

go build ./...
(sin salida — ok)

golangci-lint run ./internal/analytics/... ./internal/clientes/...
0 issues.
```
