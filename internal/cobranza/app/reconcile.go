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
	// ChangelogRetentionDays is how many days a row stays in the cobranza
	// changelog tables (MSP_PAGOS_CHANGELOG, MSP_SALDOS_CHANGELOG) before the
	// reconciler hard-deletes it. Defaults to 7. Set to 0 to disable changelog
	// pruning entirely.
	ChangelogRetentionDays int
	// ChangelogPruneInterval is how often the changelog pruner runs. Defaults
	// to 1 hour. Independent of Interval (which controls the full reconcile pass).
	ChangelogPruneInterval time.Duration
	// ChangelogPruneMaxPerCall is the upper bound on rows deleted per pruner
	// call, per changelog table. Caps lock contention on very large catch-up
	// passes (e.g. first run after the migration shipped). Defaults to 50_000.
	ChangelogPruneMaxPerCall int
}

func (c *ReconcilerConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 7 * 24 * time.Hour
	}
	if c.PageSize <= 0 {
		c.PageSize = 1000
	}
	// ChangelogRetentionDays < 0 → default 7; == 0 → disabled (no default applied).
	if c.ChangelogRetentionDays < 0 {
		c.ChangelogRetentionDays = 7
	}
	if c.ChangelogPruneInterval <= 0 {
		c.ChangelogPruneInterval = time.Hour
	}
	if c.ChangelogPruneMaxPerCall <= 0 {
		c.ChangelogPruneMaxPerCall = 50_000
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
	// SaldosTombstonesDeleted is how many CARGO_CANCELADO='S' rows the
	// reconciler hard-deleted from MSP_SALDOS_VENTAS in this pass.
	SaldosTombstonesDeleted int
	// PagosTombstonesDeleted is how many CANCELADO='S' rows the reconciler
	// hard-deleted from MSP_PAGOS_VENTAS in this pass.
	PagosTombstonesDeleted int
	StartedAt              time.Time
	FinishedAt             time.Time
}

// ReconcilerDeps groups the Reconciler's collaborators. Required: SaldosLister,
// SaldosRepo, Recomputer, Clock, Logger. Optional (pagos reconciliation):
// PagosLister + PagosRecomputer. Optional (tombstone cleanup): SaldosTombstone
// and/or PagosTombstone. Optional (changelog pruning): PagosChangelogRepo
// and/or SaldosChangelogRepo — if both are nil and ChangelogRetentionDays > 0
// the reconciler logs a warning at Start and skips the pruner goroutine.
type ReconcilerDeps struct {
	SaldosLister    outbound.SaldosLister
	SaldosRepo      outbound.SaldosRepo
	Recomputer      outbound.SaldosRecomputer
	PagosLister     outbound.PagosLister
	PagosRecomputer outbound.PagosRecomputer
	SaldosTombstone outbound.SaldosTombstoneCleaner
	PagosTombstone  outbound.PagosTombstoneCleaner
	// PagosChangelogRepo and SaldosChangelogRepo are required when changelog
	// pruning is enabled (ChangelogRetentionDays > 0). If both are nil and
	// retention > 0, the reconciler starts the other passes but logs a warning
	// and skips the pruner goroutine.
	PagosChangelogRepo  outbound.PagosChangelogRepo
	SaldosChangelogRepo outbound.SaldosChangelogRepo
	Clock               outbound.Clock
	Config              ReconcilerConfig
	Logger              *slog.Logger
}

// Reconciler walks every row in MSP_SALDOS_VENTAS (and optionally
// MSP_PAGOS_VENTAS) on a configurable interval, recomputes each row via the
// Firebird stored procedure, logs (and optionally fixes) any drift, and prunes
// expired tombstones and changelog rows. It implements lifecycle.Hooks so fx
// can manage its goroutines.
type Reconciler struct {
	deps    ReconcilerDeps
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewReconciler builds a Reconciler from a deps struct.
func NewReconciler(deps ReconcilerDeps) *Reconciler {
	deps.Config.applyDefaults()
	return &Reconciler{deps: deps}
}

// Start implements lifecycle.Hooks. Launches the ticker goroutine(s) and
// returns immediately. Idempotent — a second call while already running is a
// no-op.
//
//nolint:contextcheck // intentional: the loops must outlive the fx start ctx.
func (r *Reconciler) Start(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.loop(runCtx)
	}()

	// Spawn the changelog pruner goroutine only when pruning is enabled and at
	// least one repo is wired up. If both repos are nil but retention > 0 we
	// log a warning so operators know the pruner is silently skipped.
	if r.deps.Config.ChangelogRetentionDays > 0 {
		hasPagos := r.deps.PagosChangelogRepo != nil
		hasSaldos := r.deps.SaldosChangelogRepo != nil
		if !hasPagos && !hasSaldos {
			r.deps.Logger.Warn("cobranza.changelog_pruner: retention enabled but both repos are nil — pruner not started",
				slog.Int("retention_days", r.deps.Config.ChangelogRetentionDays))
		} else {
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				r.changelogPruneLoop(runCtx)
			}()
		}
	}

	return nil
}

// Stop signals all goroutines to exit and waits for them to drain, bounded by
// ctx deadline. Implements lifecycle.Hooks.
func (r *Reconciler) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return nil
	}
	cancel := r.cancel
	r.running = false
	r.mu.Unlock()

	cancel()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loop is the reconcile ticker goroutine. Each tick fires Run; transient
// errors are logged and the loop continues.
func (r *Reconciler) loop(ctx context.Context) {
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
				"saldos_tombstones_deleted", report.SaldosTombstonesDeleted,
				"pagos_tombstones_deleted", report.PagosTombstonesDeleted,
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

	if r.deps.Config.TombstoneRetentionDays > 0 {
		cutoff := r.deps.Clock.Now().AddDate(0, 0, -r.deps.Config.TombstoneRetentionDays)
		r.runTombstoneCleanup(ctx, cutoff, &report)
	}

	report.FinishedAt = r.deps.Clock.Now()
	return report, nil
}

// runTombstoneCleanup hard-deletes expired tombstone rows from both caches.
// Both cleaners share the same cutoff (TombstoneRetentionDays). Errors are
// logged and counted; they do not abort the pass.
func (r *Reconciler) runTombstoneCleanup(ctx context.Context, cutoff time.Time, report *ReconcileReport) {
	if r.deps.SaldosTombstone != nil {
		n, err := r.deps.SaldosTombstone.DeleteTombstonesOlderThan(ctx, cutoff)
		if err != nil {
			r.deps.Logger.WarnContext(ctx, "cobranza.reconciler: saldos tombstone cleanup failed",
				"error", err, "cutoff", cutoff)
		} else {
			report.SaldosTombstonesDeleted = n
		}
	}
	if r.deps.PagosTombstone != nil {
		n, err := r.deps.PagosTombstone.DeleteTombstonesOlderThan(ctx, cutoff)
		if err != nil {
			r.deps.Logger.WarnContext(ctx, "cobranza.reconciler: pagos tombstone cleanup failed",
				"error", err, "cutoff", cutoff)
		} else {
			report.PagosTombstonesDeleted = n
		}
	}
}

// changelogPruneLoop is the pruner ticker goroutine. It runs independently of
// the reconcile loop, fires every ChangelogPruneInterval, and calls
// pruneChangelogs on each tick.
func (r *Reconciler) changelogPruneLoop(ctx context.Context) {
	ticker := time.NewTicker(r.deps.Config.ChangelogPruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.pruneChangelogs(ctx)
		}
	}
}

// pruneChangelogs hard-deletes rows older than ChangelogRetentionDays from
// MSP_PAGOS_CHANGELOG and MSP_SALDOS_CHANGELOG. Each table is capped at
// ChangelogPruneMaxPerCall rows per call to avoid lock escalation on Firebird.
// Errors are logged and do not abort the pass — the next tick will retry.
func (r *Reconciler) pruneChangelogs(ctx context.Context) {
	if r.deps.Config.ChangelogRetentionDays <= 0 {
		return
	}
	cutoff := r.deps.Clock.Now().Add(-time.Duration(r.deps.Config.ChangelogRetentionDays) * 24 * time.Hour)
	maxN := r.deps.Config.ChangelogPruneMaxPerCall

	if r.deps.PagosChangelogRepo != nil {
		deleted, err := r.deps.PagosChangelogRepo.DeleteOlderThan(ctx, cutoff, maxN)
		if err != nil {
			r.deps.Logger.WarnContext(ctx, "cobranza.changelog_prune_failed",
				slog.String("kind", "pagos"),
				slog.String("error", err.Error()))
		} else {
			r.deps.Logger.InfoContext(ctx, "cobranza.changelog_pruned",
				slog.String("kind", "pagos"),
				slog.Int("deleted", deleted),
				slog.Time("cutoff", cutoff))
		}
	}
	if r.deps.SaldosChangelogRepo != nil {
		deleted, err := r.deps.SaldosChangelogRepo.DeleteOlderThan(ctx, cutoff, maxN)
		if err != nil {
			r.deps.Logger.WarnContext(ctx, "cobranza.changelog_prune_failed",
				slog.String("kind", "saldos"),
				slog.String("error", err.Error()))
		} else {
			r.deps.Logger.InfoContext(ctx, "cobranza.changelog_pruned",
				slog.String("kind", "saldos"),
				slog.Int("deleted", deleted),
				slog.Time("cutoff", cutoff))
		}
	}
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
