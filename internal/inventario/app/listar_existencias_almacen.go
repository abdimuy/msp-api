//nolint:misspell // domain vocabulary is Spanish (existencia, almacén, etc.) per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// ListarExistenciasAlmacen returns the full stock snapshot for all artículos
// in the given almacén. Pass-through to the ExistenciaQuery port.
func (s *Service) ListarExistenciasAlmacen(ctx context.Context, almacenID int) ([]domain.Existencia, error) {
	return s.existencia.ExistenciasPorAlmacen(ctx, almacenID)
}
