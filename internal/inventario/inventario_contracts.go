// Package inventario is the cross-module surface of the inventario bounded
// context. Other modules import only this package — never
// internal/inventario/domain, internal/inventario/app, or
// internal/inventario/infra. The depguard linter enforces the rule.
//
// The contract exports:
//   - TraspasoService: the command interface invoked by the ventas module to
//     create traspasos and revoke them when a venta is canceled.
//   - Projected DTOs (Traspaso, Almacen, Existencia, ...) consumed by HTTP
//     handlers and other modules.
//   - Input types for cross-module commands.
//
//nolint:misspell // Spanish vocabulary (Descripcion, Traspaso) per project convention.
package inventario

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// TraspasoService is the cross-module surface of the inventario app layer.
// The ventas module depends on this interface (not on
// internal/inventario/app.Service) so the dependency direction stays:
// ventas → inventario_contracts → inventario/app.
type TraspasoService interface {
	// ValidarStockParaVenta verifies each item has enough existencia in its
	// origin almacén. Returns inventario.ErrArticuloSinExistencia (a
	// validation error with details: articulo_id, almacen_id,
	// cantidad_requerida, existencia_disponible) when any item fails the
	// check. The validation runs inside a READ COMMITTED NO WAIT transaction
	// so simultaneous "last item" sales fail fast.
	ValidarStockParaVenta(ctx context.Context, items []ValidarStockItem) error

	// CrearTraspasoParaVenta creates the automatic traspaso that moves the
	// venta's productos from their origin almacén to the
	// AlmacenDestinoVentasID configured for the module. Returns the created
	// Traspaso plus the Microsip DOCTO_IN_ID. Called inside the ambient
	// transaction owned by ventas — the inventario tx is re-entrant via
	// firebird.TxManager.RunInTx so both halves commit or roll back together.
	CrearTraspasoParaVenta(ctx context.Context, p CrearTraspasoParaVentaParams) (Traspaso, int, error)

	// CrearTraspasoReverso creates the inverse traspaso that returns the
	// productos to their origin almacén. Used when a venta is canceled
	// before it has been aplicada in Microsip. Returns an error when no
	// directo traspaso is found or when multiple directos exist (which
	// requires manual resolution).
	CrearTraspasoReverso(ctx context.Context, ventaID, by uuid.UUID) (Traspaso, int, error)

	// ResincronizarTraspasoParaVenta keeps the inventory reservation for a
	// venta consistent with a possibly-changed set of detalles. It is a
	// superset of CrearTraspasoParaVenta: if an active directo already
	// exists it is reversed first, then a new directo is created with the
	// new detalles. When the net effect is identical (same almacenes, same
	// quantities) the call is a no-op that returns the active traspaso.
	// When p.Detalles is empty and no active directo exists it returns a
	// zero-value Traspaso with doctoInID 0 and nil error — callers must
	// treat (zero-ID Traspaso, 0, nil) as the no-op / reverse-only case.
	ResincronizarTraspasoParaVenta(ctx context.Context, p CrearTraspasoParaVentaParams) (Traspaso, int, error)
}

// ValidarStockItem is one item submitted to ValidarStockParaVenta.
type ValidarStockItem struct {
	ArticuloID    int
	AlmacenOrigen int
	Cantidad      decimal.Decimal
}

// CrearTraspasoParaVentaParams is the input to CrearTraspasoParaVenta.
type CrearTraspasoParaVentaParams struct {
	VentaID        uuid.UUID
	AlmacenOrigen  int
	AlmacenDestino int
	Fecha          time.Time
	Descripcion    string
	Detalles       []CrearTraspasoDetalleInput
	CreatedBy      uuid.UUID
}

// CrearTraspasoDetalleInput is one detalle inside a CrearTraspasoParaVenta
// request — one row per articulo with a positive cantidad.
type CrearTraspasoDetalleInput struct {
	ArticuloID int
	Cantidad   decimal.Decimal
}

// Traspaso is the projected, cross-module view of a domain.Traspaso. It is
// a flat struct of primitive values so other modules can consume it without
// importing the inventario domain types.
type Traspaso struct {
	ID             uuid.UUID
	Folio          string
	AlmacenOrigen  int
	AlmacenDestino int
	Fecha          time.Time
	Descripcion    string
	VentaID        *uuid.UUID
	TipoReverso    bool
	DoctoInID      *int
	Detalles       []TraspasoDetalle
	CreatedAt      time.Time
	CreatedBy      uuid.UUID
}

// TraspasoDetalle is the projected view of a domain.TraspasoDetalle.
type TraspasoDetalle struct {
	ID         uuid.UUID
	ArticuloID int
	Cantidad   decimal.Decimal
}

// Almacen is the projected view of an ALMACENES row.
type Almacen struct {
	ID     int
	Nombre string
}

// Existencia is the projected view of a (articulo, almacén) stock pair. The
// Cantidad may be zero or negative if the almacén has been oversold; it is
// a SUM(ENTRADAS - SALIDAS) from SALDOS_IN.
type Existencia struct {
	ArticuloID int
	AlmacenID  int
	Cantidad   decimal.Decimal
}
