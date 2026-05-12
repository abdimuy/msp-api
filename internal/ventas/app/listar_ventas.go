package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ListarVentasInput aggregates the pagination params and the filter set for
// the listing query. It exists so callers can pass a single value through
// the http handler boundary rather than two structs.
type ListarVentasInput struct {
	Pagination outbound.ListParams
	Filters    outbound.ListVentasFilters
}

// ListarVentas returns a cursor-paginated page of ventas matching the input
// filters. Pure pass-through to the repository — no per-item logic today.
func (s *Service) ListarVentas(ctx context.Context, in ListarVentasInput) (outbound.Page[*domain.Venta], error) {
	return s.ventas.List(ctx, in.Pagination, in.Filters)
}
