//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// pagos90Window is the trailing window (days) for the live PAGOS_90D credit
// feature — counts of real payments in [now-pagos90Window, now).
const pagos90Window = 90

// pagos90Recientes returns the live trailing-90-day real-payment count per
// clienteID as of now. The bool reports whether the query succeeded: on error
// it returns (nil, false) so callers fall back to the materialized value rather
// than zeroing every client's recency signal on a transient DB failure.
func (s *Service) pagos90Recientes(ctx context.Context, clienteIDs []int, now time.Time) (map[int]int, bool) {
	desde := now.AddDate(0, 0, -pagos90Window)
	m, err := s.repo.ContarPagosRecientes(ctx, clienteIDs, desde, now)
	if err != nil {
		s.logger.WarnContext(ctx, "analytics.pagos90_live_failed",
			slog.String("error", err.Error()),
		)
		return nil, false
	}
	return m, true
}

// pagos90dFor selects the live count for c when the live query succeeded
// (absent → 0, i.e. genuinely no recent payments), or falls back to the
// materialized column when the live query failed.
func pagos90dFor(live map[int]int, ok bool, c *domain.WinbackCandidato) int {
	if !ok {
		return c.Pagos90D()
	}
	return live[c.ClienteID()]
}

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
	live, ok := s.pagos90Recientes(ctx, []int{c.ClienteID()}, now)
	p90 := pagos90dFor(live, ok, c)
	seg, score, recencia, ep := computeSegmentoScore(c, now)
	tier := computeCobranzaTier(c, now)
	diasAtraso, pctPuntualidad := ajustarCobranzaRecencia(c, now)
	cScore, cBanda, cDrivers, cAplica := computeCreditoScore(c, now, s.scorecard, p90)
	rScore, rBanda, rDrivers, rAplica := computeRecompraScore(c, now, s.recompraScorecard, s.btyd)
	clvMonto, clvBanda, clvDrivers, clvResumen, _ := computeCLVConRazones(c, now, s.btyd, s.scorecard, s.clvParams, p90)

	comp := analytics.PulsoComputado{
		Segmento:        seg.String(),
		Score:           score.Int(),
		RecenciaDias:    recencia,
		EstadoPago:      ep.String(),
		TierRiesgo:      tier.String(),
		DiasAtrasoProm:  diasAtraso,
		PctPagosATiempo: pctPuntualidad,
		ScoreCredito:    cScore.Int(),
		BandaCredito:    cBanda.String(),
		CreditoDrivers:  cDrivers,
		ScoreRecompra:   rScore.Int(),
		BandaRecompra:   rBanda.String(),
		RecompraDrivers: rDrivers,
		MontoCLV:        clvMonto.Decimal(),
		BandaCLV:        clvBanda.String(),
		CLVDrivers:      clvDrivers,
		CreditoResumen:  resumenCredito(c, now, cBanda, cScore, cAplica),
		RecompraResumen: resumenRecompra(c, now, rBanda, rScore, rAplica),
		CLVResumen:      clvResumen,
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
	candidateIDs := make([]int, len(candidates))
	for i, c := range candidates {
		candidateIDs[i] = c.ClienteID()
	}
	live, ok := s.pagos90Recientes(ctx, candidateIDs, now)

	result := make(map[int]analytics.ClientePulsoContract, len(candidates))
	for _, c := range candidates {
		p90 := pagos90dFor(live, ok, c)
		seg, score, recencia, ep := computeSegmentoScore(c, now)
		tier := computeCobranzaTier(c, now)
		diasAtraso, pctPuntualidad := ajustarCobranzaRecencia(c, now)
		cScore, cBanda, cDrivers, cAplica := computeCreditoScore(c, now, s.scorecard, p90)
		rScore, rBanda, rDrivers, rAplica := computeRecompraScore(c, now, s.recompraScorecard, s.btyd)
		clvMonto, clvBanda, clvDrivers, clvResumen, _ := computeCLVConRazones(c, now, s.btyd, s.scorecard, s.clvParams, p90)

		comp := analytics.PulsoComputado{
			Segmento:        seg.String(),
			Score:           score.Int(),
			RecenciaDias:    recencia,
			EstadoPago:      ep.String(),
			TierRiesgo:      tier.String(),
			DiasAtrasoProm:  diasAtraso,
			PctPagosATiempo: pctPuntualidad,
			ScoreCredito:    cScore.Int(),
			BandaCredito:    cBanda.String(),
			CreditoDrivers:  cDrivers,
			ScoreRecompra:   rScore.Int(),
			BandaRecompra:   rBanda.String(),
			RecompraDrivers: rDrivers,
			MontoCLV:        clvMonto.Decimal(),
			BandaCLV:        clvBanda.String(),
			CLVDrivers:      clvDrivers,
			CreditoResumen:  resumenCredito(c, now, cBanda, cScore, cAplica),
			RecompraResumen: resumenRecompra(c, now, rBanda, rScore, rAplica),
			CLVResumen:      clvResumen,
		}

		result[c.ClienteID()] = analytics.ToClientePulsoContract(c, comp)
	}
	return result, nil
}
