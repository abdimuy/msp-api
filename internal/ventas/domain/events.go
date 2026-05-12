package domain

import (
	"time"

	"github.com/google/uuid"
)

// Event is the contract every domain event in the ventas module satisfies.
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
	// EventTypeVentaCreada is emitted when a new venta is created.
	EventTypeVentaCreada = "venta.creada"
	// EventTypeVentaCancelada is emitted when a venta is canceled.
	EventTypeVentaCancelada = "venta.cancelada"
	// EventTypeImagenAdjuntada is emitted when an imagen is attached.
	EventTypeImagenAdjuntada = "venta.imagen_adjuntada"
	// EventTypeImagenEliminada is emitted when an imagen is removed.
	EventTypeImagenEliminada = "venta.imagen_eliminada"
)

// VentaCreadaEvent is emitted when a venta is created.
type VentaCreadaEvent struct {
	ventaID    uuid.UUID
	tipoVenta  TipoVenta
	occurredAt time.Time
	createdBy  uuid.UUID
}

// NewVentaCreadaEvent constructs a VentaCreadaEvent.
func NewVentaCreadaEvent(ventaID uuid.UUID, tipoVenta TipoVenta, createdBy uuid.UUID, now time.Time) VentaCreadaEvent {
	return VentaCreadaEvent{ventaID: ventaID, tipoVenta: tipoVenta, occurredAt: now, createdBy: createdBy}
}

// EventType returns the canonical event identifier.
func (e VentaCreadaEvent) EventType() string { return EventTypeVentaCreada }

// AggregateID returns the venta ID.
func (e VentaCreadaEvent) AggregateID() uuid.UUID { return e.ventaID }

// OccurredAt returns the creation timestamp.
func (e VentaCreadaEvent) OccurredAt() time.Time { return e.occurredAt }

// Payload returns the serializable event payload.
func (e VentaCreadaEvent) Payload() map[string]any {
	return map[string]any{
		"venta_id":   e.ventaID.String(),
		"tipo_venta": string(e.tipoVenta),
		"created_by": e.createdBy.String(),
	}
}

// VentaCanceladaEvent is emitted when a venta is canceled.
type VentaCanceladaEvent struct {
	ventaID    uuid.UUID
	canceledBy uuid.UUID
	reason     string
	occurredAt time.Time
}

// NewVentaCanceladaEvent constructs a VentaCanceladaEvent.
func NewVentaCanceladaEvent(ventaID, canceledBy uuid.UUID, reason string, now time.Time) VentaCanceladaEvent {
	return VentaCanceladaEvent{ventaID: ventaID, canceledBy: canceledBy, reason: reason, occurredAt: now}
}

// EventType returns the canonical event identifier.
func (e VentaCanceladaEvent) EventType() string { return EventTypeVentaCancelada }

// AggregateID returns the venta ID.
func (e VentaCanceladaEvent) AggregateID() uuid.UUID { return e.ventaID }

// OccurredAt returns the cancellation timestamp.
func (e VentaCanceladaEvent) OccurredAt() time.Time { return e.occurredAt }

// Payload returns the serializable event payload.
func (e VentaCanceladaEvent) Payload() map[string]any {
	return map[string]any{
		"venta_id":    e.ventaID.String(),
		"canceled_by": e.canceledBy.String(),
		"reason":      e.reason,
	}
}

// ImagenAdjuntadaEvent is emitted when an imagen is attached to a venta.
type ImagenAdjuntadaEvent struct {
	ventaID    uuid.UUID
	imagenID   uuid.UUID
	storageKey string
	mime       string
	sizeBytes  int64
	occurredAt time.Time
}

// NewImagenAdjuntadaEventParams carries the inputs to NewImagenAdjuntadaEvent.
type NewImagenAdjuntadaEventParams struct {
	VentaID    uuid.UUID
	ImagenID   uuid.UUID
	StorageKey string
	Mime       string
	SizeBytes  int64
	Now        time.Time
}

// NewImagenAdjuntadaEvent constructs an ImagenAdjuntadaEvent.
func NewImagenAdjuntadaEvent(p NewImagenAdjuntadaEventParams) ImagenAdjuntadaEvent {
	return ImagenAdjuntadaEvent{
		ventaID:    p.VentaID,
		imagenID:   p.ImagenID,
		storageKey: p.StorageKey,
		mime:       p.Mime,
		sizeBytes:  p.SizeBytes,
		occurredAt: p.Now,
	}
}

// EventType returns the canonical event identifier.
func (e ImagenAdjuntadaEvent) EventType() string { return EventTypeImagenAdjuntada }

// AggregateID returns the venta ID.
func (e ImagenAdjuntadaEvent) AggregateID() uuid.UUID { return e.ventaID }

// OccurredAt returns the attachment timestamp.
func (e ImagenAdjuntadaEvent) OccurredAt() time.Time { return e.occurredAt }

// Payload returns the serializable event payload.
func (e ImagenAdjuntadaEvent) Payload() map[string]any {
	return map[string]any{
		"venta_id":    e.ventaID.String(),
		"imagen_id":   e.imagenID.String(),
		"storage_key": e.storageKey,
		"mime":        e.mime,
		"size_bytes":  e.sizeBytes,
	}
}

// ImagenEliminadaEvent is emitted when an imagen is removed from a venta.
type ImagenEliminadaEvent struct {
	ventaID    uuid.UUID
	imagenID   uuid.UUID
	occurredAt time.Time
}

// NewImagenEliminadaEvent constructs an ImagenEliminadaEvent.
func NewImagenEliminadaEvent(ventaID, imagenID uuid.UUID, now time.Time) ImagenEliminadaEvent {
	return ImagenEliminadaEvent{ventaID: ventaID, imagenID: imagenID, occurredAt: now}
}

// EventType returns the canonical event identifier.
func (e ImagenEliminadaEvent) EventType() string { return EventTypeImagenEliminada }

// AggregateID returns the venta ID.
func (e ImagenEliminadaEvent) AggregateID() uuid.UUID { return e.ventaID }

// OccurredAt returns the removal timestamp.
func (e ImagenEliminadaEvent) OccurredAt() time.Time { return e.occurredAt }

// Payload returns the serializable event payload.
func (e ImagenEliminadaEvent) Payload() map[string]any {
	return map[string]any{
		"venta_id":  e.ventaID.String(),
		"imagen_id": e.imagenID.String(),
	}
}
