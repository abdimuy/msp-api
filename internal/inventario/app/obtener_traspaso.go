//nolint:misspell // domain vocabulary is Spanish (traspaso, etc.) per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// ObtenerTraspaso loads a traspaso by its Microsip DOCTO_IN_ID. Returns
// domain.ErrTraspasoNoEncontrado when not found. Pass-through to the
// TraspasoRepo port.
func (s *Service) ObtenerTraspaso(ctx context.Context, doctoInID int) (*domain.Traspaso, error) {
	return s.traspasos.FindByID(ctx, doctoInID)
}
