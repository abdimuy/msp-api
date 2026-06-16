//nolint:misspell // clientes vocabulary is Spanish per project convention.
package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// ReindexWorkerConfig tunes the worker's cadence. Zero values fall back to
// sensible production defaults.
type ReindexWorkerConfig struct {
	// Interval is how often the worker wakes to rebuild the search index.
	// Default 5 minutes — short enough to stay fresh, long enough to not
	// hammer the DB.
	Interval time.Duration
}

func (c *ReindexWorkerConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 5 * time.Minute
	}
}

// ReindexWorker runs a background goroutine that periodically calls
// Service.ReindexarBusqueda to keep the in-process Bleve index consistent
// with the Firebird client directory.
//
// On Start it fires an immediate warm-up reindex so search is available
// as soon as possible after boot (before the first scheduled tick). If the
// warm-up fails (DB not yet reachable) a warning is logged and the ticker
// retries on schedule — the app still boots and search degrades to the SQL
// fallback until a successful reindex.
//
// It satisfies lifecycle.Hooks (Start/Stop) and is registered with
// lifecycle.Append by clientes_wiring.go.
type ReindexWorker struct {
	svc    *Service
	clock  outbound.Clock
	cfg    ReindexWorkerConfig
	logger *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewReindexWorker builds a worker. cfg zero values are replaced with defaults.
func NewReindexWorker(
	svc *Service,
	clock outbound.Clock,
	cfg ReindexWorkerConfig,
	logger *slog.Logger,
) *ReindexWorker {
	cfg.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &ReindexWorker{
		svc:    svc,
		clock:  clock,
		cfg:    cfg,
		logger: logger,
	}
}

// Start launches the background loop goroutine. Idempotent: a second Start
// while already running is a no-op. Satisfies the lifecycle.Hooks interface.
func (w *ReindexWorker) Start(ctx context.Context) error {
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
func (w *ReindexWorker) Stop(ctx context.Context) error {
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

// loop runs until ctx is cancelled. It fires an immediate warm-up reindex on
// first entry, then continues on a ticker. This mirrors the cobranza
// pago-retry worker pattern (initial tick before waiting) rather than the
// analytics refresh worker (which defers the first tick).
func (w *ReindexWorker) loop(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	// Warm-up: try an initial reindex so search is ready as soon as possible.
	// If the DB is not yet available, log a warning and let the ticker retry.
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

// tick executes one reindex pass. Errors are logged as warnings and the loop
// continues — a single DB hiccup should not kill the worker.
func (w *ReindexWorker) tick(ctx context.Context) {
	n, err := w.svc.ReindexarBusqueda(ctx)
	if err != nil {
		w.logger.WarnContext(ctx, "clientes_reindex_worker.tick_failed",
			slog.String("error", err.Error()),
		)
		return
	}
	w.logger.InfoContext(ctx, "clientes_reindex_worker.tick_done",
		slog.Int("docs", n),
	)
}
