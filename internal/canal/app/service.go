// Package app implements the canal module's operations: receiving one
// inbound WhatsApp message into the durable mailbox, forwarding mailboxed
// messages on to the store's on-premise server, and sending one outbound
// WhatsApp message on the store's behalf.
//
// canal is a sealed module (ADR-0009): this package imports only the
// standard library, the failsafe-go retry/circuit-breaker packages that
// internal/platform/reliability builds policies for, internal/canal/domain,
// internal/canal/ports/outbound, internal/platform/reliability, and
// internal/platform/whatsapp (for EnviarSaliente's error classification
// against the Sender port's underlying error taxonomy — see saliente.go) —
// never another module's package. See docs/adr/0010 for why the module
// exists: the VPS answers Meta's webhook with 200 before the store's
// on-premise server has necessarily seen the message, so a durable mailbox
// plus a retrying forwarder is the whole point; ADR-0010 §5 additionally
// frames the VPS as the edge for outbound traffic too, which is what
// EnviarSaliente is for.
package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/retrypolicy"

	"github.com/abdimuy/msp-api/internal/canal/domain"
	"github.com/abdimuy/msp-api/internal/canal/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/reliability"
)

// errCircuitoAbierto is returned internally by intentarReenvio when the
// circuit breaker was already open (or opened mid-attempt) before the
// forwarder was ever called. It never reaches MarcarFallido: a message
// skipped this way is left in EstadoReenvioPendiente for a later pass,
// rather than being recorded as failed for a reason that is not its own.
var errCircuitoAbierto = errors.New("canal: circuito abierto, el forwarder no fue invocado")

// maxMotivoRunes bounds the failure reason passed to
// domain.MensajeEntrante.MarcarFallido. It is kept comfortably under
// domain's own (unexported) 500-codepoint limit: a forwarder error message
// that happened to land right at that limit, plus whatever context wrapping
// added, could otherwise make MarcarFallido itself reject the transition —
// turning a forwarding failure into a swallowed one. See legibleMotivo.
const maxMotivoRunes = 480

// TxRunner abstracts the mailbox's transaction manager so tests can inject a
// no-op runner that executes fn synchronously without a real database
// connection. Declared locally, mirroring
// internal/reactivacion/app/service.go:25, because app/ may not import
// infra even to name its transaction manager type — Task 5's canalsqlite
// transaction manager satisfies this interface implicitly.
type TxRunner interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// ReenvioConfig tunes the retry-with-backoff and circuit-breaker behaviour
// DrenarCola uses when forwarding to the store. Zero values fall back to
// reliability.DefaultRetry / reliability.DefaultCircuit.
type ReenvioConfig struct {
	Retry   reliability.RetryConfig
	Circuit reliability.CircuitConfig
}

// canalCircuitDelay overrides reliability.DefaultCircuit's 30s Delay when
// ReenvioConfig.Circuit is left zero-valued. ReenvioWorker's own tick
// cadence (30s in production — defaultReenvioInterval) is already what
// rate-limits how often the module actually touches the store; the
// breaker's Delay only controls how soon AFTER opening a probe becomes
// eligible, and the worker still will not attempt anything until its next
// tick regardless. So a Delay shorter than the tick interval costs nothing
// extra in production — probes stay bounded to once per tick either way —
// while the platform default's 30s sits right at that interval and can
// silently double a real outage's forward latency once it does recover,
// for no benefit. A short Delay does mean more frequent probes while the
// store is briefly ticking faster than 30s (worker warm-up, or a test), but
// a failed probe against an unreachable store is a fast, cheap no-op — the
// mailbox's whole design already assumes that failure is the routine case
// (ADR-0010).
const canalCircuitDelay = 200 * time.Millisecond

func (c *ReenvioConfig) applyDefaults() {
	if c.Retry.MaxAttempts <= 0 {
		c.Retry = reliability.DefaultRetry()
	}
	if c.Circuit.FailureThreshold == 0 {
		c.Circuit = reliability.DefaultCircuit()
		c.Circuit.Delay = canalCircuitDelay
	}
}

// forwardOutcome is the result type the retry/circuit executor operates
// over. Only whether an attempt succeeded matters — the real outcome
// (nil, a transient error, or a permanent error) is captured out of band in
// intentarReenvio via closure, because the policy's own machinery would
// otherwise obscure it (see intentarReenvio's doc comment).
type forwardOutcome struct{}

// Service is the canal module's application surface: RecibirMensaje handles
// the inbound webhook path, DrenarCola handles the forwarding worker's
// tick, EnviarSaliente (saliente.go) sends one outbound message, and
// ContarPendientes answers the mailbox backlog for GET /canal/v1/salud.
// Everything Service needs from the outside world goes through the outbound
// ports plus the failsafe-go policies reliability builds.
type Service struct {
	repo      outbound.BuzonRepo
	forwarder outbound.Forwarder
	sender    outbound.Sender
	clock     outbound.Clock
	txMgr     TxRunner
	logger    *slog.Logger

	retry   retrypolicy.RetryPolicy[forwardOutcome]
	circuit circuitbreaker.CircuitBreaker[forwardOutcome]
}

// NewService wires a Service against its ports. txMgr may be nil in tests
// that use in-memory fakes — runInTx handles nil gracefully (calls fn
// directly without a real transaction). logger may be nil; slog.Default()
// is used instead. sender must not be nil if EnviarSaliente is ever called
// — unlike txMgr, there is no nil-safe fallback for it.
func NewService(
	repo outbound.BuzonRepo,
	forwarder outbound.Forwarder,
	sender outbound.Sender,
	clock outbound.Clock,
	txMgr TxRunner,
	cfg ReenvioConfig,
	logger *slog.Logger,
) *Service {
	cfg.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		repo:      repo,
		forwarder: forwarder,
		sender:    sender,
		clock:     clock,
		txMgr:     txMgr,
		logger:    logger,
		retry:     reliability.NewRetry[forwardOutcome](cfg.Retry),
		circuit:   reliability.NewCircuit[forwardOutcome](cfg.Circuit),
	}
}

// ContarPendientes reports how many entrantes are waiting to be forwarded
// to the store's on-premise server — feeds GET /canal/v1/salud (Task 6).
func (s *Service) ContarPendientes(ctx context.Context) (int, error) {
	return s.repo.ContarPendientes(ctx)
}

// runInTx executes fn inside a transaction. When txMgr is nil (e.g. in tests
// using in-memory fakes), fn is invoked directly without a real transaction.
func (s *Service) runInTx(ctx context.Context, fn func(context.Context) error) error {
	if s.txMgr == nil {
		return fn(ctx)
	}
	return s.txMgr.RunInTx(ctx, fn)
}

// RecibirMensaje validates and persists one inbound WhatsApp message,
// reporting only whether it was newly inserted — not the entity.
//
// That is a deliberate signature, not a shortcut: the caller (the webhook
// handler) only needs to know whether to treat this as new, so it can
// answer Meta 200 either way. Nothing on the VPS renders a mailbox row —
// the bandeja lives on the store's on-premise server, this module is only
// the durable relay to it. Returning the entity used to be a trap: on a
// redelivered webhook the entity this method builds is a fresh,
// never-persisted one — a random new ID, Estado always Pendiente,
// MotivoFallo always empty — none of which describe the row that actually
// exists. outbound.BuzonRepo has no lookup-by-Wamid method to fetch the
// real one, deliberately: nothing in this module needs it, and adding one
// would be speculative surface on a sealed module whose whole point is
// staying small and extractable. Dropping the entity from the return type
// closes the trap outright rather than requiring every future caller to
// remember not to trust it on the duplicate path.
//
// Idempotent by Wamid: when repo.Guardar reports the row already existed
// (Meta redelivered a webhook already in the mailbox), inserted is false,
// nothing new is persisted, and no error is returned — Meta's
// at-least-once delivery is safe to retry against either way. The freshly
// built (but never persisted) entity's buffered
// canal.mensaje_entrante_recibido event goes nowhere on this path: it is
// never returned and canal has no event sink to drain it into today. If a
// future task adds one, draining it must still be gated on inserted —
// only a message that is actually new should ever surface as an event.
func (s *Service) RecibirMensaje(ctx context.Context, p domain.NewMensajeEntranteParams) (bool, error) {
	m, err := domain.NewMensajeEntrante(p)
	if err != nil {
		return false, err
	}

	var inserted bool
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		var txErr error
		inserted, txErr = s.repo.Guardar(ctx, m)
		return txErr
	}); err != nil {
		return false, err
	}

	if !inserted {
		s.logger.DebugContext(ctx, "canal.mensaje_duplicado", slog.String("wamid", m.Wamid()))
	}
	return inserted, nil
}

// DrenarResultado summarises one DrenarCola pass, for the worker's log line
// and for tests.
type DrenarResultado struct {
	// Reenviados is the number of mensajes the forwarder accepted this pass.
	Reenviados int
	// Fallidos is the number of mensajes moved to EstadoReenvioFallido this
	// pass — a PERMANENT forwarder error only (e.g. the store rejected the
	// message outright). This message will never be accepted; retrying is
	// pointless, which is exactly what EstadoReenvioFallido records.
	Fallidos int
	// Saltados is the number of pendientes left untouched because the
	// circuit breaker was already open when they were reached — the store
	// looks consistently down, so the pass stops spending attempts on it
	// and leaves them EstadoReenvioPendiente for a later pass.
	Saltados int
	// Diferidos is the number of pendientes left untouched because the
	// forwarder failed TRANSIENTLY, including a transient error whose
	// retries were exhausted this pass. A transient failure is not a
	// failure — it is "not yet": the store being unreachable is the
	// ROUTINE case (ADR-0010; the tunnel rotates hourly and is started by
	// hand), and the mailbox exists precisely so a customer's reply
	// survives that window rather than being lost. The message stays
	// EstadoReenvioPendiente and the next drain cycle picks it up again;
	// the circuit breaker (Saltados) is what keeps repeated transient
	// failures from hammering an already-down store.
	Diferidos int
}

// DrenarCola forwards up to limite pendientes to the store's on-premise
// server, in the order BuzonRepo.ListarPendientes returns them.
//
// Each pendiente is attempted through intentarReenvio (retry-with-backoff
// for transient failures, no retry for permanent ones, both bounded by the
// shared circuit breaker), and the outcome is classified into exactly one
// of four buckets — see DrenarResultado's field docs for the reasoning
// behind each:
//   - forwarder success            → EstadoReenvioReenviado (Reenviados)
//   - PERMANENT forwarder failure  → EstadoReenvioFallido   (Fallidos)
//   - TRANSIENT forwarder failure  → untouched, still Pendiente (Diferidos)
//   - circuit already open         → untouched, still Pendiente (Saltados)
//
// The permanent/transient distinction is domain.IsTransient — the same one
// intentarReenvio already uses against the retry policy. Getting this
// wrong in either direction has a real cost, and the costs are not
// symmetric: a transient failure wrongly marked Fallido silently drops a
// customer's reply forever (nothing in this module ever moves
// EstadoReenvioFallido back to Pendiente); a permanent failure wrongly
// left Pendiente just parks one message an operator can see and act on via
// GET /canal/v1/salud. When in doubt, this code errs toward Diferidos.
//
// A ListarPendientes or persistence failure aborts the whole pass and
// returns the error — a message already processed earlier in the pass keeps
// its new state; the caller sees a partial DrenarResultado only via the
// next call once ListarPendientes reflects it.
func (s *Service) DrenarCola(ctx context.Context, limite int) (DrenarResultado, error) {
	var result DrenarResultado

	pendientes, err := s.repo.ListarPendientes(ctx, limite)
	if err != nil {
		return DrenarResultado{}, err
	}

	for _, m := range pendientes {
		if err := s.reenviarUno(ctx, m, &result); err != nil {
			return DrenarResultado{}, err
		}
	}
	return result, nil
}

// reenviarUno attempts to forward one message and persists the outcome,
// mutating result in place. A forwarder success, a PERMANENT forwarder
// rejection, a TRANSIENT forwarder failure, and a circuit-open skip are
// all normal outcomes reflected in result, not errors; only a repository
// failure returns an error.
//
// The transient case deliberately does nothing to m or the repo: the
// message is already EstadoReenvioPendiente and stays there, so the next
// DrenarCola pass retries it. See DrenarResultado.Diferidos for why this
// must not be folded into Fallidos — that was the actual defect Task 11's
// durability composition test caught (a receiver-down window is the
// routine case per ADR-0010, and every message drained during it used to
// be marked Fallido — permanently, since nothing in this module ever
// transitions Fallido back to Pendiente).
func (s *Service) reenviarUno(ctx context.Context, m *domain.MensajeEntrante, result *DrenarResultado) error {
	now := s.clock.Now()

	fwdErr := s.intentarReenvio(ctx, m)
	switch {
	case errors.Is(fwdErr, errCircuitoAbierto):
		s.logger.WarnContext(ctx, "canal.circuito_abierto", slog.String("wamid", m.Wamid()))
		result.Saltados++
		return nil
	case fwdErr == nil:
		return s.marcarReenviado(ctx, m, now, result)
	case domain.IsTransient(fwdErr):
		s.logger.WarnContext(
			ctx, "canal.reenvio_diferido",
			slog.String("wamid", m.Wamid()), slog.String("motivo", fwdErr.Error()),
		)
		result.Diferidos++
		return nil
	default:
		return s.marcarFallido(ctx, m, fwdErr, now, result)
	}
}

// marcarReenviado transitions m to EstadoReenvioReenviado and persists that
// transition, incrementing result.Reenviados on success.
func (s *Service) marcarReenviado(
	ctx context.Context, m *domain.MensajeEntrante, now time.Time, result *DrenarResultado,
) error {
	if err := m.MarcarReenviado(now); err != nil {
		// Unreachable in practice: ListarPendientes only returns
		// EstadoReenvioPendiente rows, so the transition always holds.
		// Surfaced instead of swallowed in case that invariant ever breaks.
		return err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.repo.MarcarReenviado(ctx, m.ID(), now)
	}); err != nil {
		return err
	}
	result.Reenviados++
	return nil
}

// marcarFallido transitions m to EstadoReenvioFallido recording fwdErr's
// (bounded) message as the failure reason, and persists that transition,
// incrementing result.Fallidos on success.
func (s *Service) marcarFallido(
	ctx context.Context, m *domain.MensajeEntrante, fwdErr error, now time.Time, result *DrenarResultado,
) error {
	motivo := legibleMotivo(fwdErr)
	if err := m.MarcarFallido(motivo, now); err != nil {
		// Unreachable in practice, for the same reason as marcarReenviado.
		return err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.repo.MarcarFallido(ctx, m.ID(), motivo, now)
	}); err != nil {
		return err
	}
	s.logger.WarnContext(ctx, "canal.reenvio_fallido", slog.String("wamid", m.Wamid()), slog.String("motivo", motivo))
	result.Fallidos++
	return nil
}

// intentarReenvio attempts to forward m, retrying transient failures with
// backoff (reliability.NewRetry) and short-circuiting through the shared
// circuit breaker (reliability.NewCircuit) when the store looks
// consistently down. It returns:
//   - nil on success;
//   - errCircuitoAbierto if the breaker rejected the attempt before the
//     forwarder was ever called for m;
//   - the forwarder's own error otherwise — the last one observed, whether
//     that is a permanent failure (no retry spent) or a transient one whose
//     retries were exhausted.
//
// It deliberately does NOT pre-check s.circuit.IsOpen() before calling
// failsafe.With(...).Get(...) — an earlier version of this method did, and
// it was a real bug, not a harmless fast path: failsafe-go's circuit
// breaker only advances past OpenState's Delay inside
// TryAcquirePermit(), which its own executor calls when an execution is
// actually attempted through Get()/Run(). openState.State() itself always
// reports OpenState unconditionally, with no regard for elapsed time —
// querying it never advances anything. So a pre-check that returns early
// on IsOpen() == true, without ever calling the executor again, leaves the
// breaker stuck open forever: Delay becomes meaningless once nothing is
// left to observe it elapsing. Calling Get() unconditionally
// lets failsafe's own executor decide — attempted stays false exactly when
// it rejected the permit, which is what errCircuitoAbierto below reports.
//
// The real outcome is captured via the attempted/lastErr closure variables
// rather than read off the executor's own return value on purpose: when
// retries are exhausted, failsafe's retrypolicy returns a
// retrypolicy.ExceededError whose Error() renders as
// "retries exceeded. last result: {}, last error: <cause>" — technically
// legible, but a worse read for the human who later reads MotivoFallo than
// the underlying forwarder error alone, and it does not render at all when
// the failure was permanent (the policy never treats that attempt as a
// retry-triggering failure, so ExceededError is never produced for it).
//
// A permanent forwarder error is reported to the retry policy as a success
// (nil, nil) so it stops immediately without spending another attempt —
// only the closure remembers that the underlying call actually failed. The
// same (nil, nil) also keeps it from counting as a circuit-breaker
// failure: a permanent error (say, a business-level 400) says nothing
// about whether the store is reachable, so it must not push the breaker
// toward opening and blocking otherwise-healthy traffic. Only a transient
// error — the kind that means the store itself is having trouble — is
// ever reported to the circuit as a failure.
func (s *Service) intentarReenvio(ctx context.Context, m *domain.MensajeEntrante) error {
	var attempted bool
	var lastErr error
	_, _ = failsafe.With[forwardOutcome](s.retry, s.circuit).Get(func() (forwardOutcome, error) {
		attempted = true
		err := s.forwarder.Reenviar(ctx, m)
		lastErr = err
		if err != nil && !domain.IsTransient(err) {
			return forwardOutcome{}, nil
		}
		return forwardOutcome{}, err
	})

	if !attempted {
		return errCircuitoAbierto
	}
	return lastErr
}

// legibleMotivo renders err's message bounded to maxMotivoRunes runes, so it
// never collides with domain's own (unexported, stricter) length limit on
// MarcarFallido's motivo — a forwarder error message that happened to
// exceed that limit would otherwise make MarcarFallido reject the
// transition, silently losing the original failure reason instead of
// recording a truncated-but-legible one.
func legibleMotivo(err error) string {
	msg := err.Error()
	runes := []rune(msg)
	if len(runes) <= maxMotivoRunes {
		return msg
	}
	return string(runes[:maxMotivoRunes]) + "…"
}
