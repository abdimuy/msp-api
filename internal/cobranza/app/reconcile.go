package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// ReconcilerConfig configures the Reconciler's polling behavior.
type ReconcilerConfig struct {
	// Interval is how often a full reconcile pass is triggered.
	// Defaults to 7 days when zero.
	Interval time.Duration
	// PageSize is the number of cargo IDs fetched per lister.Page call.
	// Defaults to 1000 when zero.
	PageSize int
	// DriftLog controls whether each detected drift is written to the
	// structured logger. Defaults to true.
	DriftLog bool
	// FixDrift controls whether Recompute is called when drift is detected.
	// Recompute already updates the MSP_SALDOS_VENTAS row atomically, so
	// after the call the cache is refreshed. Defaults to true.
	FixDrift bool
	// TombstoneRetentionDays is how many days a CARGO_CANCELADO='S' row
	// stays in MSP_SALDOS_VENTAS before the reconciler hard-deletes it.
	// Defaults to 30 — long enough that any mobile client which was offline
	// will have either resynced from scratch or seen the tombstone. Set to 0
	// to disable tombstone cleanup entirely.
	TombstoneRetentionDays int
}

func (c *ReconcilerConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 7 * 24 * time.Hour
	}
	if c.PageSize <= 0 {
		c.PageSize = 1000
	}
}

// ReconcileReport summarises a single reconcile pass.
type ReconcileReport struct {
	// Checked is the total number of cargo IDs visited in the saldos pass.
	Checked int
	// Drift is how many saldo rows had mismatched values compared to the
	// recomputed version.
	Drift int
	// Errors counts saldo rows that could not be checked due to transient
	// errors during the saldos pass.
	Errors int
	// PagosChecked is the total number of pago IMPTE IDs visited during the
	// pagos pass. Zero when pagos reconciliation is not configured.
	PagosChecked int
	// PagosErrors counts pago IDs whose recompute call failed.
	PagosErrors int
	// TombstonesDeleted is how many CARGO_CANCELADO='S' rows the reconciler
	// hard-deleted in this pass.
	TombstonesDeleted int
	StartedAt         time.Time
	FinishedAt        time.Time
}

// ReconcilerDeps groups the Reconciler's collaborators. Required: SaldosLister,
// SaldosRepo, Recomputer, Clock, Logger. Optional (pagos reconciliation):
// PagosLister + PagosRecomputer. Optional (tombstone cleanup): TombstoneCleaner.
type ReconcilerDeps struct {
	SaldosLister     outbound.SaldosLister
	SaldosRepo       outbound.SaldosRepo
	Recomputer       outbound.SaldosRecomputer
	PagosLister      outbound.PagosLister
	PagosRecomputer  outbound.PagosRecomputer
	TombstoneCleaner outbound.SaldosTombstoneCleaner
	Clock            outbound.Clock
	Config           ReconcilerConfig
	Logger           *slog.Logger
}

// Reconciler walks every row in MSP_SALDOS_VENTAS (and optionally
// MSP_PAGOS_VENTAS) on a configurable interval, recomputes each row via the
// Firebird stored procedure, logs (and optionally fixes) any drift, and prunes
// expired tombstones. It implements lifecycle.Hooks so fx can manage its goroutine.
type Reconciler struct {
	deps    ReconcilerDeps
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewReconciler builds a Reconciler from a deps struct.
func NewReconciler(deps ReconcilerDeps) *Reconciler {
	deps.Config.applyDefaults()
	return &Reconciler{deps: deps}
}

// Start implements lifecycle.Hooks. Launches the ticker goroutine and returns
// immediately. Idempotent — a second call while already running is a no-op.
func (r *Reconciler) Start(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	r.running = true
	//nolint:contextcheck // intentional: the loop must outlive the fx start ctx.
	go r.loop(runCtx)
	return nil
}

// Stop signals the goroutine to exit and waits for it to drain, bounded by
// ctx deadline. Implements lifecycle.Hooks.
func (r *Reconciler) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return nil
	}
	cancel := r.cancel
	done := r.done
	r.running = false
	r.mu.Unlock()

	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loop is the ticker goroutine. Each tick fires Run; transient errors are
// logged and the loop continues.
func (r *Reconciler) loop(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(r.deps.Config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report, err := r.Run(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				r.deps.Logger.ErrorContext(ctx, "cobranza.reconciler: pass failed", "error", err)
				continue
			}
			r.deps.Logger.InfoContext(
				ctx, "cobranza.reconciler: pass complete",
				"checked", report.Checked,
				"drift", report.Drift,
				"errors", report.Errors,
				"pagos_checked", report.PagosChecked,
				"pagos_errors", report.PagosErrors,
				"tombstones_deleted", report.TombstonesDeleted,
				"duration_ms", report.FinishedAt.Sub(report.StartedAt).Milliseconds(),
			)
		}
	}
}

// Run executes one full reconcile pass.
func (r *Reconciler) Run(ctx context.Context) (ReconcileReport, error) {
	report := ReconcileReport{StartedAt: r.deps.Clock.Now()}

	if err := r.runSaldosPass(ctx, &report); err != nil {
		report.FinishedAt = r.deps.Clock.Now()
		return report, err
	}

	if r.deps.PagosLister != nil && r.deps.PagosRecomputer != nil {
		if err := r.runPagosPass(ctx, &report); err != nil {
			report.FinishedAt = r.deps.Clock.Now()
			return report, err
		}
	}

	if r.deps.TombstoneCleaner != nil && r.deps.Config.TombstoneRetentionDays > 0 {
		cutoff := r.deps.Clock.Now().AddDate(0, 0, -r.deps.Config.TombstoneRetentionDays)
		n, err := r.deps.TombstoneCleaner.DeleteTombstonesOlderThan(ctx, cutoff)
		if err != nil {
			r.deps.Logger.WarnContext(ctx, "cobranza.reconciler: tombstone cleanup failed",
				"error", err, "cutoff", cutoff)
		} else {
			report.TombstonesDeleted = n
		}
	}

	report.FinishedAt = r.deps.Clock.Now()
	return report, nil
}

// runSaldosPass pages through saldos and recomputes each cargo.
func (r *Reconciler) runSaldosPass(ctx context.Context, report *ReconcileReport) error {
	cursor := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		ids, nextCursor, err := r.deps.SaldosLister.Page(ctx, cursor, r.deps.Config.PageSize)
		if err != nil {
			return err
		}

		for _, cargoID := range ids {
			if err := ctx.Err(); err != nil {
				return err
			}
			r.checkOneSaldo(ctx, cargoID, report)
		}

		if nextCursor == 0 {
			return nil
		}
		cursor = nextCursor
	}
}

// checkOneSaldo recomputes a single cargo and compares it against the cache.
// All errors are counted in the report, not propagated.
func (r *Reconciler) checkOneSaldo(ctx context.Context, cargoID int, report *ReconcileReport) {
	report.Checked++

	cached, err := r.deps.SaldosRepo.PorCargo(ctx, cargoID)
	if err != nil {
		report.Errors++
		r.deps.Logger.WarnContext(ctx, "cobranza.reconciler: could not read cached row",
			"cargo_id", cargoID, "error", err)
		return
	}

	recomputed, err := r.deps.Recomputer.Recompute(ctx, cargoID)
	if err != nil {
		report.Errors++
		r.deps.Logger.WarnContext(ctx, "cobranza.reconciler: recompute failed",
			"cargo_id", cargoID, "error", err)
		return
	}

	if r.hasDrift(cached, recomputed) {
		report.Drift++
		if r.deps.Config.DriftLog {
			r.deps.Logger.WarnContext(
				ctx, "cobranza.reconciler: drift detected",
				"cargo_id", cargoID,
				"cached_saldo", cached.Saldo().String(),
				"recomputed_saldo", recomputed.Saldo().String(),
				"cached_total_importe", cached.TotalImporte().String(),
				"recomputed_total_importe", recomputed.TotalImporte().String(),
				"cached_num_pagos", cached.NumPagos(),
				"recomputed_num_pagos", recomputed.NumPagos(),
			)
		}
	}
}

// runPagosPass pages through MSP_PAGOS_VENTAS and recomputes each pago. We
// don't compare values here — the recompute is idempotent and updates
// UPDATED_AT when the row changes, which is the only signal mobile clients
// care about. Drift detection lives in the saldos pass.
func (r *Reconciler) runPagosPass(ctx context.Context, report *ReconcileReport) error {
	cursor := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		ids, nextCursor, err := r.deps.PagosLister.Page(ctx, cursor, r.deps.Config.PageSize)
		if err != nil {
			return err
		}

		for _, impteID := range ids {
			if err := ctx.Err(); err != nil {
				return err
			}
			report.PagosChecked++
			if err := r.deps.PagosRecomputer.Recompute(ctx, impteID); err != nil {
				report.PagosErrors++
				r.deps.Logger.WarnContext(ctx, "cobranza.reconciler: pago recompute failed",
					"impte_id", impteID, "error", err)
			}
		}

		if nextCursor == 0 {
			return nil
		}
		cursor = nextCursor
	}
}

// hasDrift reports whether the cached and recomputed saldo snapshots differ
// on the three fields that can drift: saldo, totalImporte, and numPagos.
func (r *Reconciler) hasDrift(cached, recomputed *domain.Saldo) bool {
	if cached == nil || recomputed == nil {
		return false
	}
	return !cached.Saldo().Equal(recomputed.Saldo()) ||
		!cached.TotalImporte().Equal(recomputed.TotalImporte()) ||
		cached.NumPagos() != recomputed.NumPagos()
}
