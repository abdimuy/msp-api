// Package app — narrativa_worker.go runs a serialized background loop that drains
// the MSP_AN_NARRATIVA_PENDIENTE queue: for each queued client it recomputes the
// pulso, generates the narrativa via the LLM, validates it, and caches the result.
//
// When the LLM is disabled (Enabled=false, the default), Start is a no-op: no
// goroutine is launched, no model is called, and the pending queue is left
// untouched. The loop mirrors RefreshWorker lifecycle EXACTLY (idempotent
// Start/Stop, drains on Stop, ticker-driven with no immediate first tick).
//
//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/llm"
)

// pulsoLoader loads a candidate and its computed pulso.
// *Service satisfies this interface via candidatoYPulso.
// The interface is unexported to seal it to this package; the constructor
// accepts it so tests can inject a fake without a fully-wired *Service.
type pulsoLoader interface {
	candidatoYPulso(ctx context.Context, clienteID int) (*domain.WinbackCandidato, analytics.PulsoComputado, error)
}

// NarrativaWorkerConfig tunes the worker's cadence and LLM settings.
// Zero values fall back to sensible production defaults.
type NarrativaWorkerConfig struct {
	// Interval is how often the worker wakes to drain the pending queue. Default 1m.
	Interval time.Duration
	// BatchSize is the maximum number of clients processed per tick. Default 10.
	BatchSize int
	// Model is the LLM model name persisted in NARRATIVA.MODELO.
	Model string
	// Enabled controls whether the worker is active. false (default) ⇒ no-op.
	Enabled bool
}

func (c *NarrativaWorkerConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = time.Minute
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 10
	}
}

// NarrativaWorker runs a background goroutine that drains the narrativa pending
// queue on a regular ticker. It satisfies lifecycle.Hooks (Start/Stop).
//
// When Enabled=false (the default), Start returns nil immediately without
// launching a goroutine — no model is ever called.
type NarrativaWorker struct {
	loader pulsoLoader
	repo   outbound.NarrativaRepo
	gen    outbound.NarrativeGenerator
	clock  outbound.Clock
	cfg    NarrativaWorkerConfig
	logger *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewNarrativaWorker builds a worker. cfg zero values are replaced with defaults.
func NewNarrativaWorker(
	loader pulsoLoader,
	repo outbound.NarrativaRepo,
	gen outbound.NarrativeGenerator,
	clock outbound.Clock,
	cfg NarrativaWorkerConfig,
	logger *slog.Logger,
) *NarrativaWorker {
	cfg.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &NarrativaWorker{
		loader: loader,
		repo:   repo,
		gen:    gen,
		clock:  clock,
		cfg:    cfg,
		logger: logger,
	}
}

// Start launches the background loop goroutine. Idempotent: a second Start
// while already running is a no-op. When Enabled=false, Start returns nil
// without launching a goroutine (log once at info).
// Satisfies the lifecycle.Hooks interface.
func (w *NarrativaWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.cfg.Enabled {
		w.logger.InfoContext(ctx, "narrativa_worker.disabled")
		return nil
	}
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
func (w *NarrativaWorker) Stop(ctx context.Context) error {
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
// No immediate first tick — we wait for the first scheduled interval.
func (w *NarrativaWorker) loop(ctx context.Context) {
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

// tick drains the pending queue serially (one client at a time).
// Errors from ListarPendientes are logged and the tick is skipped.
func (w *NarrativaWorker) tick(ctx context.Context) {
	pend, err := w.repo.ListarPendientes(ctx, w.cfg.BatchSize)
	if err != nil {
		w.logger.ErrorContext(
			ctx, "narrativa_worker.list_pendientes_failed",
			slog.String("error", err.Error()),
		)
		return
	}
	for _, p := range pend {
		w.procesarUno(ctx, p.ClienteID)
	}
}

// procesarUno generates, validates, and caches the narrativa for one client.
// On any error the loop continues; the pending entry is left in the queue
// for a transient error and removed for a permanent error (or missing candidate).
func (w *NarrativaWorker) procesarUno(ctx context.Context, clienteID int) {
	// 1. Load candidate + pulso.
	c, comp, err := w.loader.candidatoYPulso(ctx, clienteID)
	if err != nil {
		appErr, ok := apperror.As(err)
		if ok && appErr.Kind == apperror.KindNotFound {
			// Candidate gone — remove from queue, do not upsert.
			w.logger.InfoContext(
				ctx, "narrativa_worker.candidato_not_found",
				slog.Int("cliente_id", clienteID),
			)
			if berr := w.repo.BorrarPendiente(ctx, clienteID); berr != nil {
				w.logger.WarnContext(
					ctx, "narrativa_worker.borrar_pendiente_failed",
					slog.Int("cliente_id", clienteID),
					slog.String("error", berr.Error()),
				)
			}
			return
		}
		// Other error — leave in queue, retry next tick.
		w.logger.ErrorContext(
			ctx, "narrativa_worker.candidato_pulso_failed",
			slog.Int("cliente_id", clienteID),
			slog.String("error", err.Error()),
		)
		return
	}

	// 2. Compute invalidation key from the current pulso facts.
	hash := NarrativaInputHash(comp)

	// 3. Build the generator's fact-anchored input.
	in := buildNarrativeInput(c, comp, CatalogoRasgos)

	// 4. Generate.
	out, err := w.gen.Generar(ctx, in)
	if err != nil {
		w.logger.ErrorContext(
			ctx, "narrativa_worker.generar_failed",
			slog.Int("cliente_id", clienteID),
			slog.String("error", err.Error()),
		)
		if llm.IsTransient(err) {
			// Transient (network/timeout/429/5xx) — leave in queue, retry next tick.
			return
		}
		// Permanent error (incl. ErrLLMDisabled) — drop from queue without upserting.
		// The read-path re-enqueues on next view if the row is still missing.
		if berr := w.repo.BorrarPendiente(ctx, clienteID); berr != nil {
			w.logger.WarnContext(
				ctx, "narrativa_worker.borrar_pendiente_failed",
				slog.Int("cliente_id", clienteID),
				slog.String("error", berr.Error()),
			)
		}
		return
	}

	// 5. Validate direction + traits.
	v := ValidarNarrativa(out, comp)

	// 6. Build the domain row. Persist exactly what the validator returns:
	//    on passing check v.Texto is the model paragraph and v.Rasgos are the
	//    validated codes; on fallback v.Texto=="" and v.Rasgos==nil (negative
	//    cache keyed by InputHash that prevents pointless re-generation until
	//    the facts change).
	n := domain.Narrativa{
		ClienteID:  clienteID,
		Texto:      v.Texto,
		Rasgos:     v.Rasgos,
		InputHash:  hash,
		Modelo:     w.cfg.Model,
		GeneradaEn: w.clock.Now().UTC(),
	}

	// 7. Persist (always upsert, even the empty fallback row).
	if err := w.repo.UpsertNarrativa(ctx, n); err != nil {
		w.logger.ErrorContext(
			ctx, "narrativa_worker.upsert_failed",
			slog.Int("cliente_id", clienteID),
			slog.String("error", err.Error()),
		)
		// Leave in queue — retry next tick.
		return
	}

	// 8. Remove from pending queue (non-fatal if it fails).
	if err := w.repo.BorrarPendiente(ctx, clienteID); err != nil {
		w.logger.WarnContext(
			ctx, "narrativa_worker.borrar_pendiente_failed",
			slog.Int("cliente_id", clienteID),
			slog.String("error", err.Error()),
		)
	}
}
