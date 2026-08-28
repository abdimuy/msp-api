package app

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// intervaloPorDefecto is the snapshot cadence, and it is a cost/resolution
// trade-off with measured numbers on both sides.
//
// Resolution: fifteen minutes is what bounds a change to. The owner's example
// question is "at 14:32 somebody moved Eliseo" — no photograph can answer
// that to the second, but "between 14:30 and 14:45" is more than enough to
// place a change before or after a venta captured at 14:36, which is the
// question actually being asked.
//
// Cost: the pass reads the whole `users` collection, and Firestore bills per
// document read regardless of projection. Measured on the dev project on
// 2026-08-28 the collection holds 62 documents, so:
//
//	every  5 min → 288 passes/day → 17,856 reads/day  (36% of the free tier)
//	every 15 min →  96 passes/day →  5,952 reads/day  (12% of the free tier)
//	every 60 min →  24 passes/day →  1,488 reads/day  (3%)
//
// The free tier is 50,000 document reads a day, and this project has already
// been taken down once by an unpaid MX$23.49 invoice — spending a third of
// that budget on a background poller is not a trade worth making for four
// times the resolution. Fifteen minutes costs ~181k reads a month, about
// MX$2 on the paid plan, and leaves the quota to the app.
const intervaloPorDefecto = 15 * time.Minute

// SnapshotWorkerConfig tunes the worker. Zero values fall back to defaults.
type SnapshotWorkerConfig struct {
	// Intervalo is how often the roster is photographed. Default 15 minutes;
	// see intervaloPorDefecto for the arithmetic behind that number.
	Intervalo time.Duration
	// Habilitado gates the whole worker. When false, Start launches nothing
	// and not a single Firestore read is issued. Set false when Firebase is
	// unconfigured (dev mode) and available as a kill switch if the read
	// budget ever needs to be reclaimed in a hurry.
	Habilitado bool
}

func (c *SnapshotWorkerConfig) aplicarDefaults() {
	if c.Intervalo <= 0 {
		c.Intervalo = intervaloPorDefecto
	}
}

// SnapshotWorker photographs the roster on a ticker.
//
// It is the only writer of the flota tables. Shape and lifecycle deliberately
// mirror clientes.DirectoryReconcileWorker and analytics.RefreshWorker — same
// Start/Stop contract, same warm-up-then-tick loop, same "a failed pass logs
// and the loop survives" rule — so there is one background-worker pattern in
// this codebase rather than three.
type SnapshotWorker struct {
	svc    *Service
	cfg    SnapshotWorkerConfig
	logger *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewSnapshotWorker builds a worker. cfg zero values are replaced by defaults.
func NewSnapshotWorker(svc *Service, cfg SnapshotWorkerConfig, logger *slog.Logger) *SnapshotWorker {
	cfg.aplicarDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &SnapshotWorker{svc: svc, cfg: cfg, logger: logger}
}

// Start launches the background loop. Idempotent; a no-op when the worker is
// disabled. Satisfies lifecycle.Hooks.
func (w *SnapshotWorker) Start(ctx context.Context) error {
	if !w.cfg.Habilitado {
		w.logger.InfoContext(ctx, "flota_snapshot_worker.deshabilitado")
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return nil
	}
	// fx cancels the OnStart ctx once the start phase finishes and applies its
	// StartTimeout to it, so the loop must inherit neither — otherwise the
	// first pass dies with "context deadline exceeded". Keep the values
	// (trace/log) and drop the cancellation; Stop cancels loopCtx explicitly.
	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w.cancel = cancel
	w.done = make(chan struct{})
	w.running = true
	go w.loop(loopCtx)
	return nil
}

// Stop signals the loop to exit and waits for it to drain. Idempotent.
// Satisfies lifecycle.Hooks.
func (w *SnapshotWorker) Stop(ctx context.Context) error {
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

// loop photographs immediately, then on every tick.
//
// The warm-up pass on boot is intentional: after a deploy the first thing we
// want is a fresh photograph, and on a virgin database it is what lays down
// the baseline. It also means a restart shortens no window and lengthens
// none — the window is always "since the last recorded pass", whenever that
// was.
func (w *SnapshotWorker) loop(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.cfg.Intervalo)
	defer ticker.Stop()
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

// tick runs one pass. A failure is logged and swallowed: Firestore being
// unreachable, or the empty-photograph rail firing, must not kill the worker
// — the next pass simply covers a longer window. The failure is still
// visible after the fact as a gap in MSP_FLOTA_FOTOS, which is why nothing is
// written on a failed pass.
func (w *SnapshotWorker) tick(ctx context.Context) {
	inicio := time.Now()
	res, err := w.svc.TomarFoto(ctx)
	if err != nil {
		w.logger.WarnContext(ctx, "flota_snapshot_worker.tick_failed",
			slog.String("error", err.Error()))
		return
	}
	w.logger.InfoContext(ctx, "flota_snapshot_worker.tick_done",
		slog.Bool("linea_base", res.LineaBase),
		slog.Int("usuarios", res.UsuariosObservados),
		slog.Int("altas", res.Altas),
		slog.Int("actualizaciones", res.Actualizaciones),
		slog.Int("bajas", res.Bajas),
		slog.Int("cambios", res.CambiosDetectados),
		slog.Duration("elapsed", time.Since(inicio)))
}
