//nolint:misspell // domain vocabulary is Spanish (traspaso, venta, etc.) per project convention.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// ListarTraspasosPorVenta returns all traspasos linked to the given venta,
// ordered chronologically. Returns an empty slice when none exist.
// Pass-through to the TraspasoRepo port.
func (s *Service) ListarTraspasosPorVenta(ctx context.Context, ventaID uuid.UUID) ([]*domain.Traspaso, error) {
	return s.traspasos.ListByVentaID(ctx, ventaID)
}
