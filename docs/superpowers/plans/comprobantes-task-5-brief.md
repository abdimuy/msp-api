# Comprobantes — Tarea 5: puertos outbound

> **Rama:** `feat/comprobantes-domain` (la misma, encima de lo ya mergeado). Haz `git pull` de `main` primero.
> **Spec:** [`2026-07-29-comprobantes-whatsapp-design.md`](../specs/2026-07-29-comprobantes-whatsapp-design.md)
> **Plan:** tarea `0.5` de la tanda 0
> **Plazo:** entrega el **martes 25 al final de tu jornada**. Ver el calendario al final.

> **Ojo con la numeración.** Este es el brief de la tarea **5**. La `comprobantes-task-3-brief.md` es otra cosa —almacenamiento del PDF y canal local, en otra rama— y por error te la mandé la vez pasada. De ahí vas a copiar dos puertos, pero el brief que gobierna esta tarea es **éste**.

## Dónde encaja

El dominio está completo y mergeado. Falta la frontera: **las interfaces por las que el módulo pide lo que no puede hacer solo.**

Esta tarea no escribe una sola línea de lógica. Escribe once contratos. Y es la que más lejos llega: en cuanto existan, se abren **las nueve tareas de la tanda 1 en paralelo**. Hoy el módulo entero está detenido esperando este archivo.

Por eso el brief trae los puertos **escritos**. Tú los transcribes y ajustas los comentarios; no los diseñas. Una firma mal puesta aquí se propaga a nueve tareas y se descubre tres semanas después.

---

## La regla dura, y es una sola

**Ningún archivo de esta tarea puede importar `internal/ventas`, `internal/clientes`, `internal/cobranza` ni ningún otro módulo.** Sólo `internal/comprobantes/...`, `internal/platform/...` y la biblioteca estándar (más `decimal` y `uuid`).

Si importas otro módulo, `make check-sealed MODULE=comprobantes` falla y `depguard` rechaza el import antes de compilar. No es una convención de estilo: es [ADR-0009](../../adr/0009-asistencia-sealed-module.md), y es lo que permite que este módulo se pueda extraer o borrar sin arrastrar medio repositorio.

El cruce de fronteras existe, pero vive en `infra/clients` y es otra tarea, que no es tuya.

---

## Lo que más fácil se equivoca en esta tarea

**Los lectores devuelven piezas, no comprobantes.**

La tentación es que `VentaReader` devuelva un `domain.ComprobanteVenta` — ya lo escribiste, ya está ahí. No funciona: ese modelo lleva **nombre y domicilio del cliente**, que salen de `ClienteReader`. Y `ComprobantePago` lleva el **saldo restante**, que sale de `SaldoReader`. Un lector que devolviera el modelo completo tendría que leer lo que le toca a otro puerto.

Cada lector devuelve **su pedazo**, en un struct propio de `outbound`, y **la capa de aplicación arma el modelo de dominio** con los pedazos. Eso además es lo que hace que el modelo siga siendo el único lugar donde se valida.

---

## Leer antes de escribir

1. **El spec, §7** (la tabla de puertos), **§2.3** (por qué el cursor agrupa por documento), **§4.4** (el claim atómico) y **§6.2** (qué lleva cada comprobante).
2. **`internal/cobranza/ports/outbound/`** — trece puertos reales, en producción. Como referencia de **forma**: un archivo por puerto, interfaz pequeña, comentario que dice *por qué* existe.
3. **`docs/module-standards/MODULE_TEMPLATE.md`**, sección de puertos.
4. **`comprobantes-task-3-brief.md`**, sólo las dos secciones «El puerto — copiar exactamente». De ahí salen `storage.go` y `sender.go` sin cambiar una letra.

---

## Los once archivos

Todos en `internal/comprobantes/ports/outbound/`, `package outbound`.

### 1. `storage.go` y 2. `sender.go` — copiados

Están escritos verbatim en `comprobantes-task-3-brief.md`. **Cópialos tal cual**, incluidos los comentarios. Si algo no te cuadra, dilo antes de cambiarlo.

### 3. `venta_reader.go`

```go
// DatosVenta is what a receipt needs from a sale. It is NOT domain.ComprobanteVenta:
// that model also carries the client's name and address, which come from
// ClienteReader. The application layer assembles both halves.
type DatosVenta struct {
	Folio        string
	Fecha        time.Time
	Articulos    []ArticuloVenta
	Total        decimal.Decimal
	Enganche     decimal.Decimal
	Saldo        decimal.Decimal
	Parcialidad  decimal.Decimal
	Periodicidad string // "semanal" | "quincenal" | "mensual"
	NumeroPagos  int
	Vendedor     string
}

// ArticuloVenta is one detail line of the sale.
type ArticuloVenta struct {
	Descripcion    string
	Cantidad       decimal.Decimal
	PrecioUnitario decimal.Decimal
	Importe        decimal.Decimal
}

// VentaReader reads the sale behind a receipt. The implementation lives in
// infra/clients and goes through the ventas module's contracts package —
// never through its domain or its tables.
type VentaReader interface {
	Leer(ctx context.Context, ventaID int) (DatosVenta, error)
}
```

### 4. `pago_reader.go`

```go
// DatosPago is what a receipt needs from an applied payment.
type DatosPago struct {
	Folio      string
	Fecha      time.Time
	Monto      decimal.Decimal
	FormaCobro string
	VentaID    int
	VentaFolio string
	Cobrador   string
}

// CambioPago is one entry of the payment changelog: the cursor walks these to
// discover what to enqueue. DoctoCCID is the grouping key — spec §2.3: one
// payment that credits three charges must produce ONE receipt, not three.
type CambioPago struct {
	SeqID     int64
	DoctoCCID int
}

// PagoReader reads applied payments and the changelog that announces them.
//
// Datos reads from DOCTOS_CC, the source — NOT from the MSP_PAGOS_VENTAS
// cache. That distinction is not a preference: reading payment data from the
// cache was a real defect in cobranza, fixed in f665c62 (2026-08-13), and it
// is what left stale coordinates on the phones and forced the sync_epoch
// lever of migration 000055. The cache is for the cursor, not for the data.
type PagoReader interface {
	// Cambios returns changelog entries after seqID, oldest first, at most
	// limite of them.
	Cambios(ctx context.Context, desdeSeqID int64, limite int) ([]CambioPago, error)

	// Datos reads the payment identified by its DOCTOS_CC id.
	Datos(ctx context.Context, doctoCCID int) (DatosPago, error)
}
```

### 5. `saldo_reader.go`

```go
// SaldoReader answers what the client still owes on a sale AFTER a payment is
// applied. It is the number the client looks for first, so it is read at
// render time and never computed inside the domain.
type SaldoReader interface {
	SaldoRestante(ctx context.Context, ventaID int) (decimal.Decimal, error)
}
```

### 6. `cliente_reader.go`

```go
// DatosCliente is the client half of a receipt.
//
// Telefono may be empty: a client with no usable phone is a normal case, and
// the delivery is then created directly in sin_telefono. Do not model this as
// an error.
type DatosCliente struct {
	Nombre    string
	Telefono  string
	Domicilio string
}

// ClienteReader reads the client behind a receipt, through the clientes
// module's contracts package.
type ClienteReader interface {
	Leer(ctx context.Context, clienteID int) (DatosCliente, error)
}
```

### 7. `renderer.go`

```go
// Renderer turns a receipt model into the PDF bytes to store and send.
//
// Both documents carry the mandatory label of spec §6.3 — "comprobante
// informativo, no es un CFDI" — and that belongs to the renderer, not to the
// model: it is presentation, and the model has no presentation.
type Renderer interface {
	Venta(ctx context.Context, c domain.ComprobanteVenta) ([]byte, error)
	Pago(ctx context.Context, c domain.ComprobantePago) ([]byte, error)
}
```

### 8. `envio_repo.go` — el importante

```go
// FiltroEnvios narrows a listing. Zero values mean "no filter".
type FiltroEnvios struct {
	Estado    *domain.EstadoEnvio
	Tipo      *domain.TipoComprobante
	ClienteID *int
	Desde     *time.Time
	Hasta     *time.Time
	Limite    int
	Offset    int
}

// EnvioRepo persists the delivery queue and its log.
//
// The two conditional methods are the heart of the module. Both return a
// bool, not an error, when they lose: losing the race is a NORMAL outcome,
// not a failure. Spec §4.4 — whoever affects the row wins, and the atomicity
// comes from the conditional UPDATE, never from a read-then-write in Go.
type EnvioRepo interface {
	// Guardar inserts or updates a delivery unconditionally. Used for the
	// transitions that do not race.
	Guardar(ctx context.Context, e *domain.Envio) error

	// Obtener reads one delivery by id.
	Obtener(ctx context.Context, id uuid.UUID) (*domain.Envio, error)

	// Listar returns the deliveries matching the filter.
	Listar(ctx context.Context, f FiltroEnvios) ([]*domain.Envio, error)

	// ReclamarLote claims up to limite deliveries that are en_espera with
	// PROGRAMADO_PARA <= now, moving each to enviando in the same statement
	// that selects it. A delivery already claimed by another worker is simply
	// not returned.
	ReclamarLote(ctx context.Context, limite int, now time.Time) ([]*domain.Envio, error)

	// DetenerSiEnEspera moves a delivery to detenido only if it is still
	// en_espera. Returns false when the row was already claimed — that is the
	// "ya_enviado" answer the button shows, and it is a result, not an error.
	DetenerSiEnEspera(ctx context.Context, id uuid.UUID, por string, now time.Time) (bool, error)
}
```

### 9. `cursor_repo.go`

```go
// CursorRepo remembers how far the payment changelog has been walked.
//
// The cursor is the guarantee, not the notification: POST_EVENT only wakes
// the worker earlier. If the cursor is lost or rolled back, receipts are
// re-enqueued, never skipped.
type CursorRepo interface {
	Leer(ctx context.Context) (int64, error)
	Guardar(ctx context.Context, seqID int64, now time.Time) error
}
```

### 10. `config_repo.go`

```go
// Config is the single row of MSP_CM_CONFIG.
type Config struct {
	VentanaCancelacion time.Duration
	VentasActivas      bool
	PagosActivos       bool
	MaxIntentos        int
}

// ConfigRepo reads and updates the module's windows and switches.
type ConfigRepo interface {
	Leer(ctx context.Context) (Config, error)
	Actualizar(ctx context.Context, c Config, now time.Time) error
}
```

### 11. `clock.go`

Copia `internal/ventas/ports/outbound/clock.go`. Mismo contenido, mismo comentario.

---

## Lo que NO va en esta tarea

**No hay `IDGenerator`.** El spec lo lista, pero `CrearEnvio` ya genera el UUID por dentro. Dos fuentes de identificadores es peor que una.

**No hay `comprobantes_contracts.go`.** Ese archivo expone tipos a **otros** módulos, y hoy ningún módulo consume comprobantes — es al revés. Un contrato sin consumidor se diseña mal por definición. Se crea cuando exista el primer consumidor real.

**No hay implementaciones.** Ni una. Son interfaces y structs de datos.

---

## Esta tarea no lleva pruebas, y es a propósito

Un archivo de interfaces no tiene sentencias que ejecutar: no hay nada que cubrir y una prueba de un `interface` no prueba nada. **No escribas mocks para llegar a un número de cobertura** — sería ruido, y el número seguiría sin significar nada.

Las compuertas de esta entrega son estas cinco:

```sh
gofmt -l internal/comprobantes
go vet ./internal/comprobantes/...
go build ./...
golangci-lint run ./internal/comprobantes/...
make check-sealed MODULE=comprobantes
```

`check-sealed` es la que de verdad importa aquí: es la que demuestra que no importaste otro módulo.

---

## Un arreglo de un minuto, antes de empezar

`internal/platform/audit` ya tiene `MarkUpdatedAt(now)`, que entró a `main` ayer. En `envio.go`, cambia:

```go
func (e *Envio) transitionTo(nuevo EstadoEnvio) {
	e.estado = nuevo
	e.MarkUpdated()
}
```

por la forma que recibe el instante y lo pasa: `transitionTo(nuevo, now)` con `e.MarkUpdatedAt(now)`. Con eso el `now` que ya cableaste en las cinco transiciones por fin se honra, y `UpdatedAt` deja de salir del reloj de pared. Agrega una prueba que lo fije: tras `Reclamar(fecha fija)`, `UpdatedAt()` es esa fecha exacta.

Es el cierre de lo que quedó abierto en el PR #12.

---

## Archivos que puedes tocar

```
internal/comprobantes/ports/outbound/storage.go
internal/comprobantes/ports/outbound/sender.go
internal/comprobantes/ports/outbound/venta_reader.go
internal/comprobantes/ports/outbound/pago_reader.go
internal/comprobantes/ports/outbound/saldo_reader.go
internal/comprobantes/ports/outbound/cliente_reader.go
internal/comprobantes/ports/outbound/renderer.go
internal/comprobantes/ports/outbound/envio_repo.go
internal/comprobantes/ports/outbound/cursor_repo.go
internal/comprobantes/ports/outbound/config_repo.go
internal/comprobantes/ports/outbound/clock.go
internal/comprobantes/domain/envio.go          (solo el arreglo de transitionTo)
internal/comprobantes/domain/envio_test.go     (solo la prueba de UpdatedAt)
docs/superpowers/plans/comprobantes-task-5-report.md
```

**Cualquier cambio fuera de esa lista se rechaza sin revisar.** No toques los modelos de contenido, ni los value objects, ni `.golangci.yml`, ni ninguna migración.

---

## El reporte

`docs/superpowers/plans/comprobantes-task-5-report.md`.

Y esta vez con la regla que salió de la tarea anterior, porque nos costó dos vueltas: **el reporte se escribe al final, después del último commit, abriendo los archivos y contando lo que hay.** Escrito aparte, inventa — en la tarea 4 declaraba cinco tipos que no existían. Pega la salida literal de las cinco compuertas corridas sobre el commit final, lista los once puertos con sus métodos, y di qué decidiste donde el brief te dejó elegir.

---

## Calendario y puntos de control

| Cuándo | Qué |
|---|---|
| **Jueves 20 (hoy)** | **Sólo leer.** El brief completo y las dos secciones «El puerto — copiar exactamente» del `task-3`. No empieces a escribir: llegar el viernes sabiendo qué vas a hacer vale más que media jornada de código a ciegas. |
| **Viernes 21** | El arreglo de `transitionTo` y los cuatro puertos copiados o triviales: `storage`, `sender`, `clock`, `saldo_reader`. |
| **Viernes 21, fin de jornada** | **Punto de control: `envio_repo.go` escrito.** Mándamelo. Es el único de los once con diseño real y del que dependen las nueve tareas siguientes. |
| **Lunes 24** | Los seis puertos restantes. |
| **Martes 25, fin de jornada** | **Entrega**, con el reporte. |

El plazo es ajustado y lo sabes de antemano: son ~4 h de trabajo, y con el ritmo de tus tres entregas anteriores eso te da unas 12 h — tres medias jornadas, que son exactamente las que hay. No hay margen para descubrir el viernes que no leíste el brief el jueves.

Por eso el punto de control es el **viernes** y no el lunes: `envio_repo.go` es el único de los once que tiene diseño, y si sale con correcciones prefiero que las apliques el lunes y no el martes a las seis de la tarde.

Si te atoras más de dos horas en una sola cosa, avisa. Y si algo del spec no alcanza para decidir, pregunta antes de escribirlo: en esta tarea, adivinar una firma cuesta nueve tareas de retrabajo.
