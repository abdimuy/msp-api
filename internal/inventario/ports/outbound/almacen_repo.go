//nolint:misspell // domain vocabulary is Spanish (almacén, etc.) per project convention.
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// AlmacenRepo provides catalog reads for Microsip's ALMACENES table.
type AlmacenRepo interface {
	// FindByID returns the almacén with the given id, or
	// domain.ErrAlmacenNoEncontrado when not found.
	FindByID(ctx context.Context, id int) (*domain.Almacen, error)

	// ListAll returns the full almacenes catalog.
	ListAll(ctx context.Context) ([]domain.Almacen, error)
}
