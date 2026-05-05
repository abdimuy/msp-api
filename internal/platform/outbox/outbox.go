// Package outbox implements the transactional outbox pattern.
//
// Every state change that needs to be propagated externally (e.g. a push to
// Microsip) is recorded in the outbox_events table inside the same database
// transaction as the business write. A separate dispatcher reads the table
// and processes pending events with at-least-once semantics, ensuring the
// external side never diverges from our DB.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/transaction"
)

// Event is a single row in outbox_events, ready to be enqueued.
//
// Aggregate is the kind ("cliente", "venta_local", "pago"); AggregateID is
// the primary key of that entity. EventType ("push_to_microsip",
// "mark_synced", ...) routes the event to a Handler.
type Event struct {
	ID          uuid.UUID
	Aggregate   string
	AggregateID uuid.UUID
	EventType   string
	Payload     json.RawMessage
	CreatedAt   time.Time
	ProcessedAt *time.Time
	FailedAt    *time.Time
	Error       *string
	Attempts    int
}

// Enqueue inserts a new pending event. Must run inside an active transaction —
// otherwise the dual-write guarantee is lost.
//
// All values are generated in Go (no DB defaults; see CLAUDE.md).
func Enqueue(ctx context.Context, aggregate string, aggregateID uuid.UUID, eventType string, payload any) error {
	tx, err := transaction.RequireTx(ctx)
	if err != nil {
		return fmt.Errorf("outbox: enqueue requires an active transaction: %w", err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("outbox: marshal payload: %w", err)
	}
	const q = `
		INSERT INTO outbox_events (id, aggregate, aggregate_id, event_type, payload, created_at, attempts)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	if _, err := tx.Exec(ctx, q, uuid.New(), aggregate, aggregateID, eventType, body, time.Now(), 0); err != nil {
		return fmt.Errorf("outbox: insert: %w", err)
	}
	return nil
}

// Handler processes a single event. Implementations must be idempotent — the
// dispatcher may invoke Handle more than once for the same event.
type Handler interface {
	// EventType is the value that routes events to this handler.
	EventType() string
	// Handle processes one event. Return ErrTransient to schedule a retry;
	// any other non-nil error marks the event as permanently failed.
	Handle(ctx context.Context, e Event) error
}

// ErrTransient signals a temporary failure; the dispatcher will retry the event.
var ErrTransient = errors.New("outbox: transient failure")

// HandlerRegistry maps event types to handlers.
type HandlerRegistry struct{ handlers map[string]Handler }

// NewHandlerRegistry returns an empty registry.
func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{handlers: map[string]Handler{}}
}

// Register adds a handler. Panics if the same event type is registered twice
// — duplicate registration is a programming error and we want to fail loudly
// at startup.
func (r *HandlerRegistry) Register(h Handler) {
	if _, ok := r.handlers[h.EventType()]; ok {
		panic(fmt.Sprintf("outbox: duplicate handler for %q", h.EventType()))
	}
	r.handlers[h.EventType()] = h
}

// Lookup returns the handler for the given event type, or nil.
func (r *HandlerRegistry) Lookup(eventType string) Handler {
	return r.handlers[eventType]
}
