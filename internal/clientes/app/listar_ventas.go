//nolint:misspell // Spanish vocabulary (ventas, cliente, etc.) per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ListarVentasInput aggregates the client ID and pagination params for the
// ventas listing query.
type ListarVentasInput struct {
	ClienteID  int
	Pagination outbound.ListParams
}

// ListarVentas returns a cursor-paginated page of sale headers for the given
// client, ordered by sale date descending. Pure pass-through to the repository.
func (s *Service) ListarVentas(ctx context.Context, in ListarVentasInput) (outbound.Page[*domain.VentaCliente], error) {
	const source = "clientes.ListarVentas"

	page, err := s.repo.ListarVentas(ctx, in.ClienteID, in.Pagination)
	if err != nil {
		return outbound.Page[*domain.VentaCliente]{}, apperror.NewInternal(
			"ventas_cliente_list_failed",
			"error al listar las ventas del cliente",
		).WithSource(source).WithError(err)
	}
	return page, nil
}
