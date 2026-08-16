# Task B5 — Timeline unificado compras + pagos (clientes-only)

## Dónde encaja
Repo **msp-api**, módulo `clientes` (SOLO clientes — sin analytics, sin cross-module). Agrega un feed
cronológico que mezcla **compras** (ventas DOCTOS_PV) y **pagos** (abonos) de un cliente, ordenado por
fecha. Se mostrará en el tab "Pagos & solvencia" (eso es F3, frontend). Esta tarea es solo el backend.

**Reutiliza la fuente existente:** `s.repo.ObtenerRitmoPagoData(ctx, clienteID, rango)` ya devuelve
`outbound.RitmoPagoData{ Pagos []domain.PagoCrudo, Ventas []domain.VentaCruda }` — ambas fuentes en un
solo bundle. NO crees un repo nuevo. Lee como plantilla `internal/clientes/app/obtener_ritmo_pago.go`
(método de app + manejo de errores) y el endpoint `obtener-ritmo-pago` en `clienteshttp`.

## Datos disponibles (de `internal/clientes/domain/ritmo_pago.go`)
```go
type VentaCruda struct {
    Fecha time.Time; Total decimal.Decimal; DoctoPvID int; Folio string; EsCredito bool; PlazoMeses int
}
type PagoCrudo struct {
    Fecha time.Time; Importe decimal.Decimal; DoctoCCID int; Concepto string; DoctoPVID int; Folio string; ...
}
```

## Diseño

### 1. Domain: tipo + función pura (en `internal/clientes/domain/`)
Archivo nuevo `timeline.go`:
```go
// EventoTimeline is one entry in a client's unified purchase/payment feed.
type EventoTimeline struct {
    Fecha    time.Time       // UTC
    Tipo     string          // "compra_credito" | "compra_contado" | "pago"  (extensible: "liquidacion","visita")
    Monto    decimal.Decimal // positive amount
    Etiqueta string          // short human label (folio / concepto)
    RefID    int             // DoctoPvID for compras, DoctoCCID for pagos
}

// Event type constants.
const (
    TipoCompraCredito = "compra_credito"
    TipoCompraContado = "compra_contado"
    TipoPago          = "pago"
)

// BuildTimeline merges sale and payment events into a single feed sorted by
// Fecha DESCENDING (most recent first), tie-broken deterministically (e.g. by
// RefID) so the order is stable. Pure: no time.Now(), no I/O.
func BuildTimeline(pagos []PagoCrudo, ventas []VentaCruda) []EventoTimeline
```
Mapeo:
- `VentaCruda` → `EventoTimeline{Fecha: v.Fecha, Tipo: EsCredito?TipoCompraCredito:TipoCompraContado, Monto: v.Total, Etiqueta: v.Folio, RefID: v.DoctoPvID}`.
- `PagoCrudo` → `EventoTimeline{Fecha: p.Fecha, Tipo: TipoPago, Monto: p.Importe, Etiqueta: (p.Concepto si no vacío, si no p.Folio), RefID: p.DoctoCCID}`.
- Orden: por `Fecha` descendente; desempate estable por `RefID` (o por Tipo+RefID). Determinístico.

**Decisión de alcance v1 (documéntala en el reporte):** NO se computan eventos `liquidacion` en v1.
Detectar liquidación desde datos crudos (suma de abonos ≥ total de la venta) es **aproximado** por
intereses/cargos de crédito no reflejados en `VentaCruda.Total` y por pagos sin `DoctoPVID` resoluble —
podría etiquetar mal. Preferimos correctitud: `Tipo` queda como string extensible y `liquidacion`/
`visita` se agregan en una rebanada futura con reconstrucción de saldo por venta confiable.

### 2. App: método en `Service` (`internal/clientes/app/obtener_timeline.go`, NUEVO)
`func (s *Service) ObtenerTimeline(ctx context.Context, clienteID int) ([]domain.EventoTimeline, error)`
1. (Opcional) valida que el cliente existe igual que `ObtenerRitmoPago` (puedes reusar el mismo
   `s.repo.<get cliente>` que ese método usa al inicio; si no aporta, omítelo y deja que el bundle vacío
   degrade a feed vacío). Sigue el patrón de manejo de errores de `obtener_ritmo_pago.go` (apperror con
   source en inglés / mensaje español).
2. Llama `data, err := s.repo.ObtenerRitmoPagoData(ctx, clienteID, rango)` con un **rango sin límites**
   (todo el historial). Verifica cómo `outbound.RangoFechas` representa "sin filtro": si el zero-value
   (`RangoFechas{}` con punteros nil) significa sin bounds, úsalo; si no, pasa un rango muy amplio. NO
   inventes — léelo en el repo/dominio.
3. `return domain.BuildTimeline(data.Pagos, data.Ventas), nil`.

### 3. HTTP: `clienteshttp` (mirror de `obtener-ritmo-pago`)
- `dto.go`:
  ```go
  type ObtenerTimelineInput struct { ID int `path:"id" doc:"ID de Microsip del cliente"` }
  type ObtenerTimelineOutput struct{ Body TimelineDTO }
  type EventoTimelineDTO struct {
      Fecha    string `json:"fecha"    format:"date-time" doc:"RFC3339 UTC del evento"`
      Tipo     string `json:"tipo"     doc:"compra_credito | compra_contado | pago"`
      Monto    string `json:"monto"    doc:"Importe en pesos (2 decimales)"`
      Etiqueta string `json:"etiqueta" doc:"Folio o concepto"`
      RefID    int    `json:"ref_id"   doc:"DOCTO_PV_ID (compra) o DOCTO_CC_ID (pago)"`
  }
  type TimelineDTO struct {
      Eventos []EventoTimelineDTO `json:"eventos" doc:"Feed cronológico (más reciente primero)"`
  }
  ```
- `dto_mapper.go`: `timelineToDTO(eventos []domain.EventoTimeline) TimelineDTO`. Fecha con el formateo
  RFC3339 UTC que ya usa el repo para fechas (mira cómo `ritmoPagoToDTO` formatea fechas — usa el mismo
  helper). Monto con `StringFixed(moneyScale)`.
- `handlers_clientes.go`: handler `ObtenerTimeline` espejo de `ObtenerRitmoPago`:
  `currentUserOrError` → `requirePerm(cu, auth.PermClientesLeer)` → `h.svc.ObtenerTimeline(ctx, input.ID)`
  → `timelineToDTO`. Agrega la aserción de tipo del handler.
- `routes.go`: `huma.Register` OperationID "obtener-timeline", GET `/clientes/{id}/timeline`, copia
  seguridad/tags de `obtener-ritmo-pago`.

## Restricciones (CLAUDE.md)
- Solo `clientes` (sin imports a otros módulos). Fechas UTC RFC3339 hacia el front; dinero string 2dp.
- Código inglés / mensajes español. Sin lógica en BD (no aplica — reusa repo existente).

## Tests (mandato por capa)
- **domain `timeline_test.go`** (≥99% + property `pgregory.net/rapid`): merge correcto (cuenta =
  len(pagos)+len(ventas)); orden descendente por fecha estable; tipos correctos (credito/contado/pago);
  etiqueta usa concepto y cae a folio; vacío → slice vacío (no nil-panic); property: orden monotónico,
  conteo, sin panics con entradas aleatorias.
- **app `obtener_timeline_test.go`** (fake repo, ≥90%): bundle con N ventas + M pagos → feed mezclado y
  ordenado; bundle vacío → feed vacío; error del repo → error envuelto. Reusa el fake repo del paquete.
- **handler** (≥70%): sin permiso → error; con permiso → DTO correcto (fechas RFC3339, montos string,
  orden). Extiende los fakes existentes.
- **composición**: si hay un patrón e2e/composición para ritmo-pago, síguelo; si no es práctico por el
  límite de package main, cubre adapter+app+handler con fakes y documéntalo.
- Output de tests pristino.

## Verificación (pega salida en el reporte)
- `go build ./...` + `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`
- `go test ./internal/clientes/... -count=1` + cobertura de archivos nuevos
- `golangci-lint run ./internal/clientes/...` → 0 issues

## Reporte
`docs/superpowers/plans/r2r3-B5-report.md` (archivos, decisión de alcance liquidacion, salida de
build/test/cobertura/lint, concerns). Devuelve solo (≤15 líneas): Status, commit SHA+subject, resumen de
tests en 1 línea, concerns, ruta del reporte.

## Commit
Rama `feat/cliente-360-r2-r3`. Mensaje:
`feat(clientes): timeline unificado de compras y pagos`.
Sin `--no-verify`, sin footer de Claude.
