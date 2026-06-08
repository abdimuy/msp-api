//nolint:misspell // domain vocabulary is Spanish (almacén, etc.) per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// ListarAlmacenes returns the full almacenes catalog. Pass-through to the
// AlmacenRepo port.
func (s *Service) ListarAlmacenes(ctx context.Context) ([]domain.Almacen, error) {
	return s.almacenes.ListAll(ctx)
}
