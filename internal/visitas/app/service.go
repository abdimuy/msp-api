// Package app contains the visitas module's command service. It depends only
// on the visitas domain, the module's outbound ports, and the standard
// library. Wiring (database pool, http handlers) lives in infra; cross-module
// surfaces live in the visitas root package.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/platform/eventbus"
	"github.com/abdimuy/msp-api/internal/visitas/ports/outbound"
)

// TxRunner abstracts the Firebird transaction manager so tests can inject a
// fake without importing infra. *firebird.TxManager satisfies it implicitly.
//
// It is declared here rather than in ports/outbound because it is a property
// of how this service composes its own writes, not a capability the module
// asks of the outside world.
type TxRunner interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Service is the visitas module's command surface. Handlers depend on
// *Service; everything Service depends on goes through the outbound ports.
//
// The image-writing dependencies (imagenes, storage, tx) are OPTIONAL and
// nil by default: a Service built without them still registers visits, it
// just refuses a request that carries photos. They are supplied together via
// [WithImagenes] — together, because a half-wired service (a repo but no
// storage) is a state that must not be representable.
type Service struct {
	repo     outbound.VisitasRepo
	clock    outbound.Clock
	imagenes outbound.VisitasImagenesRepo
	storage  outbound.StorageProvider
	tx       TxRunner
	bus      *eventbus.Bus
}

// Option customizes a Service at construction time.
type Option func(*Service)

// WithImagenes wires the three dependencies the photo path needs. All three
// are required together; passing a nil among them leaves the service unable
// to accept photos, which [Service.RegistrarVisitaConImagenes] reports as a
// configuration error rather than a client error.
func WithImagenes(
	imagenes outbound.VisitasImagenesRepo,
	storage outbound.StorageProvider,
	tx TxRunner,
) Option {
	return func(s *Service) {
		s.imagenes = imagenes
		s.storage = storage
		s.tx = tx
	}
}

// WithEventBus wires the push bus the SSE stream reads from. Optional and nil
// by default: a Service without it registers visits exactly the same way, it
// just announces nothing, and the reconciler's other three triggers still pick
// the visit up. Push is a latency optimization, never a correctness
// dependency, and the wiring reflects that.
//
// The bus is used directly rather than behind a port. A port here would have
// exactly one implementation and would not cross a module boundary — the
// repo's own YAGNI rule calls that a defect — and *eventbus.Bus is a pure
// in-process value with no I/O, so tests subscribe to the real bus and assert
// on what actually arrives instead of trusting a hand-written double.
func WithEventBus(bus *eventbus.Bus) Option {
	return func(s *Service) {
		s.bus = bus
	}
}

// NewService builds a Service wired against the given ports.
func NewService(repo outbound.VisitasRepo, clock outbound.Clock, opts ...Option) *Service {
	s := &Service{
		repo:  repo,
		clock: clock,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// runInTx executes fn inside a transaction when a TxRunner is wired, and
// inline otherwise. The inline fallback exists for tests and for the
// no-photos path, which needs no transaction at all.
func (s *Service) runInTx(ctx context.Context, fn func(context.Context) error) error {
	if s.tx == nil {
		return fn(ctx)
	}
	return s.tx.RunInTx(ctx, fn)
}

// anunciarVisitaConfirmada publishes the payload-free wakeup that the visitas
// SSE stream turns into an `event: visitas_confirmadas` for connected phones.
//
// Called ONLY after the write is durable — after the transaction commits on
// the photo path, after Insert returns on the plain one. Announcing from
// inside a transaction would let a rolled-back visita tell the fleet it
// exists.
//
// It is also called on the idempotent-replay paths, and that is deliberate
// rather than sloppy: the case this whole push channel exists for is a phone
// whose upload succeeded but whose RESPONSE was lost. That phone retries, the
// retry resolves to a replay, and if the replay stayed silent the phone would
// wait for the 15-minute periodic tick to learn what the server already knew.
//
// The published payload is an empty []int — the event carries no visit ids
// because it carries no authority. It wakes the reconciler; the reconciler
// confirms through by-ids. Publish is non-blocking and a no-op when no bus is
// wired, so this can never fail a write that already succeeded.
func (s *Service) anunciarVisitaConfirmada() {
	if s.bus == nil {
		return
	}
	s.bus.Publish(eventbusTopicVisitasConfirmadas, []int{})
}

// eventbusTopicVisitasConfirmadas mirrors visitashttp.TopicVisitasConfirmadas.
// The constant is duplicated rather than imported because app must not depend
// on infra (depguard's app-no-infra rule); TestTopicoDePublicacionCoincideConElDelStream
// in the infra package pins the two together so they cannot drift.
const eventbusTopicVisitasConfirmadas = "visitas_confirmadas"
