// Package authoutbox is the auth module's adapter to the platform outbox.
//
// Per ADR-0008 the enqueuer is now ATOMIC: the outbox row is inserted into
// MSP_OUTBOX_EVENTS inside the same Firebird transaction as the business
// write. firebird.TxManager.RunInTx is re-entrant, so when the caller is
// already inside a transaction the INSERT joins it and the COMMIT covers
// both; when the caller is not inside a transaction the manager opens one
// just for the event row. Errors propagate to the caller so the outer tx
// can roll back — there is no longer any "best-effort log-and-swallow"
// recovery path.
package authoutbox

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
)

// txRunner is the subset of firebird.TxManager the Enqueuer depends on.
// Exposing it as an interface lets tests inject a fake runner that plants a
// real *sql.Tx in ctx via firebird.InjectTx without rebuilding the manager.
type txRunner interface {
	RunInTx(ctx context.Context, fn func(context.Context) error) error
}

// Enqueuer wraps the platform outbox so the auth module can drop events on
// the queue without owning a *firebird.Pool reference directly.
type Enqueuer struct {
	runner   txRunner
	fallback firebird.Querier
}

// NewEnqueuer builds an Enqueuer backed by the given Firebird pool. The
// internal TxManager is re-entrant, so when callers already hold an open
// transaction the event row joins it; otherwise the manager opens its own.
func NewEnqueuer(pool *firebird.Pool) *Enqueuer {
	return &Enqueuer{
		runner:   firebird.NewTxManager(pool.DB),
		fallback: pool.DB,
	}
}

// newEnqueuerWithRunner is the test-only constructor that injects a stub
// transaction runner and a fallback querier. Kept unexported so production
// callers stay on the concrete *firebird.TxManager path.
func newEnqueuerWithRunner(r txRunner, fallback firebird.Querier) *Enqueuer {
	return &Enqueuer{runner: r, fallback: fallback}
}

// Compile-time check: Enqueuer satisfies the outbound port.
var _ outbound.OutboxEnqueuer = (*Enqueuer)(nil)

// Enqueue inserts an event row into MSP_OUTBOX_EVENTS. When ctx already
// carries a Firebird transaction the INSERT joins it; otherwise the runner
// opens a fresh transaction for the event row only. Errors propagate so
// that an outer transaction can roll back atomically with the business
// write — the dual-write guarantee that drove ADR-0008.
func (e *Enqueuer) Enqueue(
	ctx context.Context,
	aggregate string,
	aggregateID uuid.UUID,
	eventType string,
	payload any,
) error {
	body, err := marshalPayload(payload)
	if err != nil {
		return err
	}
	return e.runner.RunInTx(ctx, func(ctx context.Context) error {
		return outboxfb.Enqueue(ctx, e.fallback, outboxfb.Event{
			Aggregate:   aggregate,
			AggregateID: aggregateID,
			EventType:   eventType,
			Payload:     body,
		})
	})
}
