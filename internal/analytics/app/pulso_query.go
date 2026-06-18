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
	cScore, cBanda, cDrivers, _ := computeCreditoScore(c, now, s.scorecard)
	rScore, rBanda, rDrivers, _ := computeRecompraScore(c, now, s.recompraScorecard, s.btyd)
	clvMonto, clvBanda, _ := computeCLV(c, now, s.btyd, s.scorecard, s.clvParams)

	comp := analytics.PulsoComputado{
		Segmento:        seg.String(),
		Score:           score.Int(),
		RecenciaDias:    recencia,
		EstadoPago:      ep.String(),
		TierRiesgo:      tier.String(),
		ScoreCredito:    cScore.Int(),
		BandaCredito:    cBanda.String(),
		CreditoDrivers:  cDrivers,
		ScoreRecompra:   rScore.Int(),
		BandaRecompra:   rBanda.String(),
		RecompraDrivers: rDrivers,
		MontoCLV:        clvMonto.Decimal(),
		BandaCLV:        clvBanda.String(),
	}

	return analytics.ToClientePulsoContract(c, comp), nil
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
		cScore, cBanda, cDrivers, _ := computeCreditoScore(c, now, s.scorecard)
		rScore, rBanda, rDrivers, _ := computeRecompraScore(c, now, s.recompraScorecard, s.btyd)
		clvMonto, clvBanda, _ := computeCLV(c, now, s.btyd, s.scorecard, s.clvParams)

		comp := analytics.PulsoComputado{
			Segmento:        seg.String(),
			Score:           score.Int(),
			RecenciaDias:    recencia,
			EstadoPago:      ep.String(),
			TierRiesgo:      tier.String(),
			ScoreCredito:    cScore.Int(),
			BandaCredito:    cBanda.String(),
			CreditoDrivers:  cDrivers,
			ScoreRecompra:   rScore.Int(),
			BandaRecompra:   rBanda.String(),
			RecompraDrivers: rDrivers,
			MontoCLV:        clvMonto.Decimal(),
			BandaCLV:        clvBanda.String(),
		}

		result[c.ClienteID()] = analytics.ToClientePulsoContract(c, comp)
	}
	return result, nil
}
