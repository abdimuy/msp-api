# Task B5 — Timeline unificado de compras y pagos — Reporte

## Archivos creados/modificados

| Archivo | Cambio |
|---------|--------|
| `internal/clientes/domain/timeline.go` | NUEVO — `EventoTimeline`, constantes `TipoCompra*`/`TipoPago`, `BuildTimeline` pura |
| `internal/clientes/domain/timeline_test.go` | NUEVO — 17 tests unitarios + 4 property tests (rapid) |
| `internal/clientes/app/obtener_timeline.go` | NUEVO — `Service.ObtenerTimeline` |
| `internal/clientes/app/obtener_timeline_test.go` | NUEVO — 7 tests con fake repo |
| `internal/clientes/infra/clienteshttp/dto.go` | MOD — DTOs `ObtenerTimelineInput/Output`, `EventoTimelineDTO`, `TimelineDTO` |
| `internal/clientes/infra/clienteshttp/dto_mapper.go` | MOD — `timelineToDTO` |
| `internal/clientes/infra/clienteshttp/handlers_clientes.go` | MOD — handler `ObtenerTimeline` + aserción de tipo |
| `internal/clientes/infra/clienteshttp/routes.go` | MOD — `GET /clientes/{id}/timeline` (`obtener-timeline`) |
| `internal/clientes/infra/clienteshttp/handlers_test.go` | MOD — 6 tests de handler |

## Decisión de alcance: liquidacion

`liquidacion` NO se emite en v1. Detectar un saldo-cero desde datos crudos es
aproximado: los intereses y cargos de crédito no se reflejan en `VentaCruda.Total`,
y los pagos sin `DoctoPVID` resoluble no pueden atribuirse fiablemente a una venta
específica. `Tipo` es un string extensible (no un iota/enum) para permitir añadir
`liquidacion` y `visita` en una rebanada futura con reconstrucción de saldo confiable.

## Salidas de verificación

### Build
```
go build ./...                                         → 0 errores
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./... → 0 errores
```

### Tests
```
go test ./internal/clientes/... -count=1
ok  clientes/app          0.44s
ok  clientes/domain       0.87s
ok  clientes/infra/clientesfb     0.62s
ok  clientes/infra/clienteshttp   1.55s
ok  clientes/infra/clientespdf    1.10s
ok  clientes/infra/clientessearch 1.27s
```
28 tests nuevos (17 domain + 4 property + 7 app + 6 handler) — todo PASS.

### Cobertura de archivos nuevos
| Archivo | Cobertura |
|---------|-----------|
| `domain/timeline.go` → `BuildTimeline` | 100% |
| `app/obtener_timeline.go` → `ObtenerTimeline` | 91.7% |
| `handlers_clientes.go` → `ObtenerTimeline` | 100% |
| `dto_mapper.go` → `timelineToDTO` | 100% |

### Lint
```
golangci-lint run ./internal/clientes/... → 0 issues
```

## Concerns

Ninguno. El `gosec` G101 (falso positivo sobre `TipoCompraCredito`) fue suprimido
con `//nolint:gosec` documentado. El `gofumpt extra-rules` requirió colapsar
params del mismo tipo en los helpers de test (`importe, doctoCCID int`).
