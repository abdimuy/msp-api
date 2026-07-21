//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// defaultDrenarBatch is the number of pendientes DrenarCola processes per
// worker tick when EnvioWorkerConfig.Batch is unset.
const defaultDrenarBatch = 50

// EnvioWorkerConfig tunes the worker's cadence and batch size. Zero values
// fall back to sensible defaults.
type EnvioWorkerConfig struct {
	// Interval is how often the worker wakes to drain the queue. Default 1m.
	Interval time.Duration
	// Batch is the max pendientes drained per tick. Default 50.
	Batch int
}

func (c *EnvioWorkerConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = time.Minute
	}
	if c.Batch <= 0 {
		c.Batch = defaultDrenarBatch
	}
}

// EnvioWorker runs a background goroutine that calls Service.DrenarCola on a
// regular ticker, respecting the service's auto_send flag: when auto_send is
// off, the tick is a no-op (DrenarCola itself already returns every pendiente
// as Saltados, so calling it is harmless — but skipping the call avoids
// needless DB round-trips).
//
// It satisfies lifecycle.Hooks (Start/Stop) and is registered with
// lifecycle.Append by cmd/api/reactivacion_wiring.go.
type EnvioWorker struct {
	svc      *Service
	clock    outbound.Clock
	cfg      EnvioWorkerConfig
	autoSend bool
	logger   *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewEnvioWorker builds a worker. cfg zero values are replaced with defaults.
// autoSend mirrors the Service's own auto_send flag — passed explicitly
// (rather than read off svc) so the worker's gating decision is visible
// without reaching into Service internals from another file.
func NewEnvioWorker(
	svc *Service,
	clock outbound.Clock,
	cfg EnvioWorkerConfig,
	autoSend bool,
	logger *slog.Logger,
) *EnvioWorker {
	cfg.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &EnvioWorker{
		svc:      svc,
		clock:    clock,
		cfg:      cfg,
		autoSend: autoSend,
		logger:   logger,
	}
}

// Start launches the background loop goroutine. Idempotent: a second Start
// while already running is a no-op. Satisfies the lifecycle.Hooks interface.
func (w *EnvioWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return nil
	}
	// fx cancels the OnStart ctx (and applies its StartTimeout deadline) once
	// the start phase completes, so the loop must NOT inherit either or its
	// drain ticks would die with the deadline. Keep ctx's values but drop fx's
	// cancellation+deadline; Stop() cancels loopCtx for shutdown.
	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w.cancel = cancel
	w.done = make(chan struct{})
	w.running = true
	go w.loop(loopCtx)
	return nil
}

// Stop signals the background goroutine to exit and waits for it to drain.
// Idempotent. Satisfies the lifecycle.Hooks interface.
func (w *EnvioWorker) Stop(ctx context.Context) error {
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
func (w *EnvioWorker) loop(ctx context.Context) {
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

// tick drains one batch of the queue when auto_send is on. auto_send off is
// a deliberate no-op — the mensajes stay encolado for the Fase 3 approval
// action.
func (w *EnvioWorker) tick(ctx context.Context) {
	if !w.autoSend {
		return
	}

	w.logger.InfoContext(ctx, "reactivacion_envio_worker.tick_start", slog.Int("batch", w.cfg.Batch))
	result, err := w.svc.DrenarCola(ctx, w.cfg.Batch)
	if err != nil {
		w.logger.ErrorContext(ctx, "reactivacion_envio_worker.tick_failed", slog.String("error", err.Error()))
		return
	}
	w.logger.InfoContext(
		ctx, "reactivacion_envio_worker.tick_done",
		slog.Int("enviados", result.Enviados),
		slog.Int("fallidos", result.Fallidos),
		slog.Int("bloqueados", result.Bloqueados),
		slog.Int("saltados", result.Saltados),
	)
}
