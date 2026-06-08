//nolint:misspell // domain vocabulary is Spanish (traspaso, almacén, etc.) per project convention.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Event is the contract every domain event in the inventario module satisfies.
// Events are emitted by aggregate-root mutators into a buffer that the app
// layer drains after a successful transaction.
type Event interface {
	// EventType returns the canonical snake_case event identifier.
	EventType() string
	// AggregateID returns the ID of the aggregate that produced the event.
	AggregateID() uuid.UUID
	// OccurredAt returns the moment the event was produced.
	OccurredAt() time.Time
	// Payload returns the event-specific data as a serializable map.
	Payload() map[string]any
}

// Event type constants. Stored alongside outbox/messaging payloads.
const (
	// EventTypeTraspasoCreado is emitted when a new traspaso is created.
	EventTypeTraspasoCreado = "traspaso.creado"
	// EventTypeTraspasoReversado is emitted when a traspaso is reversed.
	EventTypeTraspasoReversado = "traspaso.reversado"
)

// TraspasoCreadoEvent is emitted when a traspaso is created.
type TraspasoCreadoEvent struct {
	traspasoID     uuid.UUID
	folio          string
	almacenOrigen  int
	almacenDestino int
	ventaID        *uuid.UUID
	tipoReverso    bool
	detallesCount  int
	occurredAt     time.Time
}

// NewTraspasoCreadoEvent constructs a TraspasoCreadoEvent.
func NewTraspasoCreadoEvent(
	traspasoID uuid.UUID,
	folio string,
	almacenOrigen, almacenDestino int,
	ventaID *uuid.UUID,
	tipoReverso bool,
	detallesCount int,
	now time.Time,
) TraspasoCreadoEvent {
	return TraspasoCreadoEvent{
		traspasoID:     traspasoID,
		folio:          folio,
		almacenOrigen:  almacenOrigen,
		almacenDestino: almacenDestino,
		ventaID:        ventaID,
		tipoReverso:    tipoReverso,
		detallesCount:  detallesCount,
		occurredAt:     now,
	}
}

// EventType returns the canonical event identifier.
func (e TraspasoCreadoEvent) EventType() string { return EventTypeTraspasoCreado }

// AggregateID returns the traspaso ID.
func (e TraspasoCreadoEvent) AggregateID() uuid.UUID { return e.traspasoID }

// OccurredAt returns the creation timestamp.
func (e TraspasoCreadoEvent) OccurredAt() time.Time { return e.occurredAt }

// Payload returns the serializable event payload.
func (e TraspasoCreadoEvent) Payload() map[string]any {
	p := map[string]any{
		"folio":           e.folio,
		"almacen_origen":  e.almacenOrigen,
		"almacen_destino": e.almacenDestino,
		"tipo_reverso":    e.tipoReverso,
		"detalles_count":  e.detallesCount,
	}
	if e.ventaID != nil {
		p["venta_id"] = e.ventaID.String()
	} else {
		p["venta_id"] = nil
	}
	return p
}

// TraspasoReversadoEvent is emitted when a traspaso is reversed.
type TraspasoReversadoEvent struct {
	traspasoID     uuid.UUID
	folio          string
	almacenOrigen  int
	almacenDestino int
	ventaID        *uuid.UUID
	tipoReverso    bool
	detallesCount  int
	occurredAt     time.Time
}

// NewTraspasoReversadoEvent constructs a TraspasoReversadoEvent.
func NewTraspasoReversadoEvent(
	traspasoID uuid.UUID,
	folio string,
	almacenOrigen, almacenDestino int,
	ventaID *uuid.UUID,
	tipoReverso bool,
	detallesCount int,
	now time.Time,
) TraspasoReversadoEvent {
	return TraspasoReversadoEvent{
		traspasoID:     traspasoID,
		folio:          folio,
		almacenOrigen:  almacenOrigen,
		almacenDestino: almacenDestino,
		ventaID:        ventaID,
		tipoReverso:    tipoReverso,
		detallesCount:  detallesCount,
		occurredAt:     now,
	}
}

// EventType returns the canonical event identifier.
func (e TraspasoReversadoEvent) EventType() string { return EventTypeTraspasoReversado }

// AggregateID returns the traspaso ID.
func (e TraspasoReversadoEvent) AggregateID() uuid.UUID { return e.traspasoID }

// OccurredAt returns the reversal timestamp.
func (e TraspasoReversadoEvent) OccurredAt() time.Time { return e.occurredAt }

// Payload returns the serializable event payload.
func (e TraspasoReversadoEvent) Payload() map[string]any {
	p := map[string]any{
		"folio":           e.folio,
		"almacen_origen":  e.almacenOrigen,
		"almacen_destino": e.almacenDestino,
		"tipo_reverso":    e.tipoReverso,
		"detalles_count":  e.detallesCount,
	}
	if e.ventaID != nil {
		p["venta_id"] = e.ventaID.String()
	} else {
		p["venta_id"] = nil
	}
	return p
}
