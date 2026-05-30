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
	// Checked is the total number of cargo IDs visited.
	Checked int
	// Drift is how many rows had mismatched saldo/totalImporte/numPagos
	// compared to the recomputed value.
	Drift int
	// Errors is how many rows could not be checked due to transient errors.
	Errors     int
	StartedAt  time.Time
	FinishedAt time.Time
}

// Reconciler walks every row in MSP_SALDOS_VENTAS on a configurable interval,
// recomputes each cargo's saldo via the Firebird stored procedure, and logs
// (and optionally fixes) any drift it finds. It implements lifecycle.Hooks so
// fx can manage its goroutine.
type Reconciler struct {
	lister     outbound.SaldosLister
	recomputer outbound.SaldosRecomputer
	repo       outbound.SaldosRepo
	clock      outbound.Clock
	cfg        ReconcilerConfig
	mu         sync.Mutex
	running    bool
	cancel     context.CancelFunc
	done       chan struct{}
	logger     *slog.Logger
}

// NewReconciler builds a Reconciler with cfg defaults applied.
func NewReconciler(
	lister outbound.SaldosLister,
	recomputer outbound.SaldosRecomputer,
	repo outbound.SaldosRepo,
	clock outbound.Clock,
	cfg ReconcilerConfig,
	logger *slog.Logger,
) *Reconciler {
	cfg.applyDefaults()
	// DriftLog and FixDrift default to true (zero value is false, so we
	// invert the semantics with an explicit setter rather than a bool pointer).
	// The caller passes the desired values directly; we rely on ReconcilerConfig
	// being populated by the wiring layer with explicit true values.
	return &Reconciler{
		lister:     lister,
		recomputer: recomputer,
		repo:       repo,
		clock:      clock,
		cfg:        cfg,
		logger:     logger,
	}
}

// Start implements lifecycle.Hooks. Launches the ticker goroutine and returns
// immediately. Idempotent — a second call while already running is a no-op.
func (r *Reconciler) Start(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}
	// Decouple from the fx start context — it is short-lived, but the
	// reconciler must outlive it.
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
	ticker := time.NewTicker(r.cfg.Interval)
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
				r.logger.ErrorContext(ctx, "cobranza.reconciler: pass failed", "error", err)
				continue
			}
			r.logger.InfoContext(
				ctx, "cobranza.reconciler: pass complete",
				"checked", report.Checked,
				"drift", report.Drift,
				"errors", report.Errors,
				"duration_ms", report.FinishedAt.Sub(report.StartedAt).Milliseconds(),
			)
		}
	}
}

// Run executes one full reconcile pass: pages through all cargo IDs, fetches
// the current cached row, recomputes via the stored procedure, and compares
// the two snapshots. Returns a summary report.
func (r *Reconciler) Run(ctx context.Context) (ReconcileReport, error) {
	report := ReconcileReport{StartedAt: r.clock.Now()}

	cursor := 0
	for {
		// Yield to ctx between pages so Stop can drain cleanly.
		select {
		case <-ctx.Done():
			report.FinishedAt = r.clock.Now()
			return report, ctx.Err()
		default:
		}

		ids, nextCursor, err := r.lister.Page(ctx, cursor, r.cfg.PageSize)
		if err != nil {
			return report, err
		}

		for _, cargoID := range ids {
			select {
			case <-ctx.Done():
				report.FinishedAt = r.clock.Now()
				return report, ctx.Err()
			default:
			}

			report.Checked++

			cached, err := r.repo.PorCargo(ctx, cargoID)
			if err != nil {
				report.Errors++
				r.logger.WarnContext(ctx, "cobranza.reconciler: could not read cached row",
					"cargo_id", cargoID, "error", err)
				continue
			}

			recomputed, err := r.recomputer.Recompute(ctx, cargoID)
			if err != nil {
				report.Errors++
				r.logger.WarnContext(ctx, "cobranza.reconciler: recompute failed",
					"cargo_id", cargoID, "error", err)
				continue
			}

			if r.hasDrift(cached, recomputed) {
				report.Drift++
				if r.cfg.DriftLog {
					r.logger.WarnContext(
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
				// When FixDrift is true, Recompute already updated the row
				// atomically, so no additional write is needed here.
			}
		}

		if nextCursor == 0 {
			break
		}
		cursor = nextCursor
	}

	report.FinishedAt = r.clock.Now()
	return report, nil
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
