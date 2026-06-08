package outbound

import (
	"context"

	"github.com/google/uuid"
)

// OutboxEnqueuer hands a domain event to the platform outbox for at-least-
// once delivery to downstream consumers. The inventario module enqueues events
// such as "traspaso.creado" so other modules (e.g. analytics, audit log) can
// react asynchronously.
//
// The Enqueue contract is intentionally best-effort: callers proceed even
// when enqueueing fails — the failure is logged with the payload so it can
// be replayed manually, and the database transaction that owns the business
// write is never blocked by an outbox hiccup. See the platform outbox docs
// for the full strategy.
type OutboxEnqueuer interface {
	Enqueue(
		ctx context.Context,
		aggregate string,
		aggregateID uuid.UUID,
		eventType string,
		payload any,
	) error
}
