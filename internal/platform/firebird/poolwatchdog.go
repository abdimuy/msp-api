package firebird

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"
)

// defaultWatchdogInterval is how often the pool is sampled.
const defaultWatchdogInterval = 30 * time.Second

// PoolWatchdog samples sql.DBStats periodically and logs a WARN as soon as the
// pool runs out of connections or callers start queueing for one.
//
// It exists because pool exhaustion used to be invisible: the API stayed up,
// answered /livez, and simply stopped serving anything that touched Firebird.
// This turns that silence into a log line on the first sample.
type PoolWatchdog struct {
	pool     *Pool
	interval time.Duration
	logger   *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}

	lastWaitCount int64
}

// NewPoolWatchdog builds a watchdog over pool. A non-positive interval falls
// back to defaultWatchdogInterval; a nil logger falls back to slog.Default.
func NewPoolWatchdog(pool *Pool, interval time.Duration, logger *slog.Logger) *PoolWatchdog {
	if interval <= 0 {
		interval = defaultWatchdogInterval
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &PoolWatchdog{pool: pool, interval: interval, logger: logger}
}

// Start launches the sampling loop. Idempotent. Satisfies lifecycle.Hooks.
func (w *PoolWatchdog) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return nil
	}
	// fx cancels the OnStart ctx once the start phase completes, which would
	// kill the loop on the first tick. Keep the values (trace/log), drop the
	// cancellation; Stop() cancels loopCtx explicitly.
	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w.cancel = cancel
	w.done = make(chan struct{})
	w.running = true
	go w.loop(loopCtx)
	return nil
}

// Stop signals the loop to exit and waits for it to drain. Idempotent.
func (w *PoolWatchdog) Stop(ctx context.Context) error {
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
		return ctx.Err() //nolint:wrapcheck // shutdown deadline surfaces verbatim.
	}
}

// loop samples on a ticker until ctx is cancelled.
func (w *PoolWatchdog) loop(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.sample(ctx)
		}
	}
}

// sample reads one snapshot of the pool and evaluates it.
func (w *PoolWatchdog) sample(ctx context.Context) {
	w.evaluate(ctx, w.pool.Stats())
}

// evaluate warns when the snapshot looks saturated: every connection checked
// out, or callers waiting since the previous sample.
func (w *PoolWatchdog) evaluate(ctx context.Context, stats sql.DBStats) {
	waited := stats.WaitCount - w.lastWaitCount
	w.lastWaitCount = stats.WaitCount

	saturated := stats.MaxOpenConnections > 0 && stats.InUse >= stats.MaxOpenConnections
	if !saturated && waited <= 0 {
		return
	}
	w.logger.WarnContext(ctx, "firebird: pool saturado",
		"in_use", stats.InUse,
		"idle", stats.Idle,
		"max_open", stats.MaxOpenConnections,
		"wait_count_total", stats.WaitCount,
		"wait_count_delta", waited,
		"wait_duration", stats.WaitDuration.String(),
	)
}
