//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"errors"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ObtenerPredicciones returns Bayesian credible-interval predictions for a single
// client. When the client has no materialized candidato row it degrades gracefully:
// returns PrediccionesContract{Disponible: false} with a nil error.
// Likewise when the BTYD/CLV models are not loaded or the client lacks enough
// purchase history (fewer than 1 distinct purchase month or zero first-purchase
// date) the method degrades without error.
//
// Note on x==0: when the client has a single distinct purchase month,
// Posteriors uses ExpectedAvgProfit(0, monetary) which returns the population
// mean ticket rather than the observed one — slightly optimistic. This is
// acceptable because the endpoint is its own estimator and its point estimate
// may legitimately differ from the materialized score.
func (s *Service) ObtenerPredicciones(ctx context.Context, clienteID int) (analytics.PrediccionesContract, error) {
	const source = "analytics.ObtenerPredicciones"

	c, err := s.repo.GetCandidato(ctx, clienteID)
	if err != nil {
		if errors.Is(err, domain.ErrWinbackCandidatoNotFound) {
			return analytics.PrediccionesContract{Disponible: false}, nil
		}
		return analytics.PrediccionesContract{}, apperror.NewInternal(
			"predicciones_cliente_failed",
			"error al obtener predicciones del cliente",
		).WithSource(source).WithError(err)
	}

	// Gate: models must be loaded and client must have meaningful purchase history.
	if !s.btyd.Loaded() || !s.clvParams.Loaded() ||
		c.FechaPrimerVenta().IsZero() || c.VentasMesesDistintos() < 1 {
		return analytics.PrediccionesContract{Disponible: false}, nil
	}

	now := s.clock.Now()
	x, tx, n := clvVGrid(c, now)
	monetary := c.MonetaryVProm().InexactFloat64()

	live, ok := s.pagos90Recientes(ctx, []int{c.ClienteID()}, now)
	p90 := pagos90dFor(live, ok, c)

	cScore, _, _, cAplica := computeCreditoScore(c, now, s.scorecard, p90)
	pPaga := 1.0
	if cAplica {
		pPaga = float64(cScore.Int()) / 100.0
	}

	saldo := c.Saldo().InexactFloat64()
	perdida := (1 - pPaga) * saldo * s.clvParams.LGD()

	in := PosteriorInput{
		X: x, Tx: tx, N: n,
		Frequency:       x,
		Monetary:        monetary,
		Draws:           0, // Posteriors applies its default of 2000
		Margin:          s.clvParams.Margin(),
		PPaga:           pPaga,
		PerdidaEsperada: perdida,
		HorizonCLV:      s.clvParams.HorizonMonths(),
		Discount:        s.clvParams.MonthlyDiscount(),
	}
	p := s.btyd.Posteriors(in)
	return toPrediccionesContract(p), nil
}

// toPrediccionesContract maps an app-layer Predicciones to its cross-module
// PrediccionesContract. The mapper lives here (in package app) so that the
// root analytics package never has to import analytics/app — which would create
// an import cycle.
func toPrediccionesContract(p Predicciones) analytics.PrediccionesContract {
	return analytics.PrediccionesContract{
		Disponible: p.Disponible,
		PAlive: analytics.IntervaloContract{
			Punto: p.PAlive.Punto,
			Lo:    p.PAlive.Lo,
			Hi:    p.PAlive.Hi,
		},
		ComprasEsperadas12m: analytics.IntervaloContract{
			Punto: p.ComprasEsperadas12m.Punto,
			Lo:    p.ComprasEsperadas12m.Lo,
			Hi:    p.ComprasEsperadas12m.Hi,
		},
		CLV: analytics.IntervaloContract{
			Punto: p.CLV.Punto,
			Lo:    p.CLV.Lo,
			Hi:    p.CLV.Hi,
		},
		ProximaCompraDias: analytics.IntervaloContract{
			Punto: p.ProximaCompraDias.Punto,
			Lo:    p.ProximaCompraDias.Lo,
			Hi:    p.ProximaCompraDias.Hi,
		},
		Draws: p.Draws,
	}
}
