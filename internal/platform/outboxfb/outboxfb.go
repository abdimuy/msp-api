package outboxfb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// errNoTx is returned when Enqueue is called outside an active Firebird tx.
var errNoTx = apperror.NewInternal(
	"outbox_no_tx",
	"no hay transacción activa de firebird para insertar el evento",
)

// errEmptyPayload is returned when Enqueue is called with a nil payload.
var errEmptyPayload = apperror.NewInternal(
	"outbox_empty_payload",
	"el payload del evento no puede ser nulo",
)

// Event is a single row in MSP_OUTBOX_EVENTS, ready to be enqueued.
//
// Aggregate is the kind ("venta", "cliente", "pago"); AggregateID is the
// primary key of that entity. EventType ("push_to_microsip", "mark_synced",
// ...) routes the event to a Handler.
//
// ProcessedAt, FailedAt, LastError and Attempts are populated by the
// dispatcher; callers of Enqueue leave them at their zero values.
type Event struct {
	ID          uuid.UUID
	Aggregate   string
	AggregateID uuid.UUID
	EventType   string
	Payload     json.RawMessage
	CreatedAt   time.Time
	ProcessedAt *time.Time
	FailedAt    *time.Time
	LastError   *string
	Attempts    int
}

// Enqueue inserts a new pending event into MSP_OUTBOX_EVENTS using the active
// Firebird transaction. Returns an apperror with code "outbox_no_tx" if ctx
// has no active tx — the dual-write guarantee is lost otherwise.
//
// q is the fallback Querier (the pool's *sql.DB); the function prefers
// firebird.GetQuerier(ctx, q) so it always runs through the ambient tx when
// one is present. The plan REQUIRES it to be invoked from inside an open tx,
// so the fallback is provided only for symmetry with other repos.
//
// If e.ID is uuid.Nil a fresh UUID is generated. If e.CreatedAt is zero it
// is set to time.Now(). Payload must be non-nil.
func Enqueue(ctx context.Context, q firebird.Querier, e Event) error {
	if !firebird.HasTx(ctx) {
		return errNoTx
	}
	if e.Payload == nil {
		return errEmptyPayload
	}
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}

	querier := firebird.GetQuerier(ctx, q)

	const query = `
		INSERT INTO MSP_OUTBOX_EVENTS
			(ID, AGGREGATE, AGGREGATE_ID, EVENT_TYPE, PAYLOAD, CREATED_AT, ATTEMPTS)
		VALUES
			(?, ?, ?, ?, ?, ?, ?)`

	_, err := querier.ExecContext(
		ctx,
		query,
		e.ID.String(),
		e.Aggregate,
		e.AggregateID.String(),
		e.EventType,
		[]byte(e.Payload),
		firebird.ToWallClock(e.CreatedAt),
		0,
	)
	if err != nil {
		return fmt.Errorf("outboxfb: insert: %w", firebird.MapError(err))
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
var ErrTransient = errors.New("outboxfb: transient failure")

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
		panic(fmt.Sprintf("outboxfb: duplicate handler for %q", h.EventType()))
	}
	r.handlers[h.EventType()] = h
}

// Lookup returns the handler for the given event type, or nil.
func (r *HandlerRegistry) Lookup(eventType string) Handler {
	return r.handlers[eventType]
}
