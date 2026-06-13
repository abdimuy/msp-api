//nolint:misspell // domain vocabulary is Spanish (ventas, productos, traspaso) per project convention.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// resincronizarTraspaso keeps the inventory reservation for a venta consistent
// with its current set of productos and combos. Called inside a transaction
// after ReemplazarProductos or ReemplazarCombos persists the new collections.
//
// Skips when:
//   - s.inventario is nil (inventario not wired)
//   - venta.IsAplicada() — the traspaso is already materialized in Microsip
//
// When detalles is empty (all line items removed), ResincronizarTraspasoParaVenta
// is still called — the inventario module reverses any active traspaso, or
// no-ops when there is none.
//
// Stock validation is intentionally NOT performed here. It happens inside
// ResincronizarTraspasoParaVenta AFTER the active directo is reversed, so
// the existencia read reflects the released stock and does not double-count
// the old reservation as unavailable.
func (s *Service) resincronizarTraspaso(ctx context.Context, venta *domain.Venta, by uuid.UUID, now time.Time) error {
	if s.inventario == nil {
		return nil
	}
	if venta.IsAplicada() {
		return nil
	}
	detalles, almacenOrigen, err := buildTraspasoDetallesFromVenta(venta)
	if err != nil {
		return err
	}
	_, err = s.inventario.ResincronizarTraspasoParaVenta(ctx, outbound.InventarioCrearTraspasoParams{
		VentaID:       venta.ID(),
		AlmacenOrigen: almacenOrigen,
		Fecha:         now,
		Descripcion:   "Traspaso automático por venta " + venta.ID().String(),
		CreatedBy:     by,
		Detalles:      detalles,
	})
	return err
}
