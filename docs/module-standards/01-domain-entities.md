# Step 01 — Domain Entities

> Applies to: Type A (CRUD), Type B (Pipeline), Type C (Microsip-synced).
> Depends on: —
> Parallel with: Step 02 (value objects + errors).
> Scope: the aggregate root entity **plus** its composition — child entities, `iter.Seq`
> accessors, and domain events (todo lo de la capa de entidades vive aquí). El *drain* de eventos
> post-commit se detalla en `06-service-recipes.md`.

> **Adaptado de la guía por capas de `ancla-api`, reconciliado con el código real de msp-api.**
> Diferencias clave vs. ancla: (1) **NO usamos optimistic locking** (`version`/`IncrementVersion`) —
> ningún entity de msp-api lo usa; (2) el paquete de audit es `internal/platform/audit`
> (`audit.Auditable`), no `platform.*`; (3) agregamos **Tipo C (Microsip sync)** con
> `audit.MicrosipSync`, que ancla no tiene.

---

## Files to create

```
internal/{module}/domain/{entity}.go
internal/{module}/domain/{entity}_rules.go   # solo si la validación es compleja
```

Package name is always `domain`. Imports allowed: stdlib + `github.com/google/uuid` +
`github.com/shopspring/decimal` + `internal/platform/{audit,domain,apperror}`. Nothing else
(depguard enforces this).

---

## Entity types (msp-api)

| Type | Cuándo | Embebe |
|------|--------|--------|
| **A** CRUD | entidad msp-only (rutas, comisiones, ventas locales) | `audit.Auditable` |
| **B** Pipeline | máquina de estados (traspaso cotización→entrega) | `audit.Timestamped` + transitorios `previousState`/`stateChanged` |
| **C-bidi** | entidad espejo de Microsip, ambos sentidos (clientes) | `audit.Auditable` + `audit.MicrosipSync` |
| **C-push** | creada localmente, solo empujada a Microsip (pagos) | `audit.Auditable` + `audit.MicrosipSync` |

---

## Entity struct

Todos los campos privados. Sin excepciones. **Sin campo `version`.**

```go
type {Entity} struct {
    id      uuid.UUID
    // ... campos de dominio (privados)
    status  {Entity}Status        // si aplica máquina de estados / lifecycle
    audit   audit.Auditable       // Tipo A y C
    // o, para Tipo B:
    timestamps audit.Timestamped
    // para Tipo C, además:
    sync    audit.MicrosipSync
}
```

---

## Constructors

Dos constructores por entidad. Siempre.

### `Crear{Entity}` / `New{Entity}` — crea nueva, valida todas las invariantes

- Para entidades con muchos campos, usar un struct de params: `Crear{Entity}(p Crear{Entity}Params) (*{Entity}, error)` (patrón `ventas`).
- Para entidades simples, argumentos posicionales: `New{Entity}(...) (*{Entity}, error)` (patrón `auth`).

```go
func Crear{Entity}(p Crear{Entity}Params) (*{Entity}, error) {
    // 1. Validar campos requeridos (TrimSpace, no vacío)
    // 2. Validar VOs (vo.IsValid())
    // 3. Validar invariantes de negocio (réplica de cada CHECK de la migración)
    now := time.Now()
    return &{Entity}{
        id:     uuid.New(),
        // ... campos validados
        status: {Entity}StatusInicial,
        audit:  audit.NewAuditable(now, p.CreatedBy), // Tipo A/C
    }, nil
}
```

Reglas:
- Genera `uuid.New()` internamente.
- Fija el estado/status inicial.
- Devuelve `(*{Entity}, error)`.
- Valida **cada** campo con restricción (mismo conjunto que los CHECK de la migración).
- Fija audit/timestamps con `time.Now()` vía `audit.NewAuditable(now, userID)` / `audit.NewTimestamped(now)`.
- **Tipo C:** inicia `sync: audit.NewMicrosipSync()` (local, aún sin push) o
  `audit.NewMicrosipSyncFromPull(microsipID, now)` si vino de un pull.

### `Hydrate{Entity}` — reconstruye desde DB, cero validación

Usa un struct de params para evitar argumentos posicionales frágiles.

```go
type Hydrate{Entity}Params struct {
    ID     uuid.UUID
    // ... todos los campos
    Status {Entity}Status
    Audit  audit.Auditable       // Tipo A/C
    // Timestamps audit.Timestamped  // Tipo B
    // Sync   audit.MicrosipSync     // Tipo C
}

func Hydrate{Entity}(p Hydrate{Entity}Params) *{Entity} {
    return &{Entity}{
        id: p.ID, /* ... */ status: p.Status, audit: p.Audit,
    }
}
```

Reglas:
- Acepta TODOS los campos (incl. id, status, timestamps, sync).
- Usa struct de params — nunca posicional.
- Cero validación; devuelve solo puntero, sin error.
- Lo usa **exclusivamente** la capa de repositorio.
- Los campos transitorios de Tipo B (`previousState`/`stateChanged`) **no** van en `HydrateParams`.

---

## Getters

Un getter por campo. Nombre = campo en PascalCase. (Sin `Version()`.)

```go
func (e *{Entity}) ID() uuid.UUID            { return e.id }
func (e *{Entity}) Status() {Entity}Status   { return e.status }
func (e *{Entity}) Audit() audit.Auditable   { return e.audit }
```

Reglas: receptor puntero, sin lógica, cada campo con getter.

---

## Type A mutations

### `Update` / verbos de lenguaje ubicuo — mutan solo campos mutables

msp-api usa **verbos de dominio en español** (lenguaje ubicuo) para el comportamiento:
`Cancelar`, `AdjuntarImagen`, `Renombrar`, etc. (los **códigos** de error siguen en inglés).

```go
func (e *{Entity}) Renombrar(nombre string, updatedBy uuid.UUID) error {
    if strings.TrimSpace(nombre) == "" {
        return Err{Entity}NombreRequired
    }
    e.nombre = strings.TrimSpace(nombre)
    e.audit.MarkUpdated(updatedBy) // siempre al final
    return nil
}
```

Reglas:
- Nunca muta: `id`, código (si existe), `createdAt`, `createdBy`.
- Siempre llama `audit.MarkUpdated(updatedBy)` al final.
- Devuelve error si la validación falla.

### `Cambiar{Estado}` — transición de lifecycle (CRUD con status)

```go
func (e *{Entity}) CambiarStatus(nuevo {Entity}Status, updatedBy uuid.UUID) error {
    if !nuevo.IsValid() {
        return Err{Entity}StatusInvalid
    }
    if !e.status.CanTransitionTo(nuevo) {
        return Err{Entity}StatusTransitionInvalid
    }
    e.status = nuevo
    e.audit.MarkUpdated(updatedBy)
    return nil
}
```

---

## Type B mutations — máquina de estados

Las entidades pipeline usan métodos de transición nombrados (no `Update`/`CambiarStatus` genéricos).
Cada transición: (1) valida la máquina, (2) fija el estado destino, (3) captura su timestamp,
(4) captura los datos de salida de esa etapa, (5) `timestamps.MarkUpdated()`.

```go
func (e *{Entity}) Iniciar{Etapa}() error {
    if err := e.transitionTo({Entity}State{Etapa}); err != nil {
        return err
    }
    now := time.Now()
    e.{etapa}StartedAt = &now
    return nil
}

func (e *{Entity}) Completar{Etapa}(/* datos de salida */) error {
    if err := e.transitionTo({Entity}State{Siguiente}); err != nil {
        return err
    }
    now := time.Now()
    e.{etapa}CompletedAt = &now
    // asignar datos de salida
    return nil
}

func (e *{Entity}) Fallar(reason string) error {
    if e.state.IsTerminal() {
        return Err{Entity}AlreadyTerminal
    }
    e.previousState = e.state
    e.stateChanged = true
    e.state = {Entity}StateFailed
    e.errorMessage = &reason
    e.timestamps.MarkUpdated()
    return nil
}
```

### Enforcer privado de transición + tracking transitorio

```go
func (e *{Entity}) transitionTo(target {Entity}State) error {
    if !e.state.CanTransitionTo(target) {
        return Err{Entity}InvalidTransition
    }
    e.previousState = e.state
    e.stateChanged = true
    e.state = target
    e.timestamps.MarkUpdated()
    return nil
}
```

Campos transitorios (privados, **no** persistidos, **no** en `HydrateParams`):

```go
previousState {Entity}State  // set por transitionTo
stateChanged  bool           // set por transitionTo

func (e *{Entity}) PreviousState() {Entity}State { return e.previousState }
func (e *{Entity}) StateChanged() bool           { return e.stateChanged }
```

Reglas: `Fallar()` funciona desde cualquier estado no-terminal; `transitionTo` es privado;
cada transición captura su timestamp y sus datos de salida.

---

## Type C — Microsip sync

Tipo C embebe `audit.Auditable` + `audit.MicrosipSync`. El dominio expone el comportamiento de
sincronización; el repo/worker lo invoca tras un push/pull exitoso.

```go
// C-push: creada local, empujada a Microsip.
func (e *{Entity}) MarcarEmpujada(microsipID int) {
    e.sync.SetMicrosipID(microsipID) // fija el PK de Firebird tras el primer push
    e.sync.MarkPushed()
}

// C-bidi: además se refresca desde Microsip.
func (e *{Entity}) MarcarSincronizadaDesdeMicrosip() {
    e.sync.MarkPulled()
}

func (e *{Entity}) Sync() audit.MicrosipSync { return e.sync }
```

API real de `audit.MicrosipSync` (`internal/platform/audit/audit.go`):
`NewMicrosipSync()`, `NewMicrosipSyncFromPull(id, now)`, `HydrateMicrosipSync(id, pulledAt, pushedAt)`,
`SetMicrosipID(int)`, `MarkPulled()`, `MarkPushed()`, `MicrosipID() *int`, `PulledAt()/PushedAt() *time.Time`.

---

## Embedded audit types

Viven en `internal/platform/audit` (NO en el dominio del módulo). No los redefinas.

- **`audit.Auditable`** (Tipo A/C): `createdAt/updatedAt/createdBy/updatedBy`.
  `NewAuditable(now, userID)`, `HydrateAuditable(...)`, `MarkUpdated(userID)`, getters.
- **`audit.Timestamped`** (Tipo B): `createdAt/updatedAt`. `NewTimestamped(now)`,
  `HydrateTimestamped(...)`, `MarkUpdated()`, getters.
- **`audit.MicrosipSync`** (Tipo C): ver arriba.

`MarkUpdated` usa `time.Now()` internamente; al **escribir** a Firebird envolver con
`firebird.ToWallClock(t)` y al **leer** usar `firebird.ScanUTCTime` (ver `DATETIME_HANDLING.md`).

---

## Invariantes en Go, no en la DB

Los CHECK de la migración son documentación y segunda línea de defensa; la fuente de verdad es el
dominio. **Cada CHECK debe replicarse como validación en `Crear{Entity}`.** Ejemplo `ventas`: el
CHECK `CK_MSP_VENTAS_TIPO_CREDITO_COHERENTE` se replica en `Venta.validateCreditoCoherencia(...)`,
con tests que prueban ambos sentidos (input válido → ok; cada violación → su sentinel específico).

Errores: sentinelas package-level vía `apperror.New{Validation,NotFound,Conflict,...}` (código inglés
snake_case, mensaje español). Ver `02-value-objects-errors.md`. Nunca `errors.New`/`fmt.Errorf` en el
dominio (lo prohíbe el linter `err113`).

---

## Composición del agregado — hijos, `iter.Seq` y eventos de dominio

Cuando el agregado tiene colecciones hijas (p. ej. `Venta` con `Combo`, `Producto`, `Vendedor`,
`Imagen`), todo vive en el mismo package `domain`.

### Hijos de agregado — constructor package-private

Los hijos exponen:
- **Constructor package-private** `new{Child}(...)` — solo la raíz puede crearlos, así un hijo
  nunca se detacha de su padre. `Crear{Entity}` llama `new{Child}` por cada input y los guarda.
- **`Hydrate{Child}(p Hydrate{Child}Params) *{Child}` público** — lo usa el repo al reconstruir.

```go
func newCombo(...) (*Combo, error) { /* valida + arma */ }   // solo la raíz lo llama
func HydrateCombo(p HydrateComboParams) *Combo { /* sin validación, lo usa el repo */ }
```

### Lectura inmutable con `iter.Seq` + accessor `…ForRepo()`

Go 1.23+ range-over-func para que los callers lean sin copiar ni mutar:

```go
func (v *Venta) Combos() iter.Seq[*Combo] {
    return func(yield func(*Combo) bool) {
        for _, c := range v.combos {
            if !yield(c) { return }
        }
    }
}

func (v *Venta) CombosForRepo() []*Combo { return v.combos } // sufijo ForRepo = solo persistencia
```

La capa app itera `for c := range v.Combos()`; el repo toma el slice con `CombosForRepo()`.

### Eventos de dominio

El agregado emite eventos en cada operación que cambia estado. Se **bufferean** en la entidad
(`pendingEvents []Event`) y la capa app los **drena después** de que el commit fue exitoso
(detalle en `06-service-recipes.md`).

```go
type Event interface {
    EventType() string
    AggregateID() uuid.UUID
    OccurredAt() time.Time
    Payload() map[string]any
}

func (v *Venta) PendingEvents() []Event { /* copia defensiva */ }
func (v *Venta) ClearPendingEvents()
```

Los tipos concretos viven en `events.go` y usan la convención canónica `{module}.{verbo}`:
`venta.creada`, `venta.cancelada`, `venta.imagen_adjuntada`. `Crear{Entity}` produce su evento
`…Creada` y lo agrega a `pendingEvents`.

Referencia real: `internal/ventas/domain/combo.go` (hijo), `venta.go` (`Combos()`/`CombosForRepo()`),
`events.go` (eventos).

---

## Módulos de referencia (código real)

- **Tipo A:** `internal/ventas/domain/venta.go` (`CrearVenta`, `Cancelar`, `audit.Auditable`).
- **Tipo C:** `internal/auth/domain/usuario.go` (`audit.MicrosipSync`).

---

## Agent checklist

- [ ] Todos los campos privados; **no hay campo `version`**.
- [ ] `Crear{Entity}`/`New{Entity}` valida todas las restricciones y genera `uuid.New()`.
- [ ] `Hydrate{Entity}` usa `Hydrate{Entity}Params`, devuelve puntero, cero validación.
- [ ] Cada campo tiene getter (sin `Version()`).
- [ ] Tipo A: mutaciones solo en campos mutables + `audit.MarkUpdated(updatedBy)` al final.
- [ ] Tipo A: transición de status valida VO + `CanTransitionTo` + `audit.MarkUpdated`.
- [ ] Tipo B: cada transición captura su timestamp y datos; `Fallar` desde cualquier no-terminal; `transitionTo` privado; transitorios no en `HydrateParams`.
- [ ] Tipo C: embebe `audit.MicrosipSync`; expone `MarcarEmpujada`/`MarcarSincronizada...`.
- [ ] Audit usado desde `internal/platform/audit` (no redefinido).
- [ ] Hijos de agregado: constructor `new{Child}` package-private + `Hydrate{Child}` público.
- [ ] Colecciones hijas expuestas con `iter.Seq` + accessor `…ForRepo()` para el repo.
- [ ] Eventos en `events.go` con convención `{module}.{verbo}`; `pendingEvents` + `PendingEvents()` (copia defensiva) + `ClearPendingEvents()`.
- [ ] Nombre de archivo `{entity}.go`; package `domain`.
