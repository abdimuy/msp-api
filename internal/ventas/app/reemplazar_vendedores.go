//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ReemplazarVendedoresInput is the request DTO for replacing the vendedores
// collection of a venta.
type ReemplazarVendedoresInput struct {
	VentaID    uuid.UUID
	Vendedores []CrearVentaVendedorInput
}

// ReemplazarVendedores replaces the venta's vendedores collection.
func (s *Service) ReemplazarVendedores(ctx context.Context, in ReemplazarVendedoresInput, by uuid.UUID) (*domain.Venta, error) {
	venta, err := s.ventas.FindByID(ctx, in.VentaID)
	if err != nil {
		return nil, err
	}
	if err := venta.ReemplazarVendedores(domain.ReemplazarVendedoresParams{
		Vendedores: buildVendedorInputs(in.Vendedores),
		By:         by,
		Now:        s.clock.Now(),
	}); err != nil {
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.ventas.ReplaceVendedores(ctx, venta)
	}); err != nil {
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return venta, nil
}
