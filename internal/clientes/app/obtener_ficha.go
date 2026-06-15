//nolint:misspell // Spanish vocabulary (ficha, pulso, cliente, etc.) per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// FichaCliente is the assembled 360 view: native identity + aggregated KPIs/series + analytics pulse.
type FichaCliente struct {
	Cliente    *domain.Cliente
	Resumen    outbound.ResumenFicha
	Pulso      analytics.ClientePulsoContract // zero value when TienePulso is false
	TienePulso bool                           // false when the client has no materialized analytics row
}

// ObtenerFicha assembles a Customer 360 view for the given clienteID.
//
// The analytics pulse is fetched on a best-effort basis: if the client has no
// materialized row in the analytics module (TienePulso=false) the ficha is still
// returned successfully. Only a transport failure on ObtenerPulso is treated as
// an error.
func (s *Service) ObtenerFicha(ctx context.Context, clienteID int) (FichaCliente, error) {
	const source = "clientes.ObtenerFicha"

	// Step 1: fetch client identity.
	cliente, err := s.repo.ObtenerCliente(ctx, clienteID)
	if err != nil {
		if appErr, ok := apperror.As(err); ok {
			return FichaCliente{}, appErr.WithSource(source)
		}
		return FichaCliente{}, apperror.NewInternal("cliente_fetch_failed", "error al obtener el cliente").
			WithSource(source).WithError(err)
	}

	// Step 2: fetch aggregated financial summary.
	resumen, err := s.repo.ObtenerResumenFicha(ctx, clienteID)
	if err != nil {
		if appErr, ok := apperror.As(err); ok {
			return FichaCliente{}, appErr.WithSource(source)
		}
		return FichaCliente{}, apperror.NewInternal("resumen_ficha_failed", "error al obtener el resumen del cliente").
			WithSource(source).WithError(err)
	}

	// Step 3: fetch analytics pulse — DEGRADE on not-found, ERROR on transport failure.
	pulso, found, err := s.analytics.ObtenerPulso(ctx, clienteID)
	if err != nil {
		return FichaCliente{}, apperror.NewInternal("pulso_fetch_failed", "error al obtener el pulso de analítica del cliente").
			WithSource(source).WithError(err)
	}

	return FichaCliente{
		Cliente:    cliente,
		Resumen:    resumen,
		Pulso:      pulso,
		TienePulso: found,
	}, nil
}
