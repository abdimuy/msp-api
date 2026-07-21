// Package app contains the reactivación module's command and query service.
// It depends only on the reactivación domain, the module's outbound ports, and
// the standard library. Wiring (database pool, HTTP handlers) lives in infra.
//
//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// defaultControlPct is the share of the universe assigned to the control group
// when Config.ControlPct is not set. The piloto uses a generous ~50% split
// because the channel (not the universe) is the bottleneck: the control group is
// free, so a large one tightens the attribution estimate.
const defaultControlPct = 50

// TxRunner abstracts the Firebird transaction manager so tests can inject a
// no-op runner that executes fn synchronously without a real DB connection.
// *firebird.TxManager satisfies this interface implicitly.
type TxRunner interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Config carries the tunable knobs of the reactivación service.
type Config struct {
	// ControlPct is the percentage [0,100] of clientes deterministically
	// assigned to the control group. Zero falls back to defaultControlPct.
	ControlPct int
}

// controlPct returns the effective control percentage, applying the default when
// unset. Values are clamped to [0,100] to keep deterministicControl well-defined.
func (c Config) controlPct() int {
	pct := c.ControlPct
	if pct == 0 {
		pct = defaultControlPct
	}
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// Service is the reactivación module's query and command surface. All handlers
// depend on *Service; everything Service needs from the outside world goes
// through the outbound ports declared in ports/outbound.
//
// txMgr may be nil in tests — runInTx handles nil gracefully (calls fn directly
// without a real transaction).
type Service struct {
	reader outbound.UniversoReader
	repo   outbound.CohorteRepo
	clock  outbound.Clock
	txMgr  TxRunner
	cfg    Config
	logger *slog.Logger

	// construirRunning is the single-flight guard for ConstruirEnSegundoPlano.
	construirRunning atomic.Bool

	// ─── Canal (Fase 2) — set via WithCanal; zero-valued in a Fase 1 Service ───

	mensajeRepo outbound.MensajeRepo
	sender      outbound.MessageSender
	opener      Opener
	gobernador  *Gobernador
	autoSend    bool

	// encolarRunning is the single-flight guard for EncolarEnSegundoPlano.
	encolarRunning atomic.Bool

	// ─── Copiloto (Fase 3a) — set via WithCopiloto; zero-valued in a Fase 1/2 Service ───

	convRepo     outbound.ConversacionRepo
	decisionRepo outbound.DecisionRepo
	notaReader   outbound.NotaReader
	copilotoLLM  outbound.CopilotoLLM
	factsReader  outbound.ClienteFactsReader
}

// NewService builds a Service wired against the required ports. txMgr may be nil
// in tests that use in-memory fakes for the write side.
func NewService(
	reader outbound.UniversoReader,
	repo outbound.CohorteRepo,
	clock outbound.Clock,
	txMgr TxRunner,
	cfg Config,
) *Service {
	return &Service{
		reader: reader,
		repo:   repo,
		clock:  clock,
		txMgr:  txMgr,
		cfg:    cfg,
		logger: slog.Default(),
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

// WithCanal wires the Fase 2 channel dependencies onto an existing Service,
// reusing its runInTx/clock/logger/txMgr. A Service built with NewService
// alone (Fase 1) has these fields zero-valued — EncolarCohorte/DrenarCola
// must not be called until WithCanal has run. Returns s for chaining.
func (s *Service) WithCanal(
	mensajeRepo outbound.MensajeRepo,
	sender outbound.MessageSender,
	opener Opener,
	gobernador *Gobernador,
	autoSend bool,
) *Service {
	s.mensajeRepo = mensajeRepo
	s.sender = sender
	s.opener = opener
	s.gobernador = gobernador
	s.autoSend = autoSend
	return s
}

// WithCopiloto wires the Fase 3a copiloto dependencies onto an existing
// Service, reusing its runInTx/clock/logger/txMgr. A Service built without
// this (Fase 1/2) has these fields zero-valued — ProcesarMensajeEntrante,
// AprobarBorrador, EditarYAprobar, Escalar, Dictar, ListarConversaciones, and
// ObtenerConversacion must not be called until WithCopiloto has run.
//
// AprobarBorrador and EditarYAprobar additionally reuse the Fase 2
// mensajeRepo (set via WithCanal) to enqueue the approved/edited draft —
// production wiring must call both WithCanal and WithCopiloto on the same
// Service. Returns s for chaining.
func (s *Service) WithCopiloto(
	convRepo outbound.ConversacionRepo,
	decisionRepo outbound.DecisionRepo,
	notaReader outbound.NotaReader,
	copilotoLLM outbound.CopilotoLLM,
	factsReader outbound.ClienteFactsReader,
) *Service {
	s.convRepo = convRepo
	s.decisionRepo = decisionRepo
	s.notaReader = notaReader
	s.copilotoLLM = copilotoLLM
	s.factsReader = factsReader
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
