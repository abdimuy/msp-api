//nolint:misspell // analytics vocabulary is Spanish per project convention.
package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// fullRefreshHour is the hour (in UTC, 24-h clock) at which the worker runs a
// full self-healing refresh instead of the regular incremental one. 3 AM UTC
// is well outside business hours for Mexico City (UTC-6 / UTC-5 DST).
const fullRefreshHour = 3

// RefreshWorkerConfig tunes the worker's cadence. Zero values fall back to
// sensible production defaults.
type RefreshWorkerConfig struct {
	// Interval is how often the worker wakes to refresh candidatos. Default 1h.
	// Intervals shorter than 1h cause the full refresh to fire on every tick
	// whose hour equals fullRefreshHour (e.g. a 15m interval runs the full
	// refresh four times during the 03:00 hour); keep it >= 1h in production.
	Interval time.Duration
}

func (c *RefreshWorkerConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = time.Hour
	}
}

// RefreshWorker runs a background goroutine that calls Service.RefrescarCandidatos
// on a regular ticker. At the configurable hour (fullRefreshHour, default 3 AM UTC)
// it triggers a full self-healing refresh; all other ticks are incremental.
//
// It satisfies lifecycle.Hooks (Start/Stop) and is registered with
// lifecycle.Append by analytics_wiring.go.
type RefreshWorker struct {
	svc    *Service
	clock  outbound.Clock
	cfg    RefreshWorkerConfig
	logger *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewRefreshWorker builds a worker. cfg zero values are replaced with defaults.
func NewRefreshWorker(
	svc *Service,
	clock outbound.Clock,
	cfg RefreshWorkerConfig,
	logger *slog.Logger,
) *RefreshWorker {
	cfg.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &RefreshWorker{
		svc:    svc,
		clock:  clock,
		cfg:    cfg,
		logger: logger,
	}
}

// Start launches the background loop goroutine. Idempotent: a second Start
// while already running is a no-op. Satisfies the lifecycle.Hooks interface.
func (w *RefreshWorker) Start(ctx context.Context) error {
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
func (w *RefreshWorker) Stop(ctx context.Context) error {
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

// loop runs until ctx is cancelled, firing tick on every ticker interval.
// Unlike the pago-retry worker, analytics does NOT fire an immediate tick on
// Start — the first refresh was already run (or is not yet due) at deploy
// time; we wait for the first scheduled interval.
func (w *RefreshWorker) loop(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick executes one refresh pass. It decides whether to run a full or
// incremental refresh based on the current UTC hour. Errors are logged and
// the loop continues — a single DB hiccup should not kill the worker.
func (w *RefreshWorker) tick(ctx context.Context) {
	now := w.clock.Now()
	full := fullRefreshDue(now)
	w.logger.InfoContext(ctx, "analytics_refresh_worker.tick_start",
		slog.Bool("full", full),
		slog.String("hour_utc", now.UTC().Format("15:04")),
	)
	result, err := w.svc.RefrescarCandidatos(ctx, full)
	if err != nil {
		w.logger.ErrorContext(ctx, "analytics_refresh_worker.tick_failed",
			slog.Bool("full", full),
			slog.String("error", err.Error()),
		)
		return
	}
	w.logger.InfoContext(ctx, "analytics_refresh_worker.tick_done",
		slog.Bool("full", full),
		slog.Int("procesados", result.Procesados),
		slog.Time("watermark", result.Watermark),
	)
}

// fullRefreshDue reports whether now falls on the designated full-refresh hour.
// Extracted as a pure helper so it can be unit-tested without a real Service.
func fullRefreshDue(now time.Time) bool {
	return now.UTC().Hour() == fullRefreshHour
}
