# CQRS Pattern

How to organize the app layer of a module.

## TL;DR

- One `Service` struct in `app/service.go` aggregates dependencies and lifecycle helpers.
- Commands and queries live in separate files in the same `app/` package — NOT in `app/commands/` and `app/queries/` subpackages.
- Each command is a method on `*Service` defined in its own file (`crear_venta.go`, `cancelar_venta.go`, etc.).
- Each query is similarly a method on `*Service` in its own file (`obtener_venta.go`, `listar_ventas.go`).

## Why no `commands/` and `queries/` subpackages?

Go does not allow methods on a struct to span multiple packages. If `Service` is in `app` and `CrearVenta` is in `app/commands/`, then `CrearVenta` can either:

1. Be a method on a separate `CrearVentaHandler` struct (drops the "Service is the surface" convention), or
2. Live in the same package as `Service` (which means it's not in a subpackage).

We choose option 2 + filename conventions. The reader knows `crear_venta.go` is a command because its only public function is a method on Service named `CrearVenta`.

## `Service` shape

```go
// internal/ventas/app/service.go
type Service struct {
    ventas  outbound.VentaRepo
    storage outbound.StorageProvider
    clock   outbound.Clock
    outbox  outbound.OutboxEnqueuer
    txMgr   *firebird.TxManager
}

func NewService(
    ventas outbound.VentaRepo,
    storage outbound.StorageProvider,
    clock outbound.Clock,
    outbox outbound.OutboxEnqueuer,
    txMgr *firebird.TxManager,
) *Service { ... }

// runInTx is the helper every multi-step command uses.
func (s *Service) runInTx(ctx context.Context, fn func(context.Context) error) error {
    if s.txMgr == nil { return fn(ctx) }
    return s.txMgr.RunInTx(ctx, fn)
}

// enqueueEvent is best-effort by design — outbox failures are logged and
// nil is returned so the business write always wins.
func (s *Service) enqueueEvent(ctx context.Context, ...) { ... }
```

## Command shape

```go
// internal/ventas/app/crear_venta.go
type CrearVentaInput struct {
    // primitive types only — no domain VOs at this boundary.
}

func (s *Service) CrearVenta(ctx context.Context, in CrearVentaInput, by uuid.UUID) (*domain.Venta, error) {
    params, err := in.intoDomain()           // builds VOs, returns the first error.
    if err != nil { return nil, err }
    venta, err := domain.CrearVenta(params)  // domain validates invariants and emits the initial event.
    if err != nil { return nil, err }
    if err := s.runInTx(ctx, func(ctx context.Context) error {
        return s.ventas.Save(ctx, venta)
    }); err != nil {
        return nil, err
    }
    s.drainEvents(ctx, venta)
    return venta, nil
}
```

## Query shape

```go
// internal/ventas/app/obtener_venta.go
func (s *Service) ObtenerVenta(ctx context.Context, id uuid.UUID) (*domain.Venta, error) {
    return s.ventas.FindByID(ctx, id)
}
```

Queries are thin pass-throughs. They don't accept input DTOs or return projections — domain entities pass through and the HTTP layer projects them.

## When NOT to use CQRS

The `auth` module does not use this layout — every command and query is a method on `Service` defined in `usuarios.go`, `roles.go`, `permisos.go`. That works because:

- `auth` operations are short (one or two domain calls + one repo write + one event).
- There are no aggregate-shaped commands (no batched child collections in one POST).
- Read and write code paths are similar enough to share a file by entity.

Use CQRS-explicit (one file per command, one per query) when:

- Commands construct or mutate a non-trivial aggregate with child collections.
- The same `Service` has more than ~6 commands and the methods/file ratio becomes ugly.
- Queries gain projections or filter assembly logic that wants to live separately from writes.

`ventas` hits all three criteria. `auth` hits none.

## Idempotency

For HTTP POST/PATCH endpoints we apply the platform `idempotency.Middleware` at the chi layer (see `cmd/api/server.go`). The middleware hashes the body + `Idempotency-Key` header and replays the cached response on retry. No special handling needed in the command itself.

## Tx boundaries

- **Firebird** business writes happen inside `runInTx`. Multi-table Save (header + children) is one transaction because the repo uses `firebird.GetQuerier(ctx, pool.DB)` which honors the ambient tx installed by `firebird.TxManager`.
- **Outbox** writes happen inside the SAME Firebird tx as the business write. The `{module}outbox.Enqueuer` calls `firebird.TxManager.RunInTx` which is re-entrant — when the caller is already inside a transaction the INSERT into `MSP_OUTBOX_EVENTS` joins it and the COMMIT covers both. Errors propagate so a failed event INSERT rolls back the entire write. See ADR-0008 (which supersedes ADR-0001's best-effort dual-write pattern).
