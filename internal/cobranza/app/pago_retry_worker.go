//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// PagoRetryWorkerConfig tunes the retry worker's cadence and exponential
// backoff. Defaults are sensible for production; tests override with shorter
// intervals.
type PagoRetryWorkerConfig struct {
	// Interval is how often the worker scans for pendientes. Default 60s.
	Interval time.Duration
	// MaxIntentos caps the per-row retry count before the worker stops
	// picking up a row (still observable via the admin endpoint). Default 10.
	MaxIntentos int
	// BackoffBase is the base of the exponential backoff: a row with N
	// previous intentos is eligible only after BackoffBase * 2^N. Default
	// 30s, cap 30min.
	BackoffBase time.Duration
	// BackoffCap caps the backoff (otherwise 2^10 * 30s would be days).
	// Default 30min.
	BackoffCap time.Duration
	// BatchLimit caps rows processed per tick. Default 100 (one minute
	// drains ~6000/hr — more than enough for cobranza throughput).
	BatchLimit int
}

// applyConfigDefaults fills zero values with defaults.
func (c *PagoRetryWorkerConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 60 * time.Second
	}
	if c.MaxIntentos <= 0 {
		c.MaxIntentos = 10
	}
	if c.BackoffBase <= 0 {
		c.BackoffBase = 30 * time.Second
	}
	if c.BackoffCap <= 0 {
		c.BackoffCap = 30 * time.Minute
	}
	if c.BatchLimit <= 0 {
		c.BatchLimit = 100
	}
}

// PagoRetryWorker drains the MSP_PAGOS_RECIBIDOS outbox by calling
// Service.AplicarPago on each pendiente. It runs as a background goroutine
// kicked off by Start; Stop signals shutdown and waits for the current tick
// to complete.
type PagoRetryWorker struct {
	svc    *Service
	repo   outbound.PagosRecibidosRepo
	clock  outbound.Clock
	cfg    PagoRetryWorkerConfig
	logger *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewPagoRetryWorker builds a worker. svc must already be constructed with
// the same repo/clock — the worker reuses Service.AplicarPago for the actual
// state machine logic.
func NewPagoRetryWorker(
	svc *Service,
	repo outbound.PagosRecibidosRepo,
	clock outbound.Clock,
	cfg PagoRetryWorkerConfig,
	logger *slog.Logger,
) *PagoRetryWorker {
	cfg.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &PagoRetryWorker{
		svc:    svc,
		repo:   repo,
		clock:  clock,
		cfg:    cfg,
		logger: logger,
	}
}

// Start spins up the worker goroutine. Idempotent: a second Start while
// already running is a no-op. Returns the context passed in unchanged so it
// satisfies the lifecycle.Hooks contract used elsewhere in the project.
func (w *PagoRetryWorker) Start(ctx context.Context) error {
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

// Stop signals the worker to stop and waits up to 30s for the current tick
// to drain. Idempotent.
func (w *PagoRetryWorker) Stop(ctx context.Context) error {
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

func (w *PagoRetryWorker) loop(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	// First tick immediately so cold-start drains existing pendientes.
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

// tick runs one drain pass: lists pendientes, filters by backoff eligibility,
// applies each one. Errors are logged; the worker continues with the next
// row so a poison-pill pago doesn't stall the queue.
func (w *PagoRetryWorker) tick(ctx context.Context) {
	pendientes, err := w.repo.ListPendientes(ctx, w.cfg.MaxIntentos, w.cfg.BatchLimit)
	if err != nil {
		w.logger.ErrorContext(ctx, "pago_retry.list_pendientes_failed", slog.String("error", err.Error()))
		return
	}
	if len(pendientes) == 0 {
		return
	}
	now := w.clock.Now()
	var applied, skipped, failed int
	for _, p := range pendientes {
		if !w.elegible(p, now) {
			skipped++
			continue
		}
		if _, applyErr := w.svc.AplicarPago(ctx, p.ID(), uuid.Nil); applyErr != nil {
			failed++
			w.logger.WarnContext(ctx, "pago_retry.apply_failed",
				slog.String("pago_id", p.ID().String()),
				slog.Int("intentos", p.Intentos()+1),
				slog.String("error", applyErr.Error()),
			)
			continue
		}
		applied++
	}
	w.logger.InfoContext(ctx, "pago_retry.tick",
		slog.Int("scanned", len(pendientes)),
		slog.Int("applied", applied),
		slog.Int("skipped", skipped),
		slog.Int("failed", failed),
	)
}

// elegible reports whether `p` is past its backoff window and eligible for
// another apply attempt. A row with N previous intentos must wait at least
// BackoffBase * 2^N (capped at BackoffCap) past its last attempt before
// being retried. Uses ReceivedAt as the reference for intentos==0 and the
// Audit.UpdatedAt for subsequent ones (RegistrarFallo bumps audit).
func (w *PagoRetryWorker) elegible(p *domain.PagoRecibido, now time.Time) bool {
	if p.Intentos() == 0 {
		// First attempt: process immediately (the fast-path may have failed
		// without registering a real failure if the tx couldn't even start).
		return true
	}
	delay := backoffDelay(p.Intentos(), w.cfg.BackoffBase, w.cfg.BackoffCap)
	aud := p.Audit()
	lastAttempt := aud.UpdatedAt()
	return now.Sub(lastAttempt) >= delay
}

// backoffDelay computes base * 2^(intentos-1), capped at cap. The -1 is
// because intentos==1 → base (first retry happens 1 base after the failure).
// Uses float64 only for the exponent guard so even at intentos=63 we don't
// overflow.
func backoffDelay(intentos int, base, maxDelay time.Duration) time.Duration {
	if intentos <= 0 {
		return 0
	}
	exp := intentos - 1
	// Guard against absurd shifts that would overflow Duration: even 2^62 ns
	// exceeds Duration max. Cap the exponent.
	const maxExp = 30
	if exp > maxExp {
		exp = maxExp
	}
	factor := math.Pow(2, float64(exp))
	d := time.Duration(float64(base) * factor)
	if d > maxDelay {
		return maxDelay
	}
	if d < 0 {
		// Pow overflow safety net.
		return maxDelay
	}
	return d
}

// ErrPagoRetryAborted signals that the worker stopped mid-tick because of
// context cancellation. Kept exported for tests that assert clean shutdown.
var ErrPagoRetryAborted = errors.New("pago_retry_worker: aborted")
