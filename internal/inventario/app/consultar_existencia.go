//nolint:misspell // domain vocabulary is Spanish (existencia, artículo, almacén, etc.) per project convention.
package app

import (
	"context"

	"github.com/shopspring/decimal"
)

// ConsultarExistencia returns the current stock for the given artículo in the
// given almacén. Pass-through to the ExistenciaQuery port.
func (s *Service) ConsultarExistencia(ctx context.Context, articuloID, almacenID int) (decimal.Decimal, error) {
	return s.existencia.Existencia(ctx, articuloID, almacenID)
}
