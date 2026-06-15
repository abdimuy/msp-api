# Step 06 — Service Recipes

> Applies to: Tipo A (CRUD), Tipo B (Pipeline).
> Depends on: Steps 01–05.
> Parallel with: —
> Scope: la capa `app/` del módulo — el `Service` struct, los archivos de comando y query,
> y la mecánica de transacciones, eventos de dominio y outbox.

> **Adaptado de `ancla-api`, reconciliado con el código real de msp-api.**

---

## ancla vs. msp-api (diferencias clave)

| Aspecto | ancla-api | msp-api (real) |
|---|---|---|
| `TxManager` | `WithTransaction` (interfaz inyectada) | `*firebird.TxManager` concreta; el Service la envuelve en `runInTx` que acepta nil en tests |
| Isolation por defecto | depende del backend | `READ COMMITTED`; `RunInTxNoWait` para hot paths (traspasos) |
| Outbox | best-effort, fuera de la tx | **atómico por ADR-0008**: el INSERT en `MSP_OUTBOX_EVENTS` se une a la tx del negocio vía re-entrada; `drainEvents` se llama **después** del commit |
| Optimistic locking | `IncrementVersion` + `WithVersion` | **no existe**; msp-api usa bloqueo pesimista selectivo (`LockByID` en `AplicarVenta`) |
| Audit logger | `s.logAudit` asíncrono/síncrono | **no existe en msp-api** — los eventos de dominio encolados en el outbox son el registro de auditoría |
| Error enrichment | `appErr.WithSource(source)` en cada rama | igual, más `.WithError(err)` para errores de infra |
| Mensajes de usuario | inglés | **español, minúscula, sin punto final** (regla 3) |
| Módulo path | `github.com/ancla-dev/ancla-api` | `github.com/abdimuy/msp-api` |
| `source` const | `"{module}.Handle{Verb}{Entity}"` | igual — sigue la misma convención |

---

## Estructura de archivos

```
internal/{module}/app/
    service.go                  ← Service struct + NewService + helpers privados
    crear_{entidad}.go          ← comando Create
    obtener_{entidad}.go        ← query Get by ID
    listar_{entidades}.go       ← query List con filtros
    actualizar_{entidad}.go     ← comando Update / mutación nombrada
    cancelar_{entidad}.go       ← comando Cancel (si aplica)
    ...{verbo}_{entidad}.go     ← un archivo por comando/query significativo
```

Package siempre `app`. Importaciones permitidas: `internal/{module}/domain`,
`internal/{module}/ports/outbound`, `internal/platform/{apperror,firebird,audit,domain}`,
`github.com/google/uuid`, `github.com/shopspring/decimal`, stdlib.
No importar otros módulos directamente — usar los outbound ports declarados en `ports/outbound/`.

---

## El Service struct

```go
// Package app contains the {module} module's command and query services.
//
//nolint:misspell // vocabulario del módulo es español per project convention.
package app

import (
    "context"
    "log/slog"

    "github.com/google/uuid"

    "github.com/abdimuy/msp-api/internal/platform/firebird"
    "github.com/abdimuy/msp-api/internal/{module}/domain"
    "github.com/abdimuy/msp-api/internal/{module}/ports/outbound"
)

const outboxAggregate{Entity} = "{entity}" // nombre canónico del agregado en el outbox

type Service struct {
    repo    outbound.{Entity}Repo
    storage outbound.StorageProvider // si aplica
    clock   outbound.Clock
    outbox  outbound.OutboxEnqueuer
    txMgr   *firebird.TxManager
    // dependencias opcionales wired con With{Dep}
}

func NewService(
    repo    outbound.{Entity}Repo,
    clock   outbound.Clock,
    outbox  outbound.OutboxEnqueuer,
    txMgr   *firebird.TxManager,
) *Service {
    return &Service{
        repo:  repo,
        clock: clock,
        outbox: outbox,
        txMgr: txMgr,
    }
}
```

### Dependencias opcionales — patrón `With*`

Las dependencias que no todos los comandos requieren se añaden con un método fluente.
Esto mantiene `NewService` estable cuando el módulo crece.

```go
// WithStorage attaches a StorageProvider so AdjuntarImagen can write to disk.
// Returns s for fluent wiring at the composition root.
func (s *Service) WithStorage(sp outbound.StorageProvider) *Service {
    s.storage = sp
    return s
}
```

---

## Helpers privados del Service

Estos tres helpers aparecen en **todo** Service de msp-api. Copiarlos tal cual, ajustando el
package path y el literal de log.

### `runInTx` — delegación al TxManager, nil-safe en tests

```go
// runInTx delegates to the configured TxManager when one is wired, otherwise
// invokes fn directly so in-memory tests can omit a TxManager.
func (s *Service) runInTx(ctx context.Context, fn func(context.Context) error) error {
    if s.txMgr == nil {
        return fn(ctx)
    }
    return s.txMgr.RunInTx(ctx, fn)
}
```

### `enqueueEvent` — encola un evento al outbox, best-effort post-commit

```go
// enqueueEvent best-effort enqueues an outbox event after the business commit.
// Failures are logged but never block the caller.
func (s *Service) enqueueEvent(ctx context.Context, aggregate string, aggregateID uuid.UUID, eventType string, payload any) {
    if s.outbox == nil {
        return
    }
    if err := s.outbox.Enqueue(ctx, aggregate, aggregateID, eventType, payload); err != nil {
        slog.WarnContext(
            ctx, "{module}.outbox_enqueue_failed",
            "aggregate", aggregate,
            "aggregate_id", aggregateID,
            "event_type", eventType,
            "error", err,
        )
    }
}
```

### `drainEvents` — drena el buffer del agregado al outbox

```go
// drainEvents forwards each pending event on e to the outbox and clears the
// aggregate's buffer. Call AFTER the business transaction commits.
func (s *Service) drainEvents(ctx context.Context, e *domain.{Entity}) {
    for _, ev := range e.PendingEvents() {
        s.enqueueEvent(ctx, outboxAggregate{Entity}, ev.AggregateID(), ev.EventType(), ev.Payload())
    }
    e.ClearPendingEvents()
}
```

> **Regla de orden:** `drainEvents` se llama **fuera** de `runInTx`, después del commit exitoso.
> El outbox `Enqueuer` de msp-api es re-entrant (ADR-0008): si el contexto ya lleva una tx,
> el INSERT de `MSP_OUTBOX_EVENTS` se une a ella; si no, abre la suya. La llamada a
> `drainEvents` *fuera* de la tx es correcta porque en ese punto la tx de negocio ya se
> cerró — el Enqueuer abre una tx nueva y corta solo para la fila del outbox.

---

## Flujo canónico de un comando mutante

```
1. Precondiciones externas (validar FKs, checks de unicidad)  ← fuera de la tx si son read-only
2. Construir / hidratar el agregado (pura)
3. Llamar método de dominio (pura, puede fallar)
4. runInTx { repo.Save / repo.Update [+ otras escrituras atómicas] }
5. drainEvents (fuera de la tx, post-commit)
6. Return agregado o respuesta
```

Cualquier fallo en los pasos 1-3 devuelve error antes de abrir una transacción.
El paso 4 rollbackea automáticamente si cualquier operación dentro devuelve error.
El paso 5 es best-effort: no revierte la tx ya commiteada.

---

## Recetas Tipo A

### R1 — Crear (sin verificación de unicidad)

Ejemplo real: `internal/ventas/app/crear_venta.go` (`CrearVenta`).

```go
func (s *Service) Crear{Entity}(ctx context.Context, in Crear{Entity}Input, by uuid.UUID) (*domain.{Entity}, error) {
    const source = "{module}.Crear{Entity}"

    now := s.clock.Now()

    // 1. Precondición externa (opcional, fuera de tx)
    if err := s.validateExternalRef(ctx, in.RefID); err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_ref_check_failed", "error al verificar la referencia").
            WithSource(source).WithError(err)
    }

    // 2. Construir VOs + params (puro)
    params, err := in.intoDomain(by, now)
    if err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_build_failed", "error al construir la entidad").
            WithSource(source).WithError(err)
    }

    // 3. Constructor de dominio (puro)
    entity, err := domain.Crear{Entity}(params)
    if err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_create_failed", "error al crear la entidad").
            WithSource(source).WithError(err)
    }

    // 4. Persistir en una sola tx
    if err := s.runInTx(ctx, func(ctx context.Context) error {
        if err := s.repo.Save(ctx, entity); err != nil {
            return apperror.NewInternal("{entity}_save_failed", "error al guardar la entidad").
                WithSource(source).WithError(err)
        }
        return nil
    }); err != nil {
        return nil, err
    }

    // 5. Drain post-commit
    s.drainEvents(ctx, entity)
    return entity, nil
}
```

**`intoDomain`** es un método en el Input DTO que construye todos los VOs y el params struct del dominio.
Mantiene el handler libre de constructores de VOs y hace que la lógica de mapeo sea testeable:

```go
func (in Crear{Entity}Input) intoDomain(by uuid.UUID, now time.Time) (domain.Crear{Entity}Params, error) {
    tipoVO, err := domain.Parse{VO}(in.Tipo)
    if err != nil {
        return domain.Crear{Entity}Params{}, err
    }
    // ... más VOs ...
    return domain.Crear{Entity}Params{
        // ...
        CreatedBy: by,
        Now:       now,
    }, nil
}
```

---

### R2 — Crear con verificación de unicidad (dentro de la tx)

Cuando la unicidad se verifica contra la base de datos, la comprobación y la escritura van
en la misma transacción para evitar race conditions.

```go
func (s *Service) Crear{Entity}(ctx context.Context, in Crear{Entity}Input, by uuid.UUID) (*domain.{Entity}, error) {
    const source = "{module}.Crear{Entity}"

    now := s.clock.Now()

    params, err := in.intoDomain(by, now)
    if err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_build_failed", "error al construir la entidad").
            WithSource(source).WithError(err)
    }

    entity, err := domain.Crear{Entity}(params)
    if err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_create_failed", "error al crear la entidad").
            WithSource(source).WithError(err)
    }

    if err := s.runInTx(ctx, func(ctx context.Context) error {
        exists, err := s.repo.ExistsByCodigo(ctx, entity.Codigo())
        if err != nil {
            return apperror.NewInternal("{entity}_check_failed", "error al verificar unicidad").
                WithSource(source).WithError(err)
        }
        if exists {
            return domain.Err{Entity}CodigoYaExiste.WithSource(source)
        }
        if err := s.repo.Save(ctx, entity); err != nil {
            return apperror.NewInternal("{entity}_save_failed", "error al guardar la entidad").
                WithSource(source).WithError(err)
        }
        return nil
    }); err != nil {
        return nil, err
    }

    s.drainEvents(ctx, entity)
    return entity, nil
}
```

---

### R3 — Obtener por ID

Ejemplo real: `internal/ventas/app/obtener_venta.go`.

```go
func (s *Service) Obtener{Entity}(ctx context.Context, id uuid.UUID) (*domain.{Entity}, error) {
    entity, err := s.repo.FindByID(ctx, id)
    if err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr // ya tiene source del repo
        }
        return nil, apperror.NewInternal("{entity}_get_failed", "error al obtener la entidad").
            WithSource("{module}.Obtener{Entity}").WithError(err)
    }
    return entity, nil
}
```

> El repo añade `WithSource` al `ErrNotFound` cuando construye el sentinel — el servicio
> propaga el error ya enriquecido sin re-envolverlo.

---

### R4 — Listar con filtros y paginación

Ejemplo real: `internal/ventas/app/listar_ventas.go`.

```go
type Listar{Entities}Input struct {
    Pagination outbound.ListParams
    Filters    outbound.List{Entities}Filters
}

func (s *Service) Listar{Entities}(ctx context.Context, in Listar{Entities}Input) (outbound.Page[*domain.{Entity}], error) {
    page, err := s.repo.List(ctx, in.Pagination, in.Filters)
    if err != nil {
        return outbound.Page[*domain.{Entity}]{}, apperror.NewInternal("{entities}_list_failed", "error al listar las entidades").
            WithSource("{module}.Listar{Entities}").WithError(err)
    }
    return page, nil
}
```

`outbound.Page[T]` encapsula el slice de items + `NextCursor *string` + `Total int`. El repo
construye el cursor; el servicio lo pasa sin transformar.

---

### R5 — Mutar campos (Update / verbo de dominio)

Ejemplo real: `internal/ventas/app/aprobar_venta.go` (`Aprobar`, `EnviarARevision`).
Flujo: hidratar → mutar en dominio → persistir en tx → drain.

```go
func (s *Service) {Verbo}{Entity}(ctx context.Context, id, by uuid.UUID) (*domain.{Entity}, error) {
    const source = "{module}.{Verbo}{Entity}"

    // 1. Hidratar (fuera de tx — lectura)
    entity, err := s.repo.FindByID(ctx, id)
    if err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_get_failed", "error al obtener la entidad").
            WithSource(source).WithError(err)
    }

    // 2. Mutar (puro — puede devolver sentinel de dominio)
    if err := entity.{Verbo}(by, s.clock.Now()); err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_mutation_failed", "error inesperado al mutar la entidad").
            WithSource(source).WithError(err)
    }

    // 3. Persistir en tx
    if err := s.runInTx(ctx, func(ctx context.Context) error {
        if err := s.repo.Update(ctx, entity); err != nil {
            return apperror.NewInternal("{entity}_update_failed", "error al actualizar la entidad").
                WithSource(source).WithError(err)
        }
        return nil
    }); err != nil {
        return nil, err
    }

    // 4. Drain post-commit
    s.drainEvents(ctx, entity)
    return entity, nil
}
```

---

### R6 — Cancelar / soft-delete

Ejemplo real: `internal/ventas/app/cancelar_venta.go`.
Puede agrupar múltiples escrituras en la tx (p. ej. reverso de traspaso atómico con la cancelación).

```go
func (s *Service) Cancelar{Entity}(ctx context.Context, id uuid.UUID, reason string, by uuid.UUID) (*domain.{Entity}, error) {
    const source = "{module}.Cancelar{Entity}"

    entity, err := s.repo.FindByID(ctx, id)
    if err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_get_failed", "error al obtener la entidad").
            WithSource(source).WithError(err)
    }

    if err := entity.Cancelar(reason, by, s.clock.Now()); err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_cancel_failed", "error inesperado al cancelar").
            WithSource(source).WithError(err)
    }

    if err := s.runInTx(ctx, func(ctx context.Context) error {
        if err := s.repo.Update(ctx, entity); err != nil {
            return apperror.NewInternal("{entity}_update_failed", "error al actualizar la entidad").
                WithSource(source).WithError(err)
        }
        // Otras escrituras atómicas opcionales aquí (p. ej. reverso de reservas).
        return nil
    }); err != nil {
        return nil, err
    }

    s.drainEvents(ctx, entity)
    return entity, nil
}
```

---

### R7 — Comando con idempotencia explícita

Ejemplo real: `internal/ventas/app/aplicar_venta.go` (`AplicarVenta`).
Útil cuando la operación tiene consecuencias externas (Microsip, pagos, etc.) que no se deben repetir.

```go
func (s *Service) {Verbo}{Entity}(ctx context.Context, id, by uuid.UUID) (*domain.{Entity}, error) {
    const source = "{module}.{Verbo}{Entity}"

    var entity *domain.{Entity}

    if err := s.runInTx(ctx, func(ctx context.Context) error {
        // Bloqueo pesimista para serializar concurrencia (evita double-submit).
        if err := s.repo.LockByID(ctx, id); err != nil {
            return err
        }

        e, err := s.repo.FindByID(ctx, id)
        if err != nil {
            appErr, ok := apperror.As(err)
            if ok {
                return appErr.WithSource(source)
            }
            return apperror.NewInternal("{entity}_get_failed", "error al obtener la entidad").
                WithSource(source).WithError(err)
        }

        // Fast-path idempotente: ya en estado final → devolver sin re-ejecutar.
        if e.{EsIdempotente}() {
            entity = e
            return nil
        }

        if err := e.{Verbo}(by, s.clock.Now()); err != nil {
            appErr, ok := apperror.As(err)
            if ok {
                return appErr.WithSource(source)
            }
            return apperror.NewInternal("{entity}_mutation_failed", "error inesperado").
                WithSource(source).WithError(err)
        }

        if err := s.repo.Update(ctx, e); err != nil {
            return apperror.NewInternal("{entity}_update_failed", "error al actualizar la entidad").
                WithSource(source).WithError(err)
        }
        entity = e
        return nil
    }); err != nil {
        return nil, err
    }

    s.drainEvents(ctx, entity)
    return entity, nil
}
```

> **`LockByID`** implementa `SELECT ... WITH LOCK` en Firebird. Se usa únicamente cuando hay
> riesgo real de double-submit concurrente — no en el caso general. El middleware de
> idempotency key es complementario, no sustituto.

---

## Recetas Tipo B (Pipeline)

### R8 — Iniciar pipeline

```go
func (s *Service) Iniciar{Pipeline}(ctx context.Context, cmd Iniciar{Pipeline}Input, by uuid.UUID) (*domain.{Entity}, error) {
    const source = "{module}.Iniciar{Pipeline}"

    now := s.clock.Now()
    entity, err := domain.Crear{Entity}(domain.Crear{Entity}Params{
        // ...
        CreatedBy: by,
        Now:       now,
    })
    if err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_create_failed", "error al iniciar el pipeline").
            WithSource(source).WithError(err)
    }

    if err := s.runInTx(ctx, func(ctx context.Context) error {
        return s.repo.Save(ctx, entity)
    }); err != nil {
        return nil, err
    }

    s.drainEvents(ctx, entity)
    return entity, nil
}
```

### R9 — Avanzar etapa del pipeline

```go
func (s *Service) Completar{Etapa}(ctx context.Context, id uuid.UUID, cmd Completar{Etapa}Input, by uuid.UUID) (*domain.{Entity}, error) {
    const source = "{module}.Completar{Etapa}"

    entity, err := s.repo.FindByID(ctx, id)
    if err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_get_failed", "error al obtener la entidad").
            WithSource(source).WithError(err)
    }

    if err := entity.Completar{Etapa}(/* datos de salida */, by, s.clock.Now()); err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_transition_failed", "error al avanzar la etapa").
            WithSource(source).WithError(err)
    }

    if err := s.runInTx(ctx, func(ctx context.Context) error {
        return s.repo.Update(ctx, entity)
    }); err != nil {
        return nil, err
    }

    s.drainEvents(ctx, entity)
    return entity, nil
}
```

### R10 — Fallar pipeline

```go
func (s *Service) Fallar{Pipeline}(ctx context.Context, id uuid.UUID, reason string) (*domain.{Entity}, error) {
    const source = "{module}.Fallar{Pipeline}"

    entity, err := s.repo.FindByID(ctx, id)
    if err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_get_failed", "error al obtener la entidad").
            WithSource(source).WithError(err)
    }

    if err := entity.Fallar(reason); err != nil {
        appErr, ok := apperror.As(err)
        if ok {
            return nil, appErr.WithSource(source)
        }
        return nil, apperror.NewInternal("{entity}_fail_failed", "error inesperado al fallar el pipeline").
            WithSource(source).WithError(err)
    }

    if err := s.runInTx(ctx, func(ctx context.Context) error {
        return s.repo.Update(ctx, entity)
    }); err != nil {
        return nil, err
    }

    s.drainEvents(ctx, entity)
    return entity, nil
}
```

---

## Convenciones transversales

### Manejo de errores

```go
// Error de dominio (sentinel apperror) → enriquecer con source, pasar.
appErr, ok := apperror.As(err)
if ok {
    return nil, appErr.WithSource(source)
}
// Error de infra → envolver en NewInternal con código inglés, mensaje español.
return nil, apperror.NewInternal("{entity}_{verb}_failed", "error al {verbo} la entidad").
    WithSource(source).WithError(err)
```

`apperror.As` es preferible a `errors.As` porque extrae el `*apperror.Error` tipado directamente.
`.WithSource` y `.WithError` devuelven copias — el sentinel no se muta.

### Fechas

Todo timestamp se genera con `s.clock.Now()` (no `time.Now()` directa — permite test determinista).
Al pasar un `time.Time` a `repo.Save` / `repo.Update`, el repo lo envuelve con
`firebird.ToWallClock(t)`. Al leer, usa `firebird.ScanUTCTime`. Ver `DATETIME_HANDLING.md`.

### No hay `audit.ComputeDelta` ni `logAudit`

msp-api no tiene un audit logger centralizado. Los **eventos de dominio** (`VentaCreadaEvent`,
`VentaCanceladaEvent`, etc.) son el registro de auditoría: se persisten en `MSP_OUTBOX_EVENTS`
y el dispatcher los entrega a los consumidores. No reimplementar `logAudit`.

### No hay optimistic locking

msp-api no tiene campo `version` en ninguna entidad. No añadir. Si se necesita serializar
concurrencia, usar `LockByID` (SELECT ... WITH LOCK) selectivamente — solo en comandos con
riesgo real de double-submit (ver `AplicarVenta`).

### Clock inyectado

`s.clock.Now()` en lugar de `time.Now()`. El puerto `outbound.Clock` tiene una implementación
real (`RealClock`) y una fake para tests. Wiring en `NewService`.

---

## Guía de selección de receta

| Necesidad | Receta |
|---|---|
| Crear sin verificar unicidad en DB | R1 |
| Crear con verificación de unicidad en DB | R2 |
| Obtener por ID | R3 |
| Listar con filtros y paginación | R4 |
| Mutar campos / verbo de dominio nombrado | R5 |
| Cancelar / soft-delete | R6 |
| Comando con idempotencia + concurrencia | R7 |
| Iniciar pipeline (Tipo B) | R8 |
| Avanzar etapa del pipeline | R9 |
| Fallar pipeline | R10 |

---

## Módulos de referencia (código real)

- `internal/ventas/app/crear_venta.go` — R1 completo con validaciones externas, `intoDomain`, tx multi-escritura y drain.
- `internal/ventas/app/cancelar_venta.go` — R6 con escritura secundaria atómica (reverso de traspaso).
- `internal/ventas/app/aprobar_venta.go` — R5 repetido tres veces para transiciones de pipeline simples.
- `internal/ventas/app/aplicar_venta.go` — R7 con `LockByID`, idempotencia y escritura en sistema externo (Microsip).
- `internal/ventas/app/obtener_venta.go` — R3 mínimo.
- `internal/ventas/app/listar_ventas.go` — R4 mínimo.
- `internal/ventas/app/service.go` — Service struct + `NewService` + `runInTx` + `enqueueEvent` + `drainEvents`.
- `internal/ventas/infra/ventoutbox/enqueuer.go` — implementación del `OutboxEnqueuer` (re-entrant, ADR-0008).

---

## Agent checklist

- [ ] Package `app`; `//nolint:misspell` en el package doc si el módulo usa vocabulario español.
- [ ] `Service` struct con campos privados; `NewService` recibe solo dependencias requeridas.
- [ ] Dependencias opcionales con métodos `With{Dep}(*Service) *Service`.
- [ ] `runInTx` helper copiado verbatim (nil-safe para tests sin TxManager).
- [ ] `enqueueEvent` + `drainEvents` helpers copiados; `drainEvents` llamado **fuera** de `runInTx`.
- [ ] `const source = "{module}.{Verbo}{Entity}"` al inicio de cada método exportado.
- [ ] Errores de dominio: `apperror.As` → `.WithSource(source)`.
- [ ] Errores de infra: `apperror.NewInternal(código_inglés, "mensaje español")`.`WithSource(source).WithError(err)`.
- [ ] Timestamps generados con `s.clock.Now()`, nunca `time.Now()` directa.
- [ ] Sin `audit.ComputeDelta` / `logAudit` — los eventos de dominio son el audit trail.
- [ ] Sin `version` / optimistic locking; `LockByID` solo cuando hay riesgo real de double-submit.
- [ ] Comando con múltiples escrituras → todas dentro de la misma `runInTx`.
- [ ] `intoDomain` en el DTO de input para mantener el archivo de comando libre de constructores de VO.
- [ ] Un archivo por comando/query significativo; `service.go` solo para struct + helpers.
