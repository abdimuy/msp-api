//nolint:misspell // Spanish vocabulary (venta, detalle, cliente, etc.) per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ObtenerVentaDetalle returns the full detail bundle for a single sale,
// including its line items, credit contract (if any), and payment history.
// Returns domain.ErrVentaNotFound (via apperror) when no row exists for
// the given doctoPVID.
func (s *Service) ObtenerVentaDetalle(ctx context.Context, doctoPVID int) (outbound.VentaDetalle, error) {
	const source = "clientes.ObtenerVentaDetalle"

	detalle, err := s.repo.ObtenerVentaDetalle(ctx, doctoPVID)
	if err != nil {
		if appErr, ok := apperror.As(err); ok {
			return outbound.VentaDetalle{}, appErr.WithSource(source)
		}
		return outbound.VentaDetalle{}, apperror.NewInternal(
			"venta_detalle_failed",
			"error al obtener el detalle de la venta",
		).WithSource(source).WithError(err)
	}
	return detalle, nil
}
