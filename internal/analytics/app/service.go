// Package app contains the analytics module's command and query services.
// It depends only on the analytics domain, the module's outbound ports, and
// the standard library. Wiring (database pool, HTTP handlers) lives in infra.
//
//nolint:misspell // analytics vocabulary is Spanish per project convention.
package app

import (
	"context"

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
	repo  outbound.WinbackRepo
	micro outbound.MicrosipReader
	clock outbound.Clock
	txMgr TxRunner
}

// NewService builds a Service wired against the required ports.
// txMgr may be nil in tests that use in-memory fakes for the write side.
func NewService(
	repo outbound.WinbackRepo,
	micro outbound.MicrosipReader,
	clock outbound.Clock,
	txMgr TxRunner,
) *Service {
	return &Service{
		repo:  repo,
		micro: micro,
		clock: clock,
		txMgr: txMgr,
	}
}

// runInTx executes fn inside a transaction. When txMgr is nil (e.g. in tests
// using in-memory fakes), fn is invoked directly without a real transaction.
func (s *Service) runInTx(ctx context.Context, fn func(context.Context) error) error {
	if s.txMgr == nil {
		return fn(ctx)
	}
	return s.txMgr.RunInTx(ctx, fn)
}
