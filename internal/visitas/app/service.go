// Package app contains the visitas module's command service. It depends only
// on the visitas domain, the module's outbound ports, and the standard
// library. Wiring (database pool, http handlers) lives in infra; cross-module
// surfaces live in the visitas root package.
package app

import (
	"context"

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
