package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ObtenerVenta loads a venta by its ID. Returns ErrVentaNotFound on miss.
// Pure pass-through to the repository — the query layer adds no logic over
// the read path today.
func (s *Service) ObtenerVenta(ctx context.Context, ventaID uuid.UUID) (*domain.Venta, error) {
	return s.ventas.FindByID(ctx, ventaID)
}
