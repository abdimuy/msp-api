//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ObtenerPulsoCliente returns the analytics pulse for a single client.
// Returns domain.ErrWinbackCandidatoNotFound (wrapped with source) when the
// client has no materialized analytics row — the caller decides how to degrade.
func (s *Service) ObtenerPulsoCliente(ctx context.Context, clienteID int) (analytics.ClientePulsoContract, error) {
	const source = "analytics.ObtenerPulsoCliente"

	c, err := s.repo.GetCandidato(ctx, clienteID)
	if err != nil {
		appErr, ok := apperror.As(err)
		if ok {
			return analytics.ClientePulsoContract{}, appErr.WithSource(source)
		}
		return analytics.ClientePulsoContract{}, apperror.NewInternal(
			"pulso_cliente_failed",
			"error al obtener pulso del cliente",
		).WithSource(source).WithError(err)
	}

	now := s.clock.Now()
	seg, score, recencia, ep := computeSegmentoScore(c, now)
	tier := computeCobranzaTier(c, now)

	return analytics.ToClientePulsoContract(c, seg.String(), score.Int(), recencia, ep.String(), tier.String()), nil
}

// ObtenerPulsosClientes returns a map keyed by clienteID with the pulse for each
// materialized client among the input IDs. Clients without a materialized row are
// simply absent from the map (no error). Empty input → empty map. Duplicate input
// IDs collapse to a single entry (the map is keyed by clienteID).
func (s *Service) ObtenerPulsosClientes(ctx context.Context, clienteIDs []int) (map[int]analytics.ClientePulsoContract, error) {
	const source = "analytics.ObtenerPulsosClientes"

	if len(clienteIDs) == 0 {
		return map[int]analytics.ClientePulsoContract{}, nil
	}

	candidates, err := s.repo.ListCandidatosByClienteIDs(ctx, clienteIDs)
	if err != nil {
		return nil, apperror.NewInternal(
			"pulsos_clientes_failed",
			"error al obtener pulsos de clientes",
		).WithSource(source).WithError(err)
	}

	now := s.clock.Now()
	result := make(map[int]analytics.ClientePulsoContract, len(candidates))
	for _, c := range candidates {
		seg, score, recencia, ep := computeSegmentoScore(c, now)
		tier := computeCobranzaTier(c, now)
		result[c.ClienteID()] = analytics.ToClientePulsoContract(c, seg.String(), score.Int(), recencia, ep.String(), tier.String())
	}
	return result, nil
}
