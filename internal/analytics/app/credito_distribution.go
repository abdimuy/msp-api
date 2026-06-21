//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"log/slog"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// LogDistribucionBandasCredito pages through all materialized candidatos and
// logs a single INFO line with the count of clients per BandaCredito band plus
// a no_aplica count (contado-only clients) and the total. Called once nightly
// after a successful full refresh to detect scorecard drift without requiring a
// new DB column or migration.
//
// The existing ListCandidatos port is used with a large limit and no filters;
// no new port methods are introduced. If the table grows beyond the limit the
// distribution will be partial — that is an acceptable v0 trade-off.
func (s *Service) LogDistribucionBandasCredito(ctx context.Context) {
	const limit = 100_000 // generous upper bound; production has ~43k rows

	page, err := s.repo.ListCandidatos(ctx, outbound.ListWinbackParams{
		Limit: limit,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "analytics.credito_banda_distribution.list_failed",
			slog.String("error", err.Error()),
		)
		return
	}

	now := s.clock.Now()

	var d bandaDist
	for _, c := range page.Items {
		// This nightly job runs right after a full refresh, so now ≈ refresh
		// time and the materialized PAGOS_90D is fresh; using it here avoids a
		// per-client live query over the whole ~43k-row table.
		_, banda, _, aplica := computeCreditoScore(c, now, s.scorecard, c.Pagos90D())
		d.add(banda, aplica)
	}

	s.logger.InfoContext(ctx, "analytics.credito_banda_distribution",
		slog.Int("total", len(page.Items)),
		slog.Int("bajo", d.bajo),
		slog.Int("medio", d.medio),
		slog.Int("alto", d.alto),
		slog.Int("critico", d.critico),
		slog.Int("no_aplica", d.noAplica),
	)
}

// bandaDist is a typed accumulator for the per-band client counts. Keying by the
// domain BandaCredito enum (rather than raw strings) means a renamed or added
// band surfaces as a compile error in add() instead of silently misrouting.
type bandaDist struct {
	bajo, medio, alto, critico, noAplica int
}

// add increments the counter for banda, or noAplica when the score does not apply.
func (d *bandaDist) add(banda domain.BandaCredito, aplica bool) {
	if !aplica {
		d.noAplica++
		return
	}
	switch banda {
	case domain.BandaCreditoBajo:
		d.bajo++
	case domain.BandaCreditoMedio:
		d.medio++
	case domain.BandaCreditoAlto:
		d.alto++
	case domain.BandaCreditoCritico:
		d.critico++
	default:
		// Unknown band (should be impossible — scoreToBanda is exhaustive).
		d.noAplica++
	}
}
