//nolint:misspell // ventas vocabulary is Spanish (productos, almacén, etc.) per project convention.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// CancelarVenta soft-cancels the venta identified by ventaID. The aggregate
// is loaded, mutated, the header row is updated, and the resulting
// VentaCanceladaEvent is best-effort enqueued onto the outbox.
//
// When an InventarioService is wired and the venta has not yet been aplicada
// (Sincronizacion == pendiente), the cancel additionally invokes
// CrearTraspasoReverso inside the same Firebird transaction so the productos
// return to their origin almacén atomically with the cancel. Already-aplicada
// ventas leave the reverse to Microsip's normal cancel flow — issuing a
// traspaso reverso on top would double-count.
func (s *Service) CancelarVenta(ctx context.Context, ventaID uuid.UUID, reason string, by uuid.UUID) (*domain.Venta, error) {
	venta, err := s.ventas.FindByID(ctx, ventaID)
	if err != nil {
		return nil, err
	}
	if err := venta.Cancelar(reason, by, s.clock.Now()); err != nil {
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		if updErr := s.ventas.Update(ctx, venta); updErr != nil {
			return updErr
		}
		if s.inventario != nil && !venta.IsAplicada() {
			if _, revErr := s.inventario.CrearTraspasoReverso(ctx, venta.ID(), by); revErr != nil {
				return revErr
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return venta, nil
}
