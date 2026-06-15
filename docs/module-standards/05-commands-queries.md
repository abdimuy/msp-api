# Step 05 — Commands & Queries

> Applies to: Type A (CRUD), Type B (Pipeline), Type C (Microsip-synced).
> Depends on: Step 01 (domain entities), Step 02 (value objects + errors).
> Parallel with: Step 04 (repositorio + puertos outbound).

> **Adaptado de `ancla-api`, reconciliado con el código real de msp-api.**
> Diferencias clave vs. ancla:
> (1) **Sin subpaquete `services/`** — todo vive en `app/`, package `app`; en Go los métodos no pueden
> abarcar paquetes, por lo que `Service` y sus handlers comparten el mismo package.
> (2) **Nombres en español** para comandos y queries (`CrearVenta`, `ObtenerVenta`), no
> `HandleCreate{Entity}` — el lenguaje ubicuo del negocio es español.
> (3) **Los comandos devuelven el agregado de dominio** (`*domain.Venta`), no un response DTO — la
> proyección a JSON la hace la capa HTTP.
> (4) **Sin `auditLogger`** — msp-api no tiene un audit logger centralizado; el outbox cubre el trail.
> (5) **`runInTx` + `drainEvents` son helpers privados del Service** (no en una interfaz aparte).
> (6) **Dependencias opcionales vía `With*` fluent setters** (inventario, event-reader, resolvers).
> (7) **Sin cursor/pagination genérico en el service** — los filtros y la paginación son tipos del
> módulo (`outbound.ListParams`, `outbound.Page[T]`); la capa HTTP traduce los query params.

---

## Comparación: ancla vs. msp-api (real)

| Aspecto | ancla-api | msp-api (real) |
|---|---|---|
| Paquete | `app/services/` (subpackage) | `app/` (package `app`) |
| Nombre de handlers | `Handle{Verb}{Entity}` (inglés) | `{Verbo}{Entidad}` (español) |
| Valor de retorno de comandos | `*app.{Entity}Response` (DTO) | `*domain.{Entity}` (agregado) |
| Respuesta JSON | en el service (mapper) | en la capa HTTP |
| Audit logger | campo `auditLogger` en Service | no existe; outbox es el trail |
| Dependencias opcionales | todas en el constructor | constructor obligatorio + `With*` setters |
| Tx manager | `transaction.Manager` (interfaz) | `*firebird.TxManager` (concreto) |
| Outbox | fuera del scope del service | `outbox outbound.OutboxEnqueuer` + `drainEvents` |
| Input DTO de comando | struct en el mismo archivo del handler | struct en el mismo archivo del handler ✓ |
| Queries | `*app.{Entity}Response` | `*domain.{Entity}` / `outbound.Page[T]` |
| Filtros de lista | `outbound.{Entity}Filter` | `outbound.ListVentasInput` con `Pagination` + `Filters` |

---

## Archivos a crear

```
internal/{module}/app/
├─ service.go                    # struct Service + constructor + helpers privados
├─ {verbo}_{entidad}.go          # un archivo por comando (crear_venta.go, cancelar_venta.go)
└─ {verbo}_{entidad}.go          # un archivo por query   (obtener_venta.go, listar_ventas.go)
```

Package siempre `app`. No crear subpaquetes `commands/` ni `queries/`.

> **Referencia real:** `internal/ventas/app/` — 20+ archivos de comando/query, todos en package `app`.

---

## Service — siempre existe

El struct `Service` es el único punto de entrada de la capa de aplicación. Todas las dependencias
(repos, ports outbound, infraestructura transversal) pasan por él. Handlers de comandos y queries
son **métodos sobre `*Service`**.

### `app/service.go`

```go
// Package app contains the {module} module's command and query services.
//
//nolint:misspell // vocabulario en español por convención del proyecto.
package app

import (
    "context"
    "log/slog"

    "github.com/google/uuid"

    "github.com/abdimuy/msp-api/internal/platform/firebird"
    "github.com/abdimuy/msp-api/internal/platform/apperror"
    "github.com/abdimuy/msp-api/internal/{module}/domain"
    "github.com/abdimuy/msp-api/internal/{module}/ports/outbound"
)

const outboxAggregate{Entity} = "{entity}" // constante del agregado para el outbox

type Service struct {
    {entities}  outbound.{Entity}Repo
    outbox      outbound.OutboxEnqueuer
    clock       outbound.Clock
    txMgr       *firebird.TxManager
    // dependencias opcionales: se añaden con With* setters
    {optional}  outbound.{OptionalService} // nil-safe en cada método que lo use
}

// With{Optional} adjunta {optional} al servicio para fluent wiring en el composition root.
func (s *Service) With{Optional}(dep outbound.{OptionalService}) *Service {
    s.{optional} = dep
    return s
}

func NewService(
    {entities} outbound.{Entity}Repo,
    outbox     outbound.OutboxEnqueuer,
    clock      outbound.Clock,
    txMgr      *firebird.TxManager,
) *Service {
    return &Service{
        {entities}: {entities},
        outbox:     outbox,
        clock:      clock,
        txMgr:      txMgr,
    }
}

// runInTx delega al TxManager cuando está configurado.
// En tests con fakes en memoria, txMgr puede ser nil — fn se invoca directamente.
func (s *Service) runInTx(ctx context.Context, fn func(context.Context) error) error {
    if s.txMgr == nil {
        return fn(ctx)
    }
    return s.txMgr.RunInTx(ctx, fn)
}

// enqueueEvent encola un evento en el outbox, best-effort.
// Los errores se loguean pero nunca bloquean la escritura de negocio.
func (s *Service) enqueueEvent(ctx context.Context, aggregate string, aggregateID uuid.UUID, eventType string, payload any) {
    if s.outbox == nil {
        return
    }
    if err := s.outbox.Enqueue(ctx, aggregate, aggregateID, eventType, payload); err != nil {
        slog.WarnContext(ctx, "{module}.outbox_enqueue_failed",
            "aggregate", aggregate,
            "aggregate_id", aggregateID,
            "event_type", eventType,
            "error", err,
        )
    }
}

// drainEvents reenvía los eventos pendientes de la entidad al outbox y limpia el buffer.
func (s *Service) drainEvents(ctx context.Context, e *domain.{Entity}) {
    for _, ev := range e.PendingEvents() {
        s.enqueueEvent(ctx, outboxAggregate{Entity}, ev.AggregateID(), ev.EventType(), ev.Payload())
    }
    e.ClearPendingEvents()
}
```

**Reglas del Service:**

- Una sola struct `Service` por módulo; si hay múltiples agregados con mucha lógica separada, crear
  `{Entidad}Service` por agregado (como `ancla`) o expandir los campos de la struct única (como `ventas`).
- Solo struct + constructor + helpers privados (`runInTx`, `enqueueEvent`, `drainEvents`, validadores
  auxiliares). Sin lógica de negocio directamente en este archivo.
- `txMgr` puede ser nil en tests — `runInTx` lo maneja.
- Dependencias opcionales con `With*` setters: se añaden después del `NewService` en el composition root.
  Cada uso de la dependencia opcional hace `if s.{optional} == nil { return nil }` antes de invocarla.

---

## Comandos

### Forma general — `app/{verbo}_{entidad}.go`

Cada archivo de comando contiene:

1. **Input DTO** (struct de campos primitivos).
2. **Método handler** en `*Service` con el verbo de negocio en español.
3. Opcionalmente: **`intoDomain()`** como método del Input DTO que construye los VOs.

### Tipo A — Crear

```go
// app/crear_{entidad}.go
//nolint:misspell
package app

import (
    "context"
    "time"

    "github.com/google/uuid"
    "github.com/shopspring/decimal"

    "github.com/abdimuy/msp-api/internal/{module}/domain"
)

// Crear{Entidad}Input es el DTO de entrada del comando. Todos los campos son
// tipos primitivos — los VOs se construyen en intoDomain para mantener el
// handler desacoplado del dominio.
type Crear{Entidad}Input struct {
    Campo1     string
    Campo2     decimal.Decimal
    CampoOpt   *string
    // ... más campos primitivos
}

// Crear{Entidad} valida el input, construye el agregado, lo persiste dentro de
// una transacción Firebird y encola los eventos pendientes en el outbox.
// Devuelve el agregado persistido.
func (s *Service) Crear{Entidad}(ctx context.Context, in Crear{Entidad}Input, by uuid.UUID) (*domain.{Entidad}, error) {
    now := s.clock.Now()

    // 1. Validaciones que requieren I/O (antes de construir el agregado).
    if err := s.validateAlgo(ctx, in.CampoOpt); err != nil {
        return nil, err
    }

    // 2. Construir el agregado (puro, sin efectos secundarios).
    params, err := in.intoDomain(by, now)
    if err != nil {
        return nil, err
    }
    entidad, err := domain.Crear{Entidad}(params)
    if err != nil {
        return nil, err
    }

    // 3. Persistir dentro de una transacción.
    if err := s.runInTx(ctx, func(ctx context.Context) error {
        return s.{entities}.Save(ctx, entidad)
    }); err != nil {
        return nil, err
    }

    // 4. Drenar eventos al outbox (best-effort, fuera de la transacción).
    s.drainEvents(ctx, entidad)
    return entidad, nil
}

// intoDomain construye domain.Crear{Entidad}Params desde los campos primitivos,
// construyendo cada VO a lo largo del camino. Devuelve el primer error encontrado.
func (in Crear{Entidad}Input) intoDomain(by uuid.UUID, now time.Time) (domain.Crear{Entidad}Params, error) {
    vo1, err := domain.Parse{VO1}(in.Campo1)
    if err != nil {
        return domain.Crear{Entidad}Params{}, err
    }
    // ... más VOs
    return domain.Crear{Entidad}Params{
        Campo1:    vo1,
        CreatedBy: by,
        Now:       now,
    }, nil
}
```

**Ejemplo real:** `internal/ventas/app/crear_venta.go` — `CrearVentaInput` con `intoDomain`, método
`(s *Service) CrearVenta(ctx, in, by)` que valida clienteID + vendedores, construye el agregado,
valida stock con inventario opcional, persiste en tx, drena eventos.

### Tipo A — Actualizar

```go
// app/actualizar_{entidad}.go
package app

// Actualizar{Entidad}Input lleva solo los campos mutables del header.
// Los campos derivados (montos) se excluyen — el dominio los recalcula.
type Actualizar{Entidad}Input struct {
    {Entidad}ID  uuid.UUID
    Campo1       string
    CampoOpt     *string
    // ...
}

// Actualizar{Entidad} carga el agregado, aplica la mutación de dominio y
// persiste la actualización.
func (s *Service) Actualizar{Entidad}(ctx context.Context, in Actualizar{Entidad}Input, by uuid.UUID) (*domain.{Entidad}, error) {
    entidad, err := s.{entities}.FindByID(ctx, in.{Entidad}ID)
    if err != nil {
        return nil, err
    }
    now := s.clock.Now()
    params, err := in.intoDomain(by, now)
    if err != nil {
        return nil, err
    }
    if err := entidad.Actualizar{Parte}(params); err != nil {
        return nil, err
    }
    if err := s.runInTx(ctx, func(ctx context.Context) error {
        return s.{entities}.Update(ctx, entidad)
    }); err != nil {
        return nil, err
    }
    s.drainEvents(ctx, entidad)
    return entidad, nil
}
```

**Ejemplo real:** `internal/ventas/app/actualizar_header.go` — `ActualizarHeaderInput` con `intoDomain`,
carga la venta, llama `venta.ActualizarHeader(params)`, persiste en tx.

### Tipo A — Cancelar / Verbo de ciclo de vida

```go
// app/cancelar_{entidad}.go
package app

// Cancelar{Entidad} cancela el agregado. Verifica invariantes de dominio antes
// de persistir; la razón de cancelación es registrada en el agregado.
func (s *Service) Cancelar{Entidad}(ctx context.Context, id uuid.UUID, reason string, by uuid.UUID) (*domain.{Entidad}, error) {
    entidad, err := s.{entities}.FindByID(ctx, id)
    if err != nil {
        return nil, err
    }
    if err := entidad.Cancelar(reason, by, s.clock.Now()); err != nil {
        return nil, err
    }
    if err := s.runInTx(ctx, func(ctx context.Context) error {
        return s.{entities}.Update(ctx, entidad)
    }); err != nil {
        return nil, err
    }
    s.drainEvents(ctx, entidad)
    return entidad, nil
}
```

**Ejemplo real:** `internal/ventas/app/cancelar_venta.go` — también invoca `s.inventario.CrearTraspasoReverso`
dentro de la misma tx cuando el inventario está cableado y la venta no estaba aplicada.

### Patrón de inputs para comandos complejos

Cuando el comando tiene sub-colecciones (productos, combos, vendedores), cada sub-colección tiene su
propio Input struct:

```go
type Crear{Entidad}Sub1Input struct { /* campos primitivos */ }
type Crear{Entidad}Sub2Input struct { /* campos primitivos */ }

type Crear{Entidad}Input struct {
    // campos del header
    Sub1s []Crear{Entidad}Sub1Input
    Sub2s []Crear{Entidad}Sub2Input
}
```

Los helpers de traducción son funciones privadas del archivo (`build{Sub1}Inputs`, `build{Sub2}Inputs`).
No agregar helpers de traducción en `service.go`.

---

## Queries

Las queries son **pass-throughs delgados**. No traducen a response DTOs — devuelven el tipo de dominio
o el tipo de read-model del módulo. La capa HTTP hace la proyección.

### Get por ID

```go
// app/obtener_{entidad}.go
package app

// Obtener{Entidad} carga la entidad por ID. Devuelve domain.Err{Entidad}NotFound si no existe.
func (s *Service) Obtener{Entidad}(ctx context.Context, id uuid.UUID) (*domain.{Entidad}, error) {
    return s.{entities}.FindByID(ctx, id)
}
```

**Ejemplo real:** `internal/ventas/app/obtener_venta.go` — tres líneas incluyendo el comentario.

### Listar

```go
// app/listar_{entidades}.go
package app

// Listar{Entidades}Input agrupa paginación y filtros para el query de listado.
type Listar{Entidades}Input struct {
    Pagination outbound.ListParams
    Filters    outbound.List{Entidades}Filters
}

// Listar{Entidades} devuelve una página paginada por cursor de entidades que
// coinciden con los filtros. Pass-through al repositorio.
func (s *Service) Listar{Entidades}(ctx context.Context, in Listar{Entidades}Input) (outbound.Page[*domain.{Entidad}], error) {
    return s.{entities}.List(ctx, in.Pagination, in.Filters)
}
```

**Ejemplo real:** `internal/ventas/app/listar_ventas.go`.

### Queries con lógica de ensamble

Cuando una query necesita resolver nombres o combinar datos de múltiples puertos (ej. resolver
`usuario_id → nombre`), toda la lógica de ensamble vive **en el archivo del query**, no en el Service.

```go
// app/eventos_de_{entidad}.go
package app

// Eventos{Entidad}Input agrega los parámetros del query de timeline.
type Eventos{Entidad}Input struct {
    {Entidad}ID uuid.UUID
    // filtros de fecha, paginación, etc.
}

// Eventos{Entidad} construye la timeline de eventos de una entidad.
func (s *Service) Eventos{Entidad}(ctx context.Context, in Eventos{Entidad}Input) (some read-model type, error) {
    // ... ensambla eventos, resuelve nombres, etc.
}
```

**Ejemplo real:** `internal/ventas/app/eventos_de_venta.go`.

---

## Relación con la capa HTTP

Los comandos y queries del Service devuelven **tipos de dominio o read-models del módulo**, no structs
JSON. La proyección a JSON ocurre en los handlers HTTP:

```
handler HTTP → {Verbo}Input → s.{Verbo}{Entidad}(ctx, input, by) → *domain.{Entidad}
                                                                           ↓
                                                              mapper HTTP → response JSON
```

El mapper para el frontend (`internal/{module}/infra/http/`) transforma `*domain.{Entidad}` al DTO
de respuesta con tags JSON, timestamps en RFC3339 UTC y UUIDs como string.

El mapper para otros módulos (`{module}_contracts_mapper.go`) transforma `*domain.{Entidad}` al tipo
del contrato (campos Go tipados, sin tags JSON).

| Archivo | Convierte | Consumidor |
|---|---|---|
| `infra/http/{entity}_handler.go` | `*domain.{Entidad}` → response JSON | clientes HTTP |
| `{module}_contracts_mapper.go` | `*domain.{Entidad}` → `{module}.{Entidad}Contract` | otros módulos |

---

## Flujo estándar de un comando

```
1. Validaciones I/O (clienteID existe, vendedores existen, etc.)  — antes de construir el agregado
2. Input DTO → domain params via intoDomain()                      — construye VOs, primer error retorna
3. domain.Crear{Entidad}(params)                                   — valida invariantes de dominio
4. Validaciones post-construcción (stock, unicidad, etc.)          — si la validación requiere el agregado completo
5. runInTx { repo.Save / repo.Update + efectos secundarios en tx } — persiste; errores hacen rollback
6. drainEvents(ctx, entidad)                                       — best-effort, fuera de la tx
7. return entidad, nil
```

Para comandos de **lectura-mutación-escritura** (actualizar, cancelar):

```
1. repo.FindByID(ctx, id)         — carga el agregado
2. intoDomain() si el input es complejo
3. entidad.{Verbo}(params)        — muta el agregado en memoria, valida invariantes
4. runInTx { repo.Update }        — persiste
5. drainEvents(ctx, entidad)
6. return entidad, nil
```

---

## Límites de transacción

- Toda escritura multi-tabla va dentro de `runInTx`.
- El `OutboxEnqueuer` del módulo escribe a `MSP_OUTBOX_EVENTS` **dentro de la misma transacción Firebird**
  que la escritura de negocio (re-entrant via ctx) según ADR-0008. Si el INSERT al outbox falla,
  el rollback cubre también la escritura de negocio.
- `drainEvents` se llama **después de que la transacción ha commiteado**. Es best-effort: un fallo de
  outbox solo genera un warning en el log, nunca regresa un error al caller.

---

## Idempotencia

Para endpoints `POST` con efectos secundarios significativos, la idempotencia se aplica en el middleware
chi (ver `cmd/api/server.go`). El middleware hash-cachea `body + Idempotency-Key` y reproduce la
respuesta cacheada en reintentos. El comando no necesita lógica de idempotencia propia.

---

## Cuándo NO dividir por archivo

El patrón de un-archivo-por-comando se usa cuando:

- El módulo tiene más de ~6 comandos/queries.
- Algún comando construye un agregado con sub-colecciones.
- Las rutas de lectura y escritura tienen lógica suficientemente distinta.

Para módulos simples (como `auth`), todos los métodos del Service pueden vivir organizados por entidad
en archivos como `usuarios.go`, `roles.go`. Ver `CQRS_PATTERN.md` para los criterios de corte.

---

## Convención del `const source`

`ancla-api` declara `const source = "{module}.Handle{Verb}{Entity}"` en cada handler para rastrear
el origen de los errores. msp-api enriquece los errores de la misma forma usando `.WithSource(source)`,
pero la convención de nombre es:

```go
const source = "{module}.{Verbo}{Entidad}"
// Ejemplo:
const source = "ventas.CrearVenta"
const source = "ventas.CancelarVenta"
const source = "ventas.ObtenerVenta"
```

Fórmula: `"{módulo}.{NombreExactoDelMétodo}"` — coincide con el nombre del método Go.

---

## Agent checklist

- [ ] Un archivo `service.go` con `Service` struct + `NewService` + helpers privados (`runInTx`, `enqueueEvent`, `drainEvents`). Sin lógica de negocio en este archivo.
- [ ] Un archivo por comando (`{verbo}_{entidad}.go`); un archivo por query (`{verbo}_{entidad}.go`). Todos en package `app`.
- [ ] Los handlers son métodos sobre `*Service`: `func (s *Service) {Verbo}{Entidad}(ctx, ...) (*domain.{Entidad}, error)`.
- [ ] El Input DTO de comandos contiene solo tipos primitivos. Los VOs se construyen en `intoDomain()`.
- [ ] Sub-colecciones tienen su propio Input struct; helpers de traducción son funciones privadas del archivo del comando.
- [ ] Comandos siguen el flujo: validaciones I/O → `intoDomain` → `domain.Crear{Entidad}` → validaciones post-construcción → `runInTx` → `drainEvents` → `return entidad, nil`.
- [ ] Lecturas-mutaciones-escrituras: `FindByID` → `intoDomain` (si aplica) → mutación de dominio → `runInTx { Update }` → `drainEvents`.
- [ ] `drainEvents` se llama fuera de la transacción (best-effort).
- [ ] Errores de dominio se propagan directamente (ya son `apperror`). Errores de infra se envuelven con `apperror.NewInternal(code, mensaje_español).WithSource(source).WithError(err)`.
- [ ] `const source = "{módulo}.{NombreMétodo}"` en cada handler que enriquezca errores.
- [ ] Dependencias opcionales (`inventario`, `eventReader`, etc.) se añaden con `With*` setters; cada uso hace `if s.dep == nil { return nil }` antes de invocar.
- [ ] `txMgr` puede ser nil en tests — no hacer `if s.txMgr == nil { panic(...) }`.
- [ ] Queries son pass-throughs: devuelven `*domain.{Entidad}` o `outbound.Page[T]`, sin proyección a JSON.
- [ ] La proyección a JSON ocurre en `infra/http/`, no en `app/`.
- [ ] Módulo path: `github.com/abdimuy/msp-api/internal/{module}/...`.
- [ ] El archivo lleva `//nolint:misspell` si el vocabulario incluye español.
