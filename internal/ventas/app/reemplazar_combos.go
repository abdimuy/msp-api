//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ReemplazarCombosInput is the request DTO for replacing the combos
// collection of a venta.
type ReemplazarCombosInput struct {
	VentaID uuid.UUID
	Combos  []CrearVentaComboInput
}

// ReemplazarCombos replaces the venta's combos collection.
func (s *Service) ReemplazarCombos(ctx context.Context, in ReemplazarCombosInput, by uuid.UUID) (*domain.Venta, error) {
	venta, err := s.ventas.FindByID(ctx, in.VentaID)
	if err != nil {
		return nil, err
	}
	if err := venta.ReemplazarCombos(domain.ReemplazarCombosParams{
		Combos: buildComboInputs(in.Combos),
		By:     by,
		Now:    s.clock.Now(),
	}); err != nil {
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.ventas.ReplaceCombos(ctx, venta)
	}); err != nil {
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return venta, nil
}
