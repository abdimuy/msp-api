// Package app — benchmark_query.go computes the peer-benchmark for a single
// client within their zona cohort.
//
//nolint:misspell // Spanish domain vocabulary per project convention.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ventanaAntiguedadMeses is the ±window in calendar months for the
// "antiguedad" cohort sub-filter. Peers whose FechaPrimerVenta falls within
// ventanaAntiguedadMeses months before or after the target's first purchase are
// considered comparable by seniority.
const ventanaAntiguedadMeses = 6

// benchmarkScores holds the four metric values for a single candidato.
type benchmarkScores struct {
	puntualidad       float64
	puntualidadAplica bool
	clv               float64
	clvAplica         bool
	credito           float64
	creditoAplica     bool
	recompra          float64
	recompraAplica    bool
}

// computeBenchmarkScores extracts the four scored metric values for c.
func (s *Service) computeBenchmarkScores(c *domain.WinbackCandidato, now time.Time) benchmarkScores {
	var v benchmarkScores

	pct := c.PctPagosATiempo().InexactFloat64()
	v.puntualidad = pct
	v.puntualidadAplica = pct > 0

	clvMonto, _, _, _, clvAplica := computeCLVConRazones(c, now, s.btyd, s.scorecard, s.clvParams, c.Pagos90D())
	v.clvAplica = clvAplica
	if v.clvAplica {
		v.clv = clvMonto.Decimal().InexactFloat64()
	}

	cs, _, _, creditoAplica := computeCreditoScore(c, now, s.scorecard, c.Pagos90D())
	v.creditoAplica = creditoAplica
	if v.creditoAplica {
		v.credito = float64(cs.Int())
	}

	rs, _, _, recompraAplica := computeRecompraScore(c, now, s.recompraScorecard, s.btyd)
	v.recompraAplica = recompraAplica
	if v.recompraAplica {
		v.recompra = float64(rs.Int())
	}

	return v
}

// benchmarkPeerVectors collects one float64 slice per metric for a set of pares.
// Only peers for which the metric applies are included in each slice.
// Returns (puntualidad, clv, credito, recompra) value vectors.
func (s *Service) benchmarkPeerVectors(pares []*domain.WinbackCandidato, now time.Time) ([]float64, []float64, []float64, []float64) {
	vP := make([]float64, 0, len(pares))
	vC := make([]float64, 0, len(pares))
	vCr := make([]float64, 0, len(pares))
	vR := make([]float64, 0, len(pares))

	for _, p := range pares {
		v := s.computeBenchmarkScores(p, now)
		if v.puntualidadAplica {
			vP = append(vP, v.puntualidad)
		}
		if v.clvAplica {
			vC = append(vC, v.clv)
		}
		if v.creditoAplica {
			vCr = append(vCr, v.credito)
		}
		if v.recompraAplica {
			vR = append(vR, v.recompra)
		}
	}
	return vP, vC, vCr, vR
}

// ObtenerBenchmark computes the peer-benchmark for clienteID within their zona.
// cohortBy controls the sub-filter applied on top of the zona base cohort:
//   - "zona" (default, also used for unknown values): all peers in the zona.
//   - "segmento": peers sharing the same RFM segmento as the target.
//   - "antiguedad": peers whose FechaPrimerVenta is within ±ventanaAntiguedadMeses
//     of the target's first purchase.
//
// The four scored metrics (puntualidad, CLV, credito, recompra) are computed in
// Go for every cohort member using the same scoring functions as ObtenerPulsoCliente,
// per CLAUDE.md §1 (no logic in the database).
func (s *Service) ObtenerBenchmark(ctx context.Context, clienteID int, cohortBy string) (analytics.BenchmarkContract, error) {
	const source = "analytics.ObtenerBenchmark"

	// Normalise cohortBy to one of the three valid values.
	switch cohortBy {
	case "zona", "segmento", "antiguedad":
		// valid
	default:
		cohortBy = "zona"
	}

	// Fetch target candidato — graceful degrade when not materialised.
	target, err := s.repo.GetCandidato(ctx, clienteID)
	if err != nil {
		if errors.Is(err, domain.ErrWinbackCandidatoNotFound) {
			return analytics.BenchmarkContract{Disponible: false, CohortBy: cohortBy}, nil
		}
		return analytics.BenchmarkContract{}, apperror.NewInternal(
			"benchmark_cliente_failed",
			"error al obtener benchmark del cliente",
		).WithSource(source).WithError(err)
	}

	zona := target.Zona()
	if zona == "" {
		return analytics.BenchmarkContract{Disponible: false, CohortBy: cohortBy}, nil
	}

	// Load all zona members (includes target; removed below).
	todos, err := s.repo.ListCandidatosByZona(ctx, zona)
	if err != nil {
		return analytics.BenchmarkContract{}, apperror.NewInternal(
			"benchmark_zona_failed",
			"error al cargar candidatos de la zona",
		).WithSource(source).WithError(err)
	}

	// Remove target from the peer list, then apply cohort sub-filter.
	pares := make([]*domain.WinbackCandidato, 0, len(todos))
	for _, c := range todos {
		if c.ClienteID() != clienteID {
			pares = append(pares, c)
		}
	}

	now := s.clock.Now()
	paresFiltrados := benchmarkSubFiltro(pares, target, cohortBy, now)

	tv := s.computeBenchmarkScores(target, now)
	vP, vC, vCr, vR := s.benchmarkPeerVectors(paresFiltrados, now)

	return analytics.BenchmarkContract{
		Disponible:  true,
		CohortBy:    cohortBy,
		Zona:        zona,
		N:           len(paresFiltrados),
		Puntualidad: buildMetricaBenchmark(tv.puntualidadAplica, tv.puntualidad, vP),
		CLV:         buildMetricaBenchmark(tv.clvAplica, tv.clv, vC),
		Credito:     buildMetricaBenchmark(tv.creditoAplica, tv.credito, vCr),
		Recompra:    buildMetricaBenchmark(tv.recompraAplica, tv.recompra, vR),
	}, nil
}

// benchmarkSubFiltro returns the subset of pares that belong to the same
// sub-cohort as the target, based on cohortBy.
func benchmarkSubFiltro(pares []*domain.WinbackCandidato, target *domain.WinbackCandidato, cohortBy string, now time.Time) []*domain.WinbackCandidato {
	switch cohortBy {
	case "segmento":
		targetSeg, _, _, _ := computeSegmentoScore(target, now)
		out := make([]*domain.WinbackCandidato, 0, len(pares))
		for _, p := range pares {
			seg, _, _, _ := computeSegmentoScore(p, now)
			if seg == targetSeg {
				out = append(out, p)
			}
		}
		return out
	case "antiguedad":
		if target.FechaPrimerVenta().IsZero() {
			return nil
		}
		targetMonth := monthIndex(target.FechaPrimerVenta())
		out := make([]*domain.WinbackCandidato, 0, len(pares))
		for _, p := range pares {
			if p.FechaPrimerVenta().IsZero() {
				continue
			}
			diff := monthIndex(p.FechaPrimerVenta()) - targetMonth
			if diff < 0 {
				diff = -diff
			}
			if diff <= ventanaAntiguedadMeses {
				out = append(out, p)
			}
		}
		return out
	default: // "zona"
		return pares
	}
}

// buildMetricaBenchmark assembles a MetricaBenchmark from the target value and
// peer distribution. When the target does not apply, Aplica=false is returned.
// When N < benchmarkMuestraMinima, MuestraPequena=true and percentil/cuantiles
// are left at zero (not meaningful with too few peers).
func buildMetricaBenchmark(aplica bool, valorTarget float64, valoresPares []float64) analytics.MetricaBenchmark {
	if !aplica {
		return analytics.MetricaBenchmark{Aplica: false}
	}
	percentil, mediana, p25, p75, n := percentilEnCohorte(valorTarget, valoresPares)
	muestraPequena := n < benchmarkMuestraMinima
	mb := analytics.MetricaBenchmark{
		Aplica:         true,
		Valor:          valorTarget,
		N:              n,
		Mediana:        mediana,
		P25:            p25,
		P75:            p75,
		MuestraPequena: muestraPequena,
	}
	if !muestraPequena {
		mb.Percentil = percentil
	}
	return mb
}
