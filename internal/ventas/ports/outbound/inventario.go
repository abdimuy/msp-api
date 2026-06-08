// Package outbound declares the interfaces the ventas module needs from
// other modules. This file declares the ventas module's view of the
// inventario module's command surface.
//
//nolint:misspell // ventas/inventario vocabulary is Spanish per project convention.
package outbound

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// InventarioStockItem mirrors inventario.ValidarStockItem — declared locally
// so this package's clients depend only on the outbound port, not on the
// inventario package directly. The wiring at composition root maps between
// the two with a thin adapter.
type InventarioStockItem struct {
	ArticuloID    int
	AlmacenOrigen int
	Cantidad      decimal.Decimal
}

// InventarioTraspasoDetalle mirrors inventario.CrearTraspasoDetalleInput.
type InventarioTraspasoDetalle struct {
	ArticuloID int
	Cantidad   decimal.Decimal
}

// InventarioCrearTraspasoParams mirrors inventario.CrearTraspasoParaVentaParams
// minus AlmacenDestino — the destino is configured at the inventario module
// boundary so callers do not need to know the magic ID.
type InventarioCrearTraspasoParams struct {
	VentaID       uuid.UUID
	AlmacenOrigen int
	Fecha         time.Time
	Descripcion   string
	Detalles      []InventarioTraspasoDetalle
	CreatedBy     uuid.UUID
}

// InventarioService is the slice of the inventario module's command surface
// that the ventas module consumes. The actual implementation is a thin
// adapter (wired in cmd/api) that fans out to inventario.TraspasoService and
// stamps the configured AlmacenDestinoVentasID before forwarding the call.
//
// Each method may be called as a no-op when the InventarioService field on
// the ventas Service is nil — e.g. tests that don't exercise traspaso flow.
type InventarioService interface {
	// ValidarStockParaVenta verifies every item has enough existencia in its
	// origin almacén before the venta is saved. Returns a validation error
	// (apperror.NewValidation, code "articulo_sin_existencia") when any item
	// fails. The validation runs under READ COMMITTED NO WAIT so simultaneous
	// "last item" sales fail fast.
	ValidarStockParaVenta(ctx context.Context, items []InventarioStockItem) error

	// CrearTraspasoParaVenta creates the automatic traspaso that moves the
	// venta's productos from each producto's origin almacén to the
	// configured destino. Returns the assigned Microsip DOCTO_IN_ID.
	CrearTraspasoParaVenta(ctx context.Context, p InventarioCrearTraspasoParams) (int, error)

	// CrearTraspasoReverso creates the inverse traspaso that returns the
	// productos to their origin almacén. Used when a venta is canceled
	// before it has been aplicada in Microsip.
	CrearTraspasoReverso(ctx context.Context, ventaID, by uuid.UUID) (int, error)
}
