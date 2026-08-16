# Task B2 Report — Endpoint de predicciones bayesianas del cliente

## Status
DONE. `go build`, Windows cross-compile, full test suite, and `golangci-lint` all green.

---

## Commits

| SHA | Subject |
|-----|---------|
| `562ebd4` | `feat(clientes): endpoint de predicciones bayesianas del cliente` (main impl) |
| `609b92d` | `feat(clientes): endpoint de predicciones bayesianas del cliente` (3 compilation fixes for existing fakes) |

---

## Files created / modified

### Created
- `internal/analytics/app/predicciones_query.go` — `ObtenerPredicciones` service method + `toPrediccionesContract` mapper
- `internal/analytics/app/predicciones_query_test.go` — 5 tests (historial, not-found, fecha-cero, meses-cero, repo-error)
- `internal/clientes/app/obtener_predicciones.go` — thin pass-through to analytics port
- `internal/clientes/app/obtener_predicciones_test.go` — 3 tests (delegates, degrades, propagates error)

### Modified
- `internal/analytics/analytics_contracts.go` — added `IntervaloContract` + `PrediccionesContract`
- `internal/analytics/app/btyd_posteriors_test.go` — removed unused `newTestRNG`; kept `math/rand` for `TestGammaK1Branch`
- `internal/clientes/ports/outbound/analytics_client.go` — added `ObtenerPredicciones` to interface
- `cmd/api/clientes_wiring.go` — added `ObtenerPredicciones` to `clientesAnalyticsAdapter`
- `internal/clientes/infra/clienteshttp/dto.go` — `ObtenerPrediccionesInput/Output`, `IntervaloDTO`, `IntervaloMoneyDTO`, `PrediccionesDTO`
- `internal/clientes/infra/clienteshttp/dto_mapper.go` — `prediccionesToDTO` (CLV as 2-decimal strings)
- `internal/clientes/infra/clienteshttp/handlers_clientes.go` — handler + compile-time assertion
- `internal/clientes/infra/clienteshttp/routes.go` — `GET /clientes/{id}/predicciones`
- `internal/clientes/app/service_test.go` — `ObtenerPredicciones` stub on `fakeAnalyticsClient`
- `internal/clientes/app/reconciliar_directorio_integration_test.go` — `ObtenerPredicciones` on `b2AnalyticsAdapter`
- `internal/clientes/infra/clienteshttp/handlers_test.go` — handler tests + `TestE2E_ObtenerPredicciones_FullChain`
- `internal/clientes/infra/clienteshttp/openapi_test.go` — `ObtenerPredicciones` on `stubAnalytics`

---

## Build evidence
```
go build ./...                                    → exit 0
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build → exit 0
```

## Test evidence
```
ok  internal/analytics/app        0.44s  coverage: 86.9%
ok  internal/clientes/app         1.47s  coverage: 91.7%
ok  internal/clientes/infra/clienteshttp  1.76s  coverage: 79.3%
ok  cmd/api                       0.44s
(all 17 packages green)
```

### Coverage (new files)
| Function | Coverage |
|----------|----------|
| `analytics/app/predicciones_query.go:ObtenerPredicciones` | 95.5% |
| `analytics/app/predicciones_query.go:toPrediccionesContract` | 100.0% |
| `clientes/app/obtener_predicciones.go:ObtenerPredicciones` | 100.0% |
| `clienteshttp/dto_mapper.go:prediccionesToDTO` | 100.0% |
| `clienteshttp/handlers_clientes.go:ObtenerPredicciones` | 90.9% |

All coverage gates met (analytics ≥90%, clientes app ≥90%, handler ≥70%).

## Lint
```
golangci-lint run ./internal/analytics/... ./internal/clientes/... ./cmd/...
→ 0 issues.
```

---

## Note on x==0 (single purchase month)

When `VentasMesesDistintos == 1`, `clvVGrid` yields `x = 0`. In `Posteriors`, `frequency==0` causes the Gamma-Gamma model to use `ExpectedAvgProfit(0, monetary)` which returns the **population mean ticket** rather than the observed ticket. This is slightly optimistic vs. what `computeCLVConRazones` does (uses observed monetary directly when `x < 1`). Acceptable: the predicciones endpoint is its own Bayesian estimator. Not a bug — do NOT modify B1 to "fix" it.

---

## E2E test note

`TestE2E_ObtenerPredicciones_FullChain` is in `clienteshttp_test` because `clientesAnalyticsAdapter` is `package main` and cannot be imported from tests elsewhere. The compile-time assertion `var _ clientesoutbound.AnalyticsClient = (*clientesAnalyticsAdapter)(nil)` in `clientes_wiring.go` + the 1-line adapter delegation ensure the wiring is correct; the e2e test covers the DTO serialization chain.

---

## Concerns

None.
