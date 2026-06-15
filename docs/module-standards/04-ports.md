# Step 04 — Ports

> Applies to: Type A (CRUD), Type B (Pipeline), Type C (Microsip-synced).
> Depends on: Step 01 (domain entities), Step 02 (value objects + errors).
> Parallel with: Step 05 (app services).

> **Adaptado de `ancla-api`, reconciliado con el código real de msp-api.**
> Diferencias clave vs. ancla:
> (1) **No `ports/inbound/`** — los handlers llaman directamente al servicio concreto; ancla menciona la posibilidad; msp-api la omite por convención.
> (2) **Paginación propia (`ListParams`/`Page[T]`)** — msp-api no reutiliza `platform/pagination.CursorParams`; cada módulo declara su propio `ListParams` y `Page[T]` en `repos.go`.
> (3) **`interfacebloat` nolint explícito** — repositorios complejos (`VentaRepo`, `UsuarioRepo`) anotan `//nolint:interfacebloat` con justificación.
> (4) **Microsip-specific ports** — msp-api tiene ports extra para escribir en Microsip (`MicrosipVentaWriter`, `MicrosipClienteWriter`) y para leer config de mapeo (`AplicarConfig`); ancla no los necesita.
> (5) **`ProductionClock` vive en el port** — el struct concreto de reloj se declara en `clock.go`, no en infra; ancla no tiene clock como port.
> (6) **Type alias para platform ports** — `ImageProcessor` en `image_processor.go` es un type alias de `imageprocessor.Processor`; mantiene el borde hexagonal sin duplicar la definición.
> (7) **Sentinel de error en el port** — `ErrFirebaseTransient` vive en `firebase_client.go` (port), no en domain ni infra; los ports externos pueden declarar sus propios sentinelas.

---

## Comparación ancla vs. msp-api (real)

| Concepto | ancla-api | msp-api (real) |
|---|---|---|
| Paginación | `platform/pagination.CursorParams` embebido en el filtro | `ListParams{Cursor, PageSize}` + `Page[T]{Items, NextCursor}` declarados por módulo en `repos.go` |
| Filtro | struct con campos pointer, sin métodos | `ListVentasFilters` con mix de pointers y bool; string vacío = sin filtro |
| `List` signature | `([]*domain.{E}, bool, error)` — items + hasMore | `(Page[*domain.{E}], error)` — struct genérico |
| Storage port | `Store(ctx, id uuid.UUID, filename, reader) (string, error)` | `Store(ctx, key, contentType string, sizeBytes int64, body io.Reader) error` — key como string, sin uuid |
| Eventos port | `{Entity}EventPublisher` con `Publish(ctx, event)` | `OutboxEnqueuer` con `Enqueue(ctx, aggregate, aggregateID, eventType, payload)` — interfaz genérica de outbox |
| Clock | no existe | `Clock` interface + `ProductionClock` struct en `clock.go` |
| Cross-module client | importa contract types del productor | tipos locales (`InventarioStockItem`, `InventarioCrearTraspasoParams`) + interfaz local; adapter en `cmd/api/` mapea |
| Pipeline-specific ports | connectors, parsers, LLM | Microsip writers (`MicrosipVentaWriter`, `MicrosipClienteWriter`), `AplicarConfig`, `ImageProcessor` |
| Inbound ports | mencionados pero no recomendados | no existen — no crear |
| Platform ports (Transaction, Audit) | reutilizados desde platform | `firebird.TxManager` inyectado en infra; no expuesto como port de módulo |

---

## Archivos a crear

```
internal/{module}/ports/outbound/repos.go            # ListParams, Page[T], filtros, repo interfaces
internal/{module}/ports/outbound/outbox.go           # OutboxEnqueuer
internal/{module}/ports/outbound/clock.go            # Clock + ProductionClock
internal/{module}/ports/outbound/storage.go          # StorageProvider (solo si el módulo guarda blobs)
internal/{module}/ports/outbound/{concepto}.go       # un archivo por port adicional según necesidad
```

Solo crea los archivos que el módulo realmente necesita. **No ports vacíos.**

El package es siempre `outbound`. No hay subdirectorios dentro de `ports/outbound/`.

---

## No hay `ports/inbound/`

Los handlers dependen directamente del tipo concreto del servicio de la capa `app`. No existe `ports/inbound/` en msp-api.

Si otro módulo necesita consumir este módulo, lo hace a través del port `{modulo}_contracts.go` con tipos de contrato (ver Step 03). El cliente declara su propia interfaz local (ver sección de cross-module clients).

---

## Port de repositorio

### Archivo: `repos.go`

Contiene `ListParams`, `Page[T]`, los filtros de listado y todas las interfaces de repositorio del módulo en un solo archivo (o partidos por entidad si el módulo es grande). Los filtros son semánticamente parte del contrato del repo.

### Tipos de paginación

```go
package outbound

// ListParams is the cursor-pagination input accepted by every List method.
// Cursor is opaque to the caller (server encodes/decodes it); PageSize is
// the desired page size, with the repo applying its own minimum/maximum if
// necessary.
type ListParams struct {
    Cursor   string
    PageSize int
}

// Page is the generic cursor-paginated result returned by List methods.
// NextCursor is the empty string when there are no more pages.
type Page[T any] struct {
    Items      []T
    NextCursor string
}
```

### Type A — repositorio CRUD

```go
package outbound

import (
    "context"

    "github.com/google/uuid"

    "github.com/abdimuy/msp-api/internal/{module}/domain"
)

// {Entity}Repo persists and retrieves {Entity} aggregates.
type {Entity}Repo interface {
    Save(ctx context.Context, e *domain.{Entity}) error
    Update(ctx context.Context, e *domain.{Entity}) error
    FindByID(ctx context.Context, id uuid.UUID) (*domain.{Entity}, error)
    List(ctx context.Context, p ListParams, f {Entity}Filter) (Page[*domain.{Entity}], error)
}
```

Métodos opcionales — añadir solo si la entidad los necesita:

```go
    FindByEmail(ctx context.Context, email string) (*domain.{Entity}, error)
    FindByNombre(ctx context.Context, nombre string) (*domain.{Entity}, error)
    ExisteConID(ctx context.Context, id uuid.UUID) (bool, error)
```

### Type A — ejemplo real: `UsuarioRepo` (auth)

```go
// UsuarioRepo persists and retrieves Usuario entities and their
// rol/permiso associations.
//
//nolint:interfacebloat // contract defined by auth module phase-1 spec
type UsuarioRepo interface {
    Save(ctx context.Context, u *domain.Usuario) error
    Update(ctx context.Context, u *domain.Usuario) error
    FindByID(ctx context.Context, id uuid.UUID) (*domain.Usuario, error)
    FindByFirebaseUID(ctx context.Context, firebaseUID string) (*domain.Usuario, error)
    FindByEmail(ctx context.Context, email string) (*domain.Usuario, error)
    List(ctx context.Context, p ListParams) (Page[*domain.Usuario], error)
    AsignarRol(ctx context.Context, usuarioID, rolID, by uuid.UUID, now time.Time) error
    RevocarRol(ctx context.Context, usuarioID, rolID uuid.UUID) error
    PermisosFor(ctx context.Context, usuarioID uuid.UUID) ([]domain.Permission, error)
    RolesFor(ctx context.Context, usuarioID uuid.UUID) ([]*domain.Rol, error)
}
```

### Aggregate complejo — ejemplo real: `VentaRepo` (ventas)

Cuando el agregado tiene colecciones hija (`combos`, `productos`, `vendedores`, `imágenes`) que se actualizan por separado, el repo expone métodos de escritura granular en lugar de un `Update` monolítico:

```go
// VentaRepo persists and retrieves Venta aggregates as a single unit.
//
//nolint:interfacebloat // one method per aggregate-level mutation; cohesive.
type VentaRepo interface {
    Save(ctx context.Context, v *domain.Venta) error
    Update(ctx context.Context, v *domain.Venta) error
    UpdateHeader(ctx context.Context, v *domain.Venta) error
    UpdateCliente(ctx context.Context, v *domain.Venta) error
    ReplaceProductos(ctx context.Context, v *domain.Venta) error
    ReplaceCombos(ctx context.Context, v *domain.Venta) error
    ReplaceVendedores(ctx context.Context, v *domain.Venta) error
    FindByID(ctx context.Context, id uuid.UUID) (*domain.Venta, error)
    LockByID(ctx context.Context, id uuid.UUID) error
    List(ctx context.Context, p ListParams, f ListVentasFilters) (Page[*domain.Venta], error)
    InsertImagen(ctx context.Context, ventaID uuid.UUID, img *domain.Imagen) error
    DeleteImagen(ctx context.Context, ventaID, imagenID uuid.UUID) error
}
```

### Reglas

- Todos los métodos aceptan `context.Context` como primer parámetro.
- Aceptan y devuelven tipos de dominio (`*domain.{Entity}`), nunca tipos de DB, DTO ni contratos de otro módulo.
- `FindByID` devuelve el sentinel de dominio correspondiente en miss (`ErrVentaNotFound`, `ErrUsuarioNotFound`).
- `List` devuelve `(Page[*domain.{Entity}], error)`.
- Si la interfaz crece por mandato del spec (no por descuido), anotar `//nolint:interfacebloat` con un comentario de justificación.

---

## Struct de filtro

Vive en el mismo archivo que la interfaz del repositorio.

```go
// List{Entity}Filters is the structured filter set accepted by {Entity}Repo.List.
// All pointer fields are optional: nil disables that filter.
type List{Entity}Filters struct {
    // Desde restricts to entities with {fecha} >= Desde.
    Desde *time.Time
    // Hasta restricts to entities with {fecha} < Hasta.
    Hasta *time.Time
    // Status restricts to a specific status. Empty string disables.
    Status string
}
```

### Ejemplo real — `ListVentasFilters` (ventas)

```go
type ListVentasFilters struct {
    Desde             *time.Time
    Hasta             *time.Time
    VendedorUsuarioID *uuid.UUID
    ClienteID         *int
    TipoVenta         string   // string vacío = sin filtro
    Situacion         string
    Sincronizacion    string
    IncluirCanceladas bool
}
```

### Reglas

1. Campos pointer cuando el zero value del tipo es un valor legítimo (e.g. `*time.Time`, `*uuid.UUID`, `*int`).
2. Campos string planos cuando el string vacío es el convenio de "sin filtro" (e.g. `TipoVenta string`).
3. `bool` para flags de inclusión/exclusión (`IncluirCanceladas`).
4. Sin métodos. Los filtros son data pura.
5. Sin paginación embebida; `ListParams` se pasa como argumento separado a `List`.

---

## Port de outbox

### Archivo: `outbox.go`

Interfaz idéntica en todos los módulos. Copiar literalmente y cambiar solo el package doc.

```go
package outbound

import (
    "context"

    "github.com/google/uuid"
)

// OutboxEnqueuer hands a domain event to the platform outbox for at-least-
// once delivery to downstream consumers.
//
// The Enqueue contract is intentionally best-effort: callers proceed even
// when enqueueing fails — the failure is logged with the payload so it can
// be replayed manually, and the database transaction that owns the business
// write is never blocked by an outbox hiccup.
type OutboxEnqueuer interface {
    Enqueue(
        ctx context.Context,
        aggregate string,
        aggregateID uuid.UUID,
        eventType string,
        payload any,
    ) error
}
```

La implementación vive en `internal/{module}/infra/{module}outbox/enqueuer.go`. Ver Step 06 (infra) y ADR-0008.

---

## Port de reloj

### Archivo: `clock.go`

**El package doc del módulo va aquí** (único archivo con `// Package outbound ...`).

```go
// Package outbound declares the interfaces the {module} module needs from the
// outside world. Implementations live in internal/{module}/infra/* and are
// wired together at composition root via fx providers.
package outbound

import "time"

// Clock returns the current wall-clock time. Services depend on this port
// instead of calling time.Now() directly, so tests can substitute a fixed
// or controllable clock.
type Clock interface {
    Now() time.Time
}

// ProductionClock is the real-world implementation of Clock. It always
// returns UTC so timestamps inserted into the database are normalized at
// the source.
type ProductionClock struct{}

// Now returns the current wall-clock time in UTC.
func (ProductionClock) Now() time.Time { return time.Now().UTC() }
```

`ProductionClock` se declara aquí (no en infra) porque no tiene dependencias externas. Los tests usan un `fixedClock` local que implementa `Clock`.

---

## Port de almacenamiento

### Archivo: `storage.go`

Solo en módulos que guardan blobs (imágenes, documentos). El único módulo con este port actualmente es `ventas`.

```go
package outbound

import (
    "context"
    "io"
)

// StorageObject is the result of a Get call from a StorageProvider.
// The caller MUST close Body.
type StorageObject struct {
    Body        io.ReadCloser
    ContentType string
    SizeBytes   int64
}

// StorageProvider abstracts the binary blob backing store for {module} uploads.
// The only implementation is FilesystemProvider (see ADR-0003).
// Implementations must reject keys with path-traversal segments (..),
// null bytes, absolute paths, or backslashes.
type StorageProvider interface {
    // Store writes a new blob under the given key. Overwrites if already present.
    Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error

    // Get fetches a blob by key. The caller MUST close obj.Body.
    Get(ctx context.Context, key string) (StorageObject, error)

    // Delete removes the blob at key. Idempotent: nil when already absent.
    Delete(ctx context.Context, key string) error
}
```

`key` es un string libre (no `uuid.UUID`) con el formato `{ventaID}/{imagenID}.{ext}`. El módulo controla la forma de la clave; la implementación la trata como opaca.

---

## Ports de cross-module clients

### Un archivo por módulo consumido: `{modulo}_client.go`

Cuando este módulo necesita datos o comandos de otro módulo, declara una interfaz local con los tipos de datos que necesita. El adapter concreto vive en `cmd/api/` y mapea entre los tipos del módulo productor y los tipos locales.

**Principio:** el módulo consumidor no importa el paquete `domain` del módulo productor, solo su paquete raíz (contracts). Si ni eso es posible (evitar ciclos), declara tipos locales y el adapter hace el mapeo.

### Ejemplo real — `InventarioService` (ventas consume inventario)

```go
// inventario.go
package outbound

import (
    "context"
    "time"

    "github.com/google/uuid"
    "github.com/shopspring/decimal"
)

// InventarioStockItem mirrors inventario.ValidarStockItem — declared locally
// so this package's clients depend only on the outbound port.
type InventarioStockItem struct {
    ArticuloID    int
    AlmacenOrigen int
    Cantidad      decimal.Decimal
}

type InventarioCrearTraspasoParams struct {
    VentaID       uuid.UUID
    AlmacenOrigen int
    Fecha         time.Time
    Descripcion   string
    Detalles      []InventarioTraspasoDetalle
    CreatedBy     uuid.UUID
}

// InventarioService is the slice of the inventario module's command surface
// that the ventas module consumes.
type InventarioService interface {
    ValidarStockParaVenta(ctx context.Context, items []InventarioStockItem) error
    CrearTraspasoParaVenta(ctx context.Context, p InventarioCrearTraspasoParams) (int, error)
    CrearTraspasoReverso(ctx context.Context, ventaID, by uuid.UUID) (int, error)
    ResincronizarTraspasoParaVenta(ctx context.Context, p InventarioCrearTraspasoParams) (int, error)
}
```

### Ejemplo real — existence checkers (ventas)

Ports de single-method para validar la existencia de una FK antes de un INSERT, evitando que el error llegue como FK violation genérico de Firebird:

```go
// ClienteExistenceChecker is consulted by the ventas service to validate
// that an optional cliente_id points to a real row in Microsip CLIENTES.
type ClienteExistenceChecker interface {
    Exists(ctx context.Context, clienteID int) (bool, error)
}

// VendedorUsuarioExistenceChecker validates that every vendedor on a
// CrearVenta request has a matching row in MSP_USUARIOS.
type VendedorUsuarioExistenceChecker interface {
    // MissingIDs returns the subset of ids that have no matching row.
    // Returns empty slice (never nil) when every id is present.
    MissingIDs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
}
```

### Reglas

- El módulo consumidor declara sus tipos de input/output localmente (no importa `{productor}/domain`).
- El adapter en `cmd/api/{modulo}_wiring.go` hace el mapeo entre los tipos del productor y los tipos locales.
- Si el port tiene solo un método, un single-method interface está bien.
- Nombres en inglés; comentarios en inglés; mensajes de error en español.

---

## Ports de integración con Microsip

### Archivos: `microsip_{concepto}_writer.go`, `aplicar_config.go`

Ports específicos de msp-api (no existen en ancla). Abstraen la escritura en las tablas de Microsip y la resolución de mappings de configuración. Se declaran en `ports/outbound/` igual que cualquier port.

### Ejemplo real — `MicrosipVentaWriter` (ventas)

```go
// MicrosipVentaInput carries every resolved identifier the adapter needs to
// materialize a venta into Microsip's DOCTOS_PV family.
type MicrosipVentaInput struct {
    Venta            *domain.Venta
    CajaID           int
    CajeroID         int
    VendedorID       int
    VendedorListaIDs [3]int
    SucursalID       int
    FormaCobroID     int
    FormaDePagoID    *int
    CreditoEnMesesID *int
    NumeroDeVendedoresID int
}

type MicrosipVentaResult struct {
    DoctoPVID int
    Folio     string
}

// MicrosipVentaWriter materializes an approved MSP venta into Microsip's
// DOCTOS_PV ledger. Must execute all INSERTs within the caller's ambient
// transaction so the Microsip cascade and the MSP header update are atomic.
type MicrosipVentaWriter interface {
    Aplicar(ctx context.Context, in MicrosipVentaInput) (MicrosipVentaResult, error)
}
```

### Ejemplo real — `AplicarConfig` (ventas)

Port de lectura de configuración (tablas `MSP_CFG_*`). Cada método devuelve un sentinel de dominio específico en miss, para que el operador sepa exactamente qué mapping falta:

```go
type AplicarConfig interface {
    CajaCajero(ctx context.Context, zonaClienteID int) (CajaCajero, error)
    FormaDePagoID(ctx context.Context, frecuencia string) (int, error)
    CreditoEnMesesID(ctx context.Context, plazoMeses int) (int, error)
    NumeroDeVendedoresID(ctx context.Context, n int) (int, error)
    VendedorListaIDs(ctx context.Context, usuarioID uuid.UUID) ([3]int, error)
    Defaults(ctx context.Context) (AplicarDefaults, error)
}
```

---

## Ports de lectura de eventos y resolución de nombres

### Archivos: `outbox_reader.go`, `nombre_resolver.go`

Algunos módulos necesitan un port de lectura sobre el outbox (proyección de historial) y ports de resolución de display names. Estos son ports outbound normales.

### Ejemplo real — `VentaEventReader` + resolvers (ventas)

```go
// VentaEvento is a ventas-owned projection of a platform outbox row.
type VentaEvento struct {
    ID          uuid.UUID
    EventType   string
    Payload     json.RawMessage
    OccurredAt  time.Time
    ActorID     *uuid.UUID
    ActorNombre string
}

// VentaEventReader returns the chronological event timeline for a venta.
type VentaEventReader interface {
    EventosDeVenta(ctx context.Context, ventaID uuid.UUID) ([]VentaEvento, error)
}

// UsuarioNombreResolver maps usuario ids to their display names.
type UsuarioNombreResolver interface {
    NombresPorID(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

// AlmacenNombreResolver maps Microsip almacén ids to their display names.
type AlmacenNombreResolver interface {
    NombresPorID(ctx context.Context, ids []int) (map[int]string, error)
}
```

---

## Port de cliente externo (Firebase)

### Archivo: `firebase_client.go` (auth)

Ports hacia servicios externos (no Microsip, no cross-module) también viven aquí. Pueden declarar sus propios sentinelas de error cuando el tipo de falla importa para la lógica de reintentos:

```go
// ErrFirebaseTransient is the sentinel implementations return when a call fails
// due to a temporary condition (network error, 5xx, rate limit).
var ErrFirebaseTransient = errors.New("firebase: transient failure")

// FirebaseClient is the auth module's outbound port to Firebase Authentication.
type FirebaseClient interface {
    VerifyIDToken(ctx context.Context, idToken string) (*FirebaseToken, error)
    DisableUser(ctx context.Context, uid string) error
    EnableUser(ctx context.Context, uid string) error
}
```

El input/output type (`FirebaseToken`) se declara en el mismo archivo. El sentinel `ErrFirebaseTransient` se declara también en el port (no en domain ni infra) porque es un detalle del contrato de comunicación con Firebase.

---

## Type alias para ports de plataforma

### Archivo: `image_processor.go` (ventas)

Cuando el port es exactamente la interfaz de un paquete de plataforma, se puede usar un type alias en lugar de redeclarar:

```go
package outbound

import "github.com/abdimuy/msp-api/internal/platform/imageprocessor"

// ImageProcessor is the ventas module's view of the platform image processor.
// Type alias so the platform package owns the canonical shape while consumers
// depend on a module-local port — keeping the hexagonal boundary intact.
type ImageProcessor = imageprocessor.Processor

type ImageProcessorInput  = imageprocessor.Input
type ImageProcessorOutput = imageprocessor.Output
```

Usar type alias (no type definition) cuando la interfaz es idéntica y no se quiere perder la compatibilidad con implementaciones del paquete platform.

---

## Constantes de catálogo en el port

### Archivo: `cliente_defaults.go` (ventas)

Valores de catálogo Microsip necesarios en la capa `app` (para construir inputs a ports) pero sin comportamiento se pueden declarar en `ports/outbound/` como constantes tipadas:

```go
package outbound

const (
    DefaultCondPagoID     = 21497 // CONDS_PAGO: contado
    DefaultTipoClienteID  = 21499 // TIPOS_CLIENTES: particular
    DefaultMonedaID       = 1     // MONEDAS: MXN
    // ...
)
```

Esto evita que la capa `app` importe `infra` para obtener estos valores.

---

## Estructura de directorios tras este paso

```
internal/{module}/
├─ {module}_contracts.go
├─ {module}_contracts_mapper.go
├─ domain/
│  └─ ...
└─ ports/
   └─ outbound/
      ├─ clock.go                        # Clock + ProductionClock; package doc del módulo
      ├─ outbox.go                       # OutboxEnqueuer
      ├─ repos.go                        # ListParams, Page[T], filtros, {Entity}Repo(s)
      ├─ storage.go                      # StorageProvider (solo si el módulo guarda blobs)
      ├─ inventario.go                   # InventarioService (cross-module, ventas→inventario)
      ├─ microsip_venta_writer.go        # MicrosipVentaWriter (ventas — Microsip write)
      ├─ microsip_cliente_writer.go      # MicrosipClienteWriter (ventas — Microsip write)
      ├─ aplicar_config.go              # AplicarConfig (ventas — config MSP_CFG_*)
      ├─ outbox_reader.go               # VentaEventReader + resolvers (ventas — event history)
      ├─ image_processor.go             # ImageProcessor type alias (ventas)
      ├─ cliente_defaults.go            # constantes de catálogo Microsip (ventas)
      └─ firebase_client.go             # FirebaseClient + ErrFirebaseTransient (auth)
```

Para un módulo simple (tipo `auth`, sin blobs, sin Microsip writers), el conjunto mínimo es:

```
ports/outbound/
├─ clock.go
├─ outbox.go
└─ repos.go
```

---

## Agent checklist

- [ ] No existe `ports/inbound/` — los handlers llaman al servicio directamente.
- [ ] `clock.go` contiene `Clock` interface + `ProductionClock` struct + el package doc del módulo.
- [ ] `outbox.go` contiene `OutboxEnqueuer` con la firma canónica de 5 argumentos.
- [ ] `repos.go` contiene `ListParams`, `Page[T]`, el struct de filtro y la(s) interfaz(ces) de repositorio.
- [ ] Todos los métodos de repositorio aceptan `context.Context` como primer parámetro.
- [ ] Repositorios aceptan y devuelven tipos de dominio (`*domain.{Entity}`), nunca tipos DB, DTO o contract.
- [ ] `FindByID` devuelve el sentinel de dominio correspondiente en miss; nunca devuelve `nil, nil`.
- [ ] `List` devuelve `(Page[*domain.{Entity}], error)`.
- [ ] Interfaces con muchos métodos por mandato de spec llevan `//nolint:interfacebloat` con comentario justificado.
- [ ] Cross-module clients declaran tipos locales; no importan `{productor}/domain`.
- [ ] Ports de Microsip (`MicrosipVentaWriter`, `AplicarConfig`) viven en `ports/outbound/`, no en infra.
- [ ] Type alias para ports de plataforma cuando la interfaz es idéntica (no redefinir).
- [ ] Sentinelas de error del port (e.g. `ErrFirebaseTransient`) declarados en el archivo del port.
- [ ] Constantes de catálogo que la capa `app` necesita declaradas en `ports/outbound/` (no en infra).
- [ ] `storage.go` solo existe si el módulo maneja blobs; no crear por "si acaso".
- [ ] Dinero como `decimal.Decimal`, nunca `float64`.
- [ ] Fechas en UTC; timestamps del dominio no se wrappean con `firebird.ToWallClock` aquí (eso es infra).
- [ ] Ningún archivo de port importa `internal/{module}/infra/...` ni paquetes de DB.
- [ ] El módulo path es `github.com/abdimuy/msp-api/...` en todos los imports.
