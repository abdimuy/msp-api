# Task B4 — Tendencia sobre la serie de abonos/mes (clientes-only, puro)

## Dónde encaja
Repo **msp-api**, módulo `clientes` (solo clientes). La ficha ya expone series de tiempo en
`FichaDTO.Series` (`AbonosPorMes`, `CompradoVsAbonado`). Esta tarea agrega un cálculo de **tendencia**
(pendiente de regresión lineal + bandera de cambio) sobre la serie **AbonosPorMes** y lo expone en un
bloque nuevo `tendencia{slope, direccion, cambio}` dentro de `SeriesDTO`. Es código **puro** (sin
`time.Now()`, sin I/O). NO crea endpoint nuevo — extiende la ficha existente. La consume F4 (sparklines).

Archivos a leer primero: `internal/clientes/app/obtener_ficha.go` (`FichaCliente`, `ObtenerFicha`),
`internal/clientes/infra/clienteshttp/dto_mapper.go` (`toFichaDTO`, `toSeriesDTO`),
`internal/clientes/infra/clienteshttp/dto.go` (`SeriesDTO`), y la fuente de la serie:
`outbound.ResumenFicha.AbonosPorMes []PuntoMensual{Mes int, Monto decimal.Decimal}`.

## Diseño

### 1. Domain: `internal/clientes/domain/tendencia.go` (NUEVO, puro)
```go
// Tendencia describes the linear trend of a monthly series plus a change flag.
type Tendencia struct {
    Slope     float64 // least-squares slope (amount per month step); 0 when n<2
    Direccion string  // DireccionMejorando | DireccionEstable | DireccionEmpeorando
    Cambio    bool    // true when the last point deviates notably from the prior mean
}

const (
    DireccionMejorando  = "mejorando"
    DireccionEstable    = "estable"
    DireccionEmpeorando = "empeorando"
)

// CalcularTendencia computes the trend of a monthly series. valores are the
// per-month amounts in chronological order. Pure; deterministic; no I/O.
//   - n < 2          → {Slope:0, Direccion:"estable", Cambio:false}
//   - Slope          = least-squares slope of valores over index 0..n-1
//   - Direccion      = "mejorando" if Slope >  umbral, "empeorando" if Slope < -umbral,
//                      else "estable"; umbral = 0.05 * max(mediaAbs, 1.0)  (5% de la escala)
//   - Cambio         = |ultimo - mediaPrevia| > 0.20 * max(mediaPrevia, 1.0)
//                      donde mediaPrevia = promedio de valores[0..n-2]
func CalcularTendencia(valores []float64) Tendencia
```
- `mediaAbs` = promedio de `|valores[i]|` (escala de la serie, para que el umbral sea relativo).
- Regresión: slope = Σ((i-ī)(y-ȳ)) / Σ((i-ī)²) con i=0..n-1; si el denominador es 0 → slope 0.
- Sé robusto a NaN/Inf (no debería entrar, pero no paniquees). Determinístico.

### 2. App: computar en `ObtenerFicha` y adjuntar a `FichaCliente`
- Agrega campo `Tendencia domain.Tendencia` al struct `FichaCliente`.
- En `ObtenerFicha`, tras obtener `resumen`, computa:
  ```go
  valores := make([]float64, len(resumen.AbonosPorMes))
  for i, p := range resumen.AbonosPorMes { valores[i] = p.Monto.InexactFloat64() }
  tendencia := domain.CalcularTendencia(valores)
  ```
  y ponlo en el `FichaCliente{... Tendencia: tendencia}` que retorna.

### 3. HTTP: extender `SeriesDTO` + mapper
- `dto.go`: agrega a `SeriesDTO` el campo
  ```go
  Tendencia TendenciaDTO `json:"tendencia" doc:"Tendencia de la serie de abonos/mes"`
  ```
  y el tipo:
  ```go
  type TendenciaDTO struct {
      Slope     float64 `json:"slope"     doc:"pendiente de la regresión lineal de abonos/mes"`
      Direccion string  `json:"direccion" doc:"mejorando | estable | empeorando"`
      Cambio    bool    `json:"cambio"    doc:"true si el último mes difiere notablemente de la media previa"`
  }
  ```
- `dto_mapper.go`: agrega `func tendenciaToDTO(t domain.Tendencia) TendenciaDTO`. En `toFichaDTO`, como
  `toSeriesDTO` solo recibe `r outbound.ResumenFicha`, compón así:
  ```go
  series := toSeriesDTO(r)
  series.Tendencia = tendenciaToDTO(ficha.Tendencia)
  // ... usa `Series: series` en el FichaDTO
  ```
  (No metas la tendencia dentro de `toSeriesDTO` — esa función no conoce el FichaCliente.)

## Restricciones (CLAUDE.md)
- Solo `clientes`, sin cross-module. Código inglés / comentarios. Puro (sin time.Now/IO/estado global).
- Sin dependencias nuevas (stdlib `math` basta). `pgregory.net/rapid` ya está disponible para el property.

## Tests (mandato por capa)
- **domain `tendencia_test.go`** (≥99% + property `pgregory.net/rapid`):
  - serie creciente (p.ej. [1,2,3,4,5]) → `Slope>0` y `Direccion=="mejorando"`.
  - serie decreciente → `Slope<0`, `"empeorando"`.
  - serie plana/constante → `Slope==0` (o ~0), `"estable"`.
  - cambio detectado: serie estable y un último valor muy distinto → `Cambio==true`; sin cambio → false.
  - `n==0` y `n==1` → `{0,"estable",false}` (sin panic).
  - property: no panics con entradas aleatorias (incluye negativos, ceros); `Direccion` siempre uno de
    los 3 valores; `Slope` finito.
- **app**: extiende `obtener_ficha_test.go` para afirmar que `ObtenerFicha` devuelve `Tendencia`
  computada a partir de `AbonosPorMes` (usa un fake repo con una serie conocida creciente → mejorando).
- **http/mapper**: afirma que `toFichaDTO` propaga la tendencia a `Series.Tendencia` (extiende el test de
  dto_mapper / handler de ficha existente). Reusa los fakes existentes.
- Output de tests pristino.

## Verificación (pega salida en el reporte)
- `go build ./...` + `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`
- `go test ./internal/clientes/... -count=1` + cobertura de `tendencia.go`
- `golangci-lint run ./internal/clientes/...` → 0 issues

## Reporte
`docs/superpowers/plans/r2r3-B4-report.md` (archivos, fórmula/umbral usados, salida de
build/test/cobertura/lint, concerns). Devuelve solo (≤15 líneas): Status, commit SHA+subject, resumen
de tests en 1 línea, concerns, ruta del reporte.

## Commit
Rama `feat/cliente-360-r2-r3`. Mensaje:
`feat(clientes): tendencia de abonos por mes en la ficha`.
Sin `--no-verify`, sin footer de Claude.
