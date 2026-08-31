package app

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// defaultReenvioBatch is the number of pendientes DrenarCola processes per
// worker tick when ReenvioWorkerConfig.Batch is unset.
const defaultReenvioBatch = 50

// defaultReenvioInterval is how often the worker wakes to drain the mailbox
// when ReenvioWorkerConfig.Interval is unset. The store's on-premise server
// is frequently down for tens of minutes at a time (its tunnel rotates
// every 60 minutes and someone has to start it by hand), so there is
// nothing to gain from a tighter cadence than this — a message forwarded a
// few tens of seconds late is not the failure mode this module guards
// against; losing it while the VPS is down is.
const defaultReenvioInterval = 30 * time.Second

// ReenvioWorkerConfig tunes the worker's cadence and batch size. Zero
// values fall back to sensible defaults.
type ReenvioWorkerConfig struct {
	// Interval is how often the worker wakes to drain the mailbox. Default
	// defaultReenvioInterval.
	Interval time.Duration
	// Batch is the max pendientes drained per tick. Default
	// defaultReenvioBatch.
	Batch int
}

func (c *ReenvioWorkerConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = defaultReenvioInterval
	}
	if c.Batch <= 0 {
		c.Batch = defaultReenvioBatch
	}
}

// ReenvioWorker runs a background goroutine that calls Service.DrenarCola on
// a regular ticker, forwarding mailboxed messages to the store's on-premise
// server.
//
// It satisfies lifecycle.Hooks (Start/Stop) and is registered with
// lifecycle.Append at composition root (internal/canal/module.go, Task 7).
type ReenvioWorker struct {
	svc    *Service
	cfg    ReenvioWorkerConfig
	logger *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewReenvioWorker builds a worker. cfg zero values are replaced with
// defaults. logger may be nil; slog.Default() is used instead.
func NewReenvioWorker(svc *Service, cfg ReenvioWorkerConfig, logger *slog.Logger) *ReenvioWorker {
	cfg.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &ReenvioWorker{svc: svc, cfg: cfg, logger: logger}
}

// Start launches the background loop goroutine. Idempotent: a second Start
// while already running is a no-op. Satisfies the lifecycle.Hooks interface.
func (w *ReenvioWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return nil
	}
	// fx cancels the OnStart ctx (and applies its StartTimeout deadline) once
	// the start phase completes, so the loop must NOT inherit either or its
	// drain ticks would die with the deadline. Keep ctx's values but drop
	// fx's cancellation+deadline; Stop() cancels loopCtx for shutdown. Mirrors
	// internal/reactivacion/app/envio_worker.go:88-92.
	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w.cancel = cancel
	w.done = make(chan struct{})
	w.running = true
	go w.loop(loopCtx)
	return nil
}

// Stop signals the background goroutine to exit and waits for it to drain.
// Idempotent. Satisfies the lifecycle.Hooks interface.
func (w *ReenvioWorker) Stop(ctx context.Context) error {
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
func (w *ReenvioWorker) loop(ctx context.Context) {
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

// tick drains one batch of the mailbox. A failure is logged and swallowed —
// a repository outage must not kill the worker; the next tick simply tries
// again.
func (w *ReenvioWorker) tick(ctx context.Context) {
	w.logger.InfoContext(ctx, "canal_reenvio_worker.tick_start", slog.Int("batch", w.cfg.Batch))
	result, err := w.svc.DrenarCola(ctx, w.cfg.Batch)
	if err != nil {
		w.logger.ErrorContext(ctx, "canal_reenvio_worker.tick_failed", slog.String("error", err.Error()))
		return
	}
	w.logger.InfoContext(
		ctx, "canal_reenvio_worker.tick_done",
		slog.Int("reenviados", result.Reenviados),
		slog.Int("fallidos", result.Fallidos),
		slog.Int("saltados", result.Saltados),
		slog.Int("diferidos", result.Diferidos),
	)
}
