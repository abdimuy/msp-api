//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ObtenerTimeline assembles the unified purchase/payment timeline for the given
// client, ordered by date descending (most recent first). It fetches the full
// history — no date bounds applied — so the feed covers the client's entire record.
func (s *Service) ObtenerTimeline(ctx context.Context, clienteID int) ([]domain.EventoTimeline, error) {
	const source = "clientes.ObtenerTimeline"

	// Step 1: validate client exists.
	_, err := s.repo.ObtenerCliente(ctx, clienteID)
	if err != nil {
		if appErr, ok := apperror.As(err); ok {
			return nil, appErr.WithSource(source)
		}
		return nil, apperror.NewInternal("cliente_fetch_failed", "error al obtener el cliente").
			WithSource(source).WithError(err)
	}

	// Step 2: fetch raw payment and sale data. outbound.RangoFechas{} carries nil
	// pointers on both ends, which the repo interprets as "no bound" (all-time).
	data, err := s.repo.ObtenerRitmoPagoData(ctx, clienteID, outbound.RangoFechas{})
	if err != nil {
		if appErr, ok := apperror.As(err); ok {
			return nil, appErr.WithSource(source)
		}
		return nil, apperror.NewInternal("timeline_fetch_failed", "error al obtener el historial del cliente").
			WithSource(source).WithError(err)
	}

	// Step 3: build the unified timeline (pure domain function).
	return domain.BuildTimeline(data.Pagos, data.Ventas), nil
}
