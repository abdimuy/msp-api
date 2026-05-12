package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// CancelarVenta soft-cancels the venta identified by ventaID. The aggregate
// is loaded, mutated, the header row is updated, and the resulting
// VentaCanceladaEvent is best-effort enqueued onto the outbox.
func (s *Service) CancelarVenta(ctx context.Context, ventaID uuid.UUID, reason string, by uuid.UUID) (*domain.Venta, error) {
	venta, err := s.ventas.FindByID(ctx, ventaID)
	if err != nil {
		return nil, err
	}
	if err := venta.Cancelar(reason, by, s.clock.Now()); err != nil {
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.ventas.Update(ctx, venta)
	}); err != nil {
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return venta, nil
}
