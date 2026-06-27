// Package app — cartera_rollrate.go contains snapshot materialization and
// roll-rate computation for the cartera analytics module.
//
// Two methods on *Service (Task B5):
//   - MaterializarCarteraSnapshot — fetches current zone-level aging and
//     persists a point-in-time cut into MSP_AN_CARTERA_SNAPSHOT. Called by
//     RefreshWorker on full ticks alongside LogDistribucionBandasCredito.
//   - ObtenerRollRate — compares the 2 most recent snapshot cuts and returns
//     the signed net delinquency migration scalar (domain.RollRate). Returns
//     Disponible=false ("acumulando datos") when fewer than 2 cuts exist.
//
//nolint:misspell // Spanish domain vocabulary per project convention.
package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// snapshotFetchLimit is the maximum number of snapshot rows fetched from the
// repo when computing the roll-rate. Two consecutive cuts × 500 zones × 4
// buckets = 4 000 rows; 5 000 provides ample headroom for large portfolios.
const snapshotFetchLimit = 5000

// ─── MaterializarCarteraSnapshot ─────────────────────────────────────────────

// MaterializarCarteraSnapshot fetches the current zone-level aging distribution
// and persists a point-in-time snapshot batch into MSP_AN_CARTERA_SNAPSHOT.
// It then records a "cartera_snapshot" refresh state so the scheduler can track
// the last run time.
//
// Called by RefreshWorker on full ticks, after LogDistribucionBandasCredito.
//
// Design invariants:
//   - now is provided by the caller (CLAUDE.md §1 — no time.Now() in app).
//   - Failures are LOGGED and do NOT propagate — a snapshot failure must not
//     abort the broader refresh tick.
//   - Aging rows with invalid parameters (e.g. ZonaClienteID <= 0) are skipped
//     with a WARN log; valid rows are still persisted.
//   - SaveRefreshState("cartera_snapshot") is recorded when:
//     (a) len(rows) > 0 AND SaveCarteraSnapshot succeeded, or
//     (b) the portfolio is empty (0 valid rows — valid completion, just no data).
//   - SaveRefreshState is NOT called when AgingSaldosByZona or SaveCarteraSnapshot
//     failed, so the scheduler can distinguish "ran but empty" from "not ran".
func (s *Service) MaterializarCarteraSnapshot(ctx context.Context, now time.Time) {
	const src = "analytics.MaterializarCarteraSnapshot"

	if s.carteraRepo == nil {
		s.logger.WarnContext(ctx, src+".cartera_repo_nil")
		return
	}

	agingRows, err := s.carteraRepo.AgingSaldosByZona(ctx, now)
	if err != nil {
		s.logger.ErrorContext(ctx, src+".aging_failed",
			slog.String("error", err.Error()),
		)
		return
	}

	rows := make([]domain.CarteraSnapshot, 0, len(agingRows))
	for _, r := range agingRows {
		snap, buildErr := domain.NewCarteraSnapshot(domain.NewCarteraSnapshotParams{
			FechaCorte:    now,
			ZonaClienteID: r.ZonaClienteID,
			CobradorID:    nil, // zone-level aggregate, no cobrador breakdown
			Bucket:        r.Bucket,
			Saldo:         r.Saldo,
			Conteo:        r.Conteo,
			Now:           now,
		})
		if buildErr != nil {
			s.logger.WarnContext(ctx, src+".invalid_row",
				slog.String("error", buildErr.Error()),
				slog.Int("zona", r.ZonaClienteID),
				slog.String("bucket", r.Bucket),
			)
			continue
		}
		rows = append(rows, *snap)
	}

	if len(rows) > 0 {
		if saveErr := s.carteraRepo.SaveCarteraSnapshot(ctx, rows); saveErr != nil {
			s.logger.ErrorContext(ctx, src+".save_failed",
				slog.String("error", saveErr.Error()),
			)
			return
		}
	}

	// Record completion whether or not there were rows (empty portfolio is a
	// valid run). This is reached only when no fatal error occurred above.
	if stateErr := s.repo.SaveRefreshState(ctx, outbound.RefreshState{
		Job:       "cartera_snapshot",
		LastRunAt: now,
	}); stateErr != nil {
		s.logger.ErrorContext(ctx, src+".save_state_failed",
			slog.String("error", stateErr.Error()),
		)
	}

	s.logger.InfoContext(ctx, src+".done",
		slog.Int("rows", len(rows)),
		slog.Time("fecha_corte", now),
	)
}

// ─── ObtenerRollRate ──────────────────────────────────────────────────────────

// ObtenerRollRate compares the 2 most recent cartera snapshot cuts and returns
// the signed net delinquency migration scalar (domain.RollRate).
//
// The scalar is in [-1,+1]:
//   - Positive → net portfolio deterioration (balance moved to worse buckets).
//   - Negative → net portfolio improvement.
//   - Zero     → no net migration, or either distribution is empty.
//
// When fewer than 2 distinct FECHA_CORTE cuts have been persisted the result
// carries Disponible=false ("acumulando datos") and is NOT an error — the
// system is still warming up.
//
// Bucket saldos are aggregated across ALL zones (portfolio-level roll-rate).
// Zone/cobrador filtering for the roll-rate is the responsibility of the HTTP
// layer (Task B6).
func (s *Service) ObtenerRollRate(ctx context.Context) (analytics.RollRateContract, error) {
	const source = "analytics.ObtenerRollRate"

	if s.carteraRepo == nil {
		return analytics.RollRateContract{}, apperror.NewInternal(
			"cartera_repo_nil",
			"el repo de cartera no está configurado",
		).WithSource(source).WithError(ErrCarteraRepoNotConfigured)
	}

	snapshots, err := s.carteraRepo.ListRecentSnapshots(ctx, snapshotFetchLimit)
	if err != nil {
		return analytics.RollRateContract{}, apperror.NewInternal(
			"cartera_snapshot_list_failed",
			"error al obtener snapshots de cartera",
		).WithSource(source).WithError(err)
	}

	// Identify the 2 most recent distinct FECHA_CORTE cuts.
	// ListRecentSnapshots returns rows FECHA_CORTE DESC, so the first distinct
	// date is the newest cut; the second is the immediately preceding cut.
	var cuts []time.Time
	cutSet := make(map[time.Time]bool)
	for _, snap := range snapshots {
		fc := snap.FechaCorte()
		if !cutSet[fc] {
			cutSet[fc] = true
			cuts = append(cuts, fc)
		}
		if len(cuts) == 2 {
			break
		}
	}
	if len(cuts) < 2 {
		// Acumulando datos: not enough cuts yet to compute a roll-rate.
		return analytics.RollRateContract{Disponible: false}, nil
	}

	// cuts[0] is the more recent (curr); cuts[1] is the older (prev).
	fechaReciente := cuts[0]
	fechaAnterior := cuts[1]

	// Aggregate bucket saldo totals across ALL zones for each cut.
	prevDist := make(map[domain.AgingBucket]decimal.Decimal)
	currDist := make(map[domain.AgingBucket]decimal.Decimal)
	for _, snap := range snapshots {
		fc := snap.FechaCorte()
		bucket := domain.AgingBucket(snap.Bucket())
		saldo := snap.Saldo()
		switch {
		case fc.Equal(fechaAnterior):
			prevDist[bucket] = prevDist[bucket].Add(saldo)
		case fc.Equal(fechaReciente):
			currDist[bucket] = currDist[bucket].Add(saldo)
		}
	}

	rr := domain.RollRate(prevDist, currDist)

	return analytics.RollRateContract{
		Disponible:         true,
		RollRate:           rr,
		FechaCorteAnterior: fechaAnterior,
		FechaCorteReciente: fechaReciente,
	}, nil
}
