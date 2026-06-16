//nolint:misspell // clientes vocabulary is Spanish per project convention.
package app

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// DirectoryReconcileWorkerConfig tunes the worker's cadence. Zero values fall
// back to sensible production defaults.
type DirectoryReconcileWorkerConfig struct {
	// Interval is how often the worker wakes to reconcile the Meilisearch index.
	// Default 5 minutes — reads from Firebird/analytics and writes to Meilisearch.
	// Matches MEILISEARCH_SYNC_INTERVAL from config.
	Interval time.Duration
}

func (c *DirectoryReconcileWorkerConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 5 * time.Minute
	}
}

// DirectoryReconcileWorker runs a background goroutine that periodically calls
// Service.ReconciliarDirectorio to keep the Meilisearch directory index
// consistent with the Firebird client directory.
//
// On Start it fires an immediate warm-up reconcile so the index is populated
// as soon as possible after boot. If the warm-up fails (DB or Meilisearch not
// yet reachable) a warning is logged and the ticker retries on schedule — the
// app still boots and the index falls back to empty until a successful reconcile.
//
// It satisfies lifecycle.Hooks (Start/Stop) and is registered with
// lifecycle.Append by clientes_wiring.go, running ALONGSIDE the existing
// ReindexWorker (Bleve) — both workers coexist.
type DirectoryReconcileWorker struct {
	svc    *Service
	cfg    DirectoryReconcileWorkerConfig
	logger *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewDirectoryReconcileWorker builds a worker. cfg zero values are replaced with defaults.
func NewDirectoryReconcileWorker(
	svc *Service,
	cfg DirectoryReconcileWorkerConfig,
	logger *slog.Logger,
) *DirectoryReconcileWorker {
	cfg.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &DirectoryReconcileWorker{
		svc:    svc,
		cfg:    cfg,
		logger: logger,
	}
}

// Start launches the background loop goroutine. Idempotent: a second Start
// while already running is a no-op. Satisfies the lifecycle.Hooks interface.
func (w *DirectoryReconcileWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return nil
	}
	loopCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.done = make(chan struct{})
	w.running = true
	go w.loop(loopCtx)
	return nil
}

// Stop signals the background goroutine to exit and waits for it to drain.
// Idempotent. Satisfies the lifecycle.Hooks interface.
func (w *DirectoryReconcileWorker) Stop(ctx context.Context) error {
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

// loop runs until ctx is cancelled. It fires an immediate warm-up reconcile on
// first entry, then continues on a ticker. Mirrors the ReindexWorker pattern
// (initial tick before waiting).
func (w *DirectoryReconcileWorker) loop(ctx context.Context) {
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

// tick executes one reconcile pass, measuring elapsed time. Errors are logged
// as warnings and the loop continues — a transient DB or Meilisearch failure
// should not kill the worker.
func (w *DirectoryReconcileWorker) tick(ctx context.Context) {
	start := time.Now()
	n, err := w.svc.ReconciliarDirectorio(ctx)
	if err != nil {
		w.logger.WarnContext(ctx, "clientes_directory_reconcile_worker.tick_failed",
			slog.String("error", err.Error()),
		)
		return
	}
	w.logger.InfoContext(ctx, "clientes_directory_reconcile_worker.tick_done",
		slog.Int("docs", n),
		slog.Duration("elapsed", time.Since(start)),
	)
}
