package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// EventosDeVenta returns the venta's event timeline, oldest first. The venta
// must exist — a miss surfaces ErrVentaNotFound so the HTTP layer maps it to
// 404 rather than returning an empty timeline for a non-existent id.
//
// When no event reader is wired (tests, or a deployment that has not opted
// into the timeline) the method returns an empty slice without error: the
// timeline is informational and its absence must not break the venta detail
// screen.
func (s *Service) EventosDeVenta(
	ctx context.Context, ventaID uuid.UUID,
) ([]outbound.VentaEvento, error) {
	// Confirm the venta exists first so we return 404 for unknown ids instead
	// of a misleading empty timeline.
	if _, err := s.ventas.FindByID(ctx, ventaID); err != nil {
		return nil, err
	}
	if s.eventReader == nil {
		return []outbound.VentaEvento{}, nil
	}
	return s.eventReader.EventosDeVenta(ctx, ventaID)
}
