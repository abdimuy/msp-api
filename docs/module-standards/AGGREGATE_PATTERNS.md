# Aggregate Patterns

How to model entities and aggregate roots in msp-api domain layers. The two reference modules are `auth` (simple aggregate — `Usuario` has no child collections) and `ventas` (`Venta` has four child collections).

## Private fields, accessor methods

Every entity struct has private fields and exposes them through accessor methods. There are no setters. Mutation goes through behavior methods (`Cancelar`, `AdjuntarImagen`, etc.) that enforce invariants.

```go
type Venta struct {
    id        uuid.UUID
    cliente   ClienteSnapshot
    // ... 15+ private fields
    audit     audit.Auditable
}

func (v *Venta) ID() uuid.UUID                   { return v.id }
func (v *Venta) Cliente() ClienteSnapshot        { return v.cliente }
func (v *Venta) Cancelar(reason string, by uuid.UUID, now time.Time) error { ... }
```

## Two constructors per entity: `NewX` and `HydrateX`

- **`NewX(...)` / `CrearX(p XParams)`** validates every input and returns `(*X, error)`. This is what application code uses for fresh entities. It populates `audit.NewAuditable(now, userID)`.
- **`HydrateX(p HydrateXParams) *X`** bypasses validation. Used only by repositories when reconstructing an entity from persisted rows. The repo's contract guarantees those values were validated on write.

The two constructors return the same struct type — there is no `*XBuilder` or partial type. This keeps the entity's invariants invariant: every `*X` returned to client code has passed validation.

```go
func CrearVenta(p CrearVentaParams) (*Venta, error) { ... }
func HydrateVenta(p HydrateVentaParams) *Venta { ... }
```

`HydrateXParams` is a public struct (so the infra layer can fill it) with one field per private field. The constructor walks the struct in one assignment.

## Child entities have package-private constructors

Children of an aggregate (`Combo`, `Producto`, `Vendedor`, `Imagen` of `Venta`) live in the same `domain` package and expose:

- **Package-private constructor** `newCombo(...)` — only the aggregate root can call this, so children can never be detached from their parent.
- **Public `HydrateCombo(p HydrateComboParams) *Combo`** — used by the repository when reconstructing the aggregate from rows.

When `CrearVenta` runs, it calls `newCombo` for each input combo and tucks the resulting `*Combo` into `Venta.combos`.

## Read-only iteration via `iter.Seq`

Use Go 1.23+ range-over-func iterators for child collections so callers can read without taking a copy and without mutating:

```go
func (v *Venta) Combos() iter.Seq[*Combo] {
    return func(yield func(*Combo) bool) {
        for _, c := range v.combos {
            if !yield(c) { return }
        }
    }
}
```

For the repository, expose explicit slice accessors:

```go
func (v *Venta) CombosForRepo() []*Combo { return v.combos }
```

The "ForRepo" suffix telegraphs that it's intended for persistence code, not application code. The application layer iterates via `for c := range v.Combos()`; the repo grabs the slice.

## Domain events

Aggregates emit events on every state-changing operation. Events are buffered on the aggregate (`pendingEvents []Event`) and drained by the application layer after the database commit succeeds.

```go
type Event interface {
    EventType() string
    AggregateID() uuid.UUID
    OccurredAt() time.Time
    Payload() map[string]any
}

func (v *Venta) PendingEvents() []Event   // returns a defensive copy.
func (v *Venta) ClearPendingEvents()
```

Concrete event types live in `events.go` and use the canonical `{module}.{verb}` naming convention (`venta.creada`, `venta.cancelada`, `venta.imagen_adjuntada`, `venta.imagen_eliminada`).

`CrearVenta` itself produces a `VentaCreadaEvent` and appends it to `pendingEvents`. The app layer:

```go
v, err := domain.CrearVenta(params)
if err != nil { return err }
if err := s.ventas.Save(ctx, v); err != nil { return err }
for _, ev := range v.PendingEvents() {
    s.enqueueEvent(ctx, "venta", ev.AggregateID(), ev.EventType(), ev.Payload())
}
v.ClearPendingEvents()
```

## Sentinel errors

Domain errors are package-level sentinels constructed via `apperror.New{Validation,NotFound,Conflict,Forbidden,Internal}`. They have stable English snake_case codes and Spanish messages:

```go
var ErrVentaYaCancelada = apperror.NewConflict(
    "venta_ya_cancelada",
    "la venta ya está cancelada",
)
```

Never construct errors with `errors.New` or `fmt.Errorf` inside the domain — the `err113` linter forbids it, and the typed `apperror.Error` is what the HTTP layer translates into status codes.

## Invariants are enforced in Go, not the DB

CHECK constraints in the migration are documentation and a second line of defense, but the source of truth is the Go domain. Every CHECK in the migration must be replicated as a validation step in `NewX` / `CrearX`.

Example from `ventas`: the migration's `CK_MSP_VENTAS_TIPO_CREDITO_COHERENTE` (CONTADO has no plan, CREDITO requires plan) is replicated in `Venta.validateCreditoCoherencia(...)`. The tests assert both directions:

- Valid input → constructor succeeds.
- Each individual violation → constructor returns the specific sentinel.

This means an entity assembled in code is always valid before any DB round-trip, and unit tests don't need a database to verify business rules.
