//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ReemplazarProductosInput is the request DTO for replacing the productos
// collection of a venta.
type ReemplazarProductosInput struct {
	VentaID   uuid.UUID
	Productos []CrearVentaProductoInput
}

// ReemplazarProductos replaces the venta's productos collection.
func (s *Service) ReemplazarProductos(ctx context.Context, in ReemplazarProductosInput, by uuid.UUID) (*domain.Venta, error) {
	venta, err := s.ventas.FindByID(ctx, in.VentaID)
	if err != nil {
		return nil, err
	}
	productos, err := buildProductoInputs(in.Productos)
	if err != nil {
		return nil, err
	}
	if err := venta.ReemplazarProductos(domain.ReemplazarProductosParams{
		Productos: productos,
		By:        by,
		Now:       s.clock.Now(),
	}); err != nil {
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.ventas.ReplaceProductos(ctx, venta)
	}); err != nil {
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return venta, nil
}
