// Package app contains the analytics module's command and query services.
// It depends only on the analytics domain, the module's outbound ports, and
// the standard library. Wiring (database pool, HTTP handlers) lives in infra.
//
//nolint:misspell // analytics vocabulary is Spanish per project convention.
package app

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// TxRunner abstracts the Firebird transaction manager so tests can inject a
// no-op runner that executes fn synchronously without a real DB connection.
// *firebird.TxManager satisfies this interface implicitly.
type TxRunner interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Service is the analytics module's query and command surface. All handlers
// depend on *Service; everything Service needs from the outside world goes
// through the outbound ports declared in ports/outbound.
//
// txMgr may be nil in tests — runInTx handles nil gracefully (calls fn
// directly without a real transaction).
//
// N/A — drainEvents / enqueueEvent / outbox:
// RefrescarCandidatos is a read-model refresh; it does not emit business domain
// events. The outbox pattern (doc 05/06) is not applicable for a projection
// recompute (no aggregate state change, no consumer notification required).
type Service struct {
	repo              outbound.WinbackRepo
	micro             outbound.MicrosipReader
	clock             outbound.Clock
	txMgr             TxRunner
	logger            *slog.Logger
	scorecard         Scorecard
	recompraScorecard RecompraScorecard
	btyd              BTYD

	// refreshRunning is the single-flight guard for RefrescarEnSegundoPlano.
	// atomic.Bool is safe for concurrent access without a mutex.
	refreshRunning atomic.Bool
}

// NewService builds a Service wired against the required ports.
// txMgr may be nil in tests that use in-memory fakes for the write side.
// logger may be nil; slog.Default() is used in that case.
//
// The embedded credit scorecard is loaded at construction time. On load failure
// the service starts with a zero Scorecard (Loaded()==false) and degrades
// gracefully: credit scores return "no aplica" everywhere without panicking.
// The embedded JSON is build-tested so this code path is defensive only.
func NewService(
	repo outbound.WinbackRepo,
	micro outbound.MicrosipReader,
	clock outbound.Clock,
	txMgr TxRunner,
) *Service {
	sc, err := LoadScorecard()
	if err != nil {
		slog.Default().Error("analytics.scorecard_load_failed",
			slog.String("error", err.Error()),
		)
		// sc is already the zero Scorecard{}; Loaded() == false → credit scoring
		// degrades to "no aplica" on every call. Never panics.
	}
	rsc, err := LoadRecompraScorecard()
	if err != nil {
		slog.Default().Error("analytics.recompra_scorecard_load_failed",
			slog.String("error", err.Error()),
		)
		// rsc is already zero RecompraScorecard{}; Loaded() == false → recompra scoring
		// degrades to "no aplica" on every call. Never panics.
	}
	btyd, err := LoadBTYD()
	if err != nil {
		slog.Default().Error("analytics.btyd_load_failed",
			slog.String("error", err.Error()),
		)
		// btyd is already zero BTYD{}; Loaded() == false → recompra scoring
		// degrades to "no aplica" on every call. Never panics.
	}
	return &Service{
		repo:              repo,
		micro:             micro,
		clock:             clock,
		txMgr:             txMgr,
		logger:            slog.Default(),
		scorecard:         sc,
		recompraScorecard: rsc,
		btyd:              btyd,
	}
}

// WithLogger sets a custom logger on the service. Used in production wiring to
// inject the module-scoped logger. Returns s for chaining.
func (s *Service) WithLogger(l *slog.Logger) *Service {
	if l != nil {
		s.logger = l
	}
	return s
}

// runInTx executes fn inside a transaction. When txMgr is nil (e.g. in tests
// using in-memory fakes), fn is invoked directly without a real transaction.
func (s *Service) runInTx(ctx context.Context, fn func(context.Context) error) error {
	if s.txMgr == nil {
		return fn(ctx)
	}
	return s.txMgr.RunInTx(ctx, fn)
}

// RefrescarEnSegundoPlano launches RefrescarCandidatos in a detached goroutine
// so the HTTP handler can return 202 immediately without waiting for the full
// rebuild (~50 s for 43k rows).
//
// Single-flight guard: if a refresh is already running, the method returns
// false immediately without starting a second goroutine. The guard uses an
// atomic.Bool so it is safe for concurrent callers without a mutex.
//
// The goroutine runs with context.Background() so that a client disconnect or
// HTTP write-timeout does NOT cancel the work mid-write. The result (procesados,
// watermark) or any error is logged via the service logger.
//
// Note: the scheduled RefreshWorker calls RefrescarCandidatos directly (not
// through this method), so a manual trigger and a scheduled tick can technically
// overlap. This overlap is safe — UpsertCandidatos is idempotent and en_control
// flags are always preserved — but it wastes resources. If overlap becomes a
// concern, route the worker through RefrescarEnSegundoPlano (worker ignores the
// return value) and the guard will prevent double-runs at zero extra cost.
func (s *Service) RefrescarEnSegundoPlano(full bool) bool {
	if !s.refreshRunning.CompareAndSwap(false, true) {
		// Another refresh is already in progress.
		return false
	}

	go func() {
		defer s.refreshRunning.Store(false)

		ctx := context.Background()
		s.logger.InfoContext(ctx, "analytics_refresh.background_start",
			slog.Bool("full", full),
		)

		result, err := s.RefrescarCandidatos(ctx, full)
		if err != nil {
			s.logger.ErrorContext(ctx, "analytics_refresh.background_failed",
				slog.Bool("full", full),
				slog.String("error", err.Error()),
			)
			return
		}

		s.logger.InfoContext(ctx, "analytics_refresh.background_done",
			slog.Bool("full", full),
			slog.Int("procesados", result.Procesados),
			slog.Time("watermark", result.Watermark),
		)
	}()

	return true
}
