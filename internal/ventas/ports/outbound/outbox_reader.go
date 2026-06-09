package outbound

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// VentaEvento is one entry in a venta's event timeline. It is a ventas-owned
// projection of a platform outbox row — the module does not expose the
// dispatcher's delivery internals (attempts, last_error) to operators; only
// what the event WAS and WHEN it happened.
type VentaEvento struct {
	// ID is the outbox event's primary key (stable across reads).
	ID uuid.UUID
	// EventType is the canonical event name, e.g. "venta.aprobada".
	EventType string
	// Payload is the event's JSON body. The HTTP layer surfaces selected
	// fields (actor, folio, ...) to the operator; the rest is available for
	// debugging.
	Payload json.RawMessage
	// OccurredAt is when the event was recorded (the business write's commit
	// time), in UTC.
	OccurredAt time.Time
}

// VentaEventReader returns the chronological event timeline for a venta.
// Implemented in infra over the platform outbox; consumed by the ventas
// query service to power the venta-detail "Historial" view.
type VentaEventReader interface {
	// EventosDeVenta returns every event recorded for the venta, oldest
	// first. Returns an empty slice (not an error) when the venta has no
	// events yet.
	EventosDeVenta(ctx context.Context, ventaID uuid.UUID) ([]VentaEvento, error)
}
