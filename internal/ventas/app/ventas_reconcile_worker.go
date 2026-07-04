//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// VentasReconcileWorkerConfig tunes the worker's cadence. Zero values fall
// back to sensible production defaults.
type VentasReconcileWorkerConfig struct {
	// Interval is how often the worker wakes to reconcile the Meilisearch
	// ventas index. Default 5 minutes — reads from Firebird and writes to
	// Meilisearch. Matches MEILISEARCH_SYNC_INTERVAL from config.
	Interval time.Duration
}

func (c *VentasReconcileWorkerConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 5 * time.Minute
	}
}

// VentasReconcileWorker runs a background goroutine that periodically calls
// Service.ReconciliarVentas to keep the Meilisearch ventas index consistent
// with Firebird. It is the drift-recovery complement to the incremental
// outbox reindex handler (infra/ventoutbox) — the handler keeps the index
// fresh event-by-event, this worker corrects anything the handler missed
// (e.g. events that failed permanently, or documents indexed before the
// index existed).
//
// On Start it fires an immediate warm-up reconcile so the index is
// populated as soon as possible after boot. If the warm-up fails (DB or
// Meilisearch not yet reachable) a warning is logged and the ticker retries
// on schedule — the app still boots and search falls back to whatever
// state the index is already in.
//
// It satisfies lifecycle.Hooks (Start/Stop) and is registered with
// lifecycle.Append by cmd/api/ventas_wiring.go.
type VentasReconcileWorker struct {
	svc    *Service
	cfg    VentasReconcileWorkerConfig
	logger *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewVentasReconcileWorker builds a worker. cfg zero values are replaced
// with defaults.
func NewVentasReconcileWorker(
	svc *Service,
	cfg VentasReconcileWorkerConfig,
	logger *slog.Logger,
) *VentasReconcileWorker {
	cfg.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &VentasReconcileWorker{
		svc:    svc,
		cfg:    cfg,
		logger: logger,
	}
}

// Start launches the background loop goroutine. Idempotent: a second Start
// while already running is a no-op. Satisfies the lifecycle.Hooks interface.
func (w *VentasReconcileWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return nil
	}
	// fx cancels the OnStart ctx (and applies its StartTimeout deadline) once
	// the start phase completes, so the loop must NOT inherit either or its
	// first reconcile — a full-catalog query — dies with "context deadline
	// exceeded". Keep ctx's values (trace/log) but drop fx's
	// cancellation+deadline; Stop() cancels loopCtx explicitly for clean
	// shutdown.
	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w.cancel = cancel
	w.done = make(chan struct{})
	w.running = true
	go w.loop(loopCtx)
	return nil
}

// Stop signals the background goroutine to exit and waits for it to drain.
// Idempotent. Satisfies the lifecycle.Hooks interface.
func (w *VentasReconcileWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.cancel()
	done := w.done
	w.running = false
	w.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loop runs until ctx is cancelled. It fires an immediate warm-up reconcile
// on first entry, then continues on a ticker (initial tick before waiting).
func (w *VentasReconcileWorker) loop(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	// Warm-up: populate the index immediately so search is available ASAP.
	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick executes one reconcile pass, measuring elapsed time. Errors are
// logged as warnings and the loop continues — a transient DB or
// Meilisearch failure should not kill the worker.
func (w *VentasReconcileWorker) tick(ctx context.Context) {
	start := time.Now()
	n, err := w.svc.ReconciliarVentas(ctx)
	if err != nil {
		w.logger.WarnContext(
			ctx, "ventas_reconcile_worker.tick_failed",
			slog.String("error", err.Error()),
		)
		return
	}
	w.logger.InfoContext(
		ctx, "ventas_reconcile_worker.tick_done",
		slog.Int("docs", n),
		slog.Duration("elapsed", time.Since(start)),
	)
}
