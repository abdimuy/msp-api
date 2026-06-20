//nolint:misspell // Spanish vocabulary (pago, detalle, etc.) per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ObtenerPagoDetalle returns the rich detail for a single payment document.
// Returns domain.ErrPagoNotFound (via apperror) when no row exists for the
// given doctoCCID.
func (s *Service) ObtenerPagoDetalle(ctx context.Context, doctoCCID int) (outbound.PagoDetalle, error) {
	const source = "clientes.ObtenerPagoDetalle"

	detalle, err := s.repo.ObtenerPagoDetalle(ctx, doctoCCID)
	if err != nil {
		if appErr, ok := apperror.As(err); ok {
			return outbound.PagoDetalle{}, appErr.WithSource(source)
		}
		return outbound.PagoDetalle{}, apperror.NewInternal(
			"pago_detalle_failed",
			"error al obtener el detalle del pago",
		).WithSource(source).WithError(err)
	}
	return detalle, nil
}
