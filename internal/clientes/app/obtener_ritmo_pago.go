//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ObtenerRitmoPago assembles the weekly payment-rhythm series for the given client.
// It validates existence first, then delegates raw data fetching to the repo and
// the bucketing/math to domain.BuildRitmoPago.
func (s *Service) ObtenerRitmoPago(ctx context.Context, clienteID int, rango outbound.RangoFechas) (domain.RitmoPago, error) {
	const source = "clientes.ObtenerRitmoPago"

	// Step 1: validate client exists.
	_, err := s.repo.ObtenerCliente(ctx, clienteID)
	if err != nil {
		if appErr, ok := apperror.As(err); ok {
			return domain.RitmoPago{}, appErr.WithSource(source)
		}
		return domain.RitmoPago{}, apperror.NewInternal("cliente_fetch_failed", "error al obtener el cliente").
			WithSource(source).WithError(err)
	}

	// Step 2: fetch raw payment and sale data.
	data, err := s.repo.ObtenerRitmoPagoData(ctx, clienteID, rango)
	if err != nil {
		if appErr, ok := apperror.As(err); ok {
			return domain.RitmoPago{}, appErr.WithSource(source)
		}
		return domain.RitmoPago{}, apperror.NewInternal("ritmo_pago_failed", "error al obtener el ritmo de pago del cliente").
			WithSource(source).WithError(err)
	}

	// Step 3: map outbound.RangoFechas → domain.RangoFechasRitmo (same pointer values).
	rangoDom := domain.RangoFechasRitmo{
		Desde: rango.Desde,
		Hasta: rango.Hasta,
	}

	// Step 4: build the rhythm series (pure function in domain).
	return domain.BuildRitmoPago(data.Pagos, data.Ventas, data.SaldoActual, time.Now().UTC(), rangoDom), nil
}
