//nolint:misspell // ventas vocabulary is Spanish (productos, etc.) per project convention.
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
	// EventTypeVentaHeaderActualizado is emitted when the venta's header
	// fields are edited via ActualizarHeader.
	EventTypeVentaHeaderActualizado = "venta.header_actualizado"
	// EventTypeVentaClienteActualizado is emitted when the venta's cliente
	// snapshot (or cliente_id link) changes via ActualizarCliente.
	EventTypeVentaClienteActualizado = "venta.cliente_actualizado"
	// EventTypeVentaProductosReemplazados is emitted when the productos
	// collection is replaced via ReemplazarProductos.
	EventTypeVentaProductosReemplazados = "venta.productos_reemplazados"
	// EventTypeVentaCombosReemplazados is emitted when the combos
	// collection is replaced via ReemplazarCombos.
	EventTypeVentaCombosReemplazados = "venta.combos_reemplazados"
	// EventTypeVentaVendedoresReemplazados is emitted when the vendedores
	// collection is replaced via ReemplazarVendedores.
	EventTypeVentaVendedoresReemplazados = "venta.vendedores_reemplazados"
	// EventTypeVentaEnviadaARevision is emitted on borrador → revisada.
	EventTypeVentaEnviadaARevision = "venta.enviada_a_revision"
	// EventTypeVentaAprobada is emitted on revisada → aprobada.
	EventTypeVentaAprobada = "venta.aprobada"
	// EventTypeVentaRegresadaABorrador is emitted on revisada → borrador.
	EventTypeVentaRegresadaABorrador = "venta.regresada_a_borrador"
	// EventTypeVentaAplicada is emitted when a venta is materialized in
	// Microsip (sincronización pendiente → aplicada).
	EventTypeVentaAplicada = "venta.aplicada"
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

// ventaEditEvent is the common shape of the five edit events. Each carries
// the venta id, the editing user, and the timestamp; some variants add
// an extra item-count field (productos/combos/vendedores).
type ventaEditEvent struct {
	ventaID    uuid.UUID
	by         uuid.UUID
	occurredAt time.Time
	itemsCount int
	eventType  string
}

// EventType returns the canonical event identifier.
func (e ventaEditEvent) EventType() string { return e.eventType }

// AggregateID returns the venta ID.
func (e ventaEditEvent) AggregateID() uuid.UUID { return e.ventaID }

// OccurredAt returns the moment the event was produced.
func (e ventaEditEvent) OccurredAt() time.Time { return e.occurredAt }

// basePayload is the common shape every edit event serializes.
func (e ventaEditEvent) basePayload() map[string]any {
	return map[string]any{
		"venta_id":   e.ventaID.String(),
		"updated_by": e.by.String(),
	}
}

// VentaHeaderActualizadoEvent is emitted when the venta header is edited.
type VentaHeaderActualizadoEvent struct{ ventaEditEvent }

// NewVentaHeaderActualizadoEvent constructs the event.
func NewVentaHeaderActualizadoEvent(ventaID, by uuid.UUID, now time.Time) VentaHeaderActualizadoEvent {
	return VentaHeaderActualizadoEvent{ventaEditEvent{
		ventaID: ventaID, by: by, occurredAt: now,
		eventType: EventTypeVentaHeaderActualizado,
	}}
}

// Payload returns the serializable event payload.
func (e VentaHeaderActualizadoEvent) Payload() map[string]any { return e.basePayload() }

// VentaClienteActualizadoEvent is emitted when cliente snapshot or
// cliente_id is updated.
type VentaClienteActualizadoEvent struct{ ventaEditEvent }

// NewVentaClienteActualizadoEvent constructs the event.
func NewVentaClienteActualizadoEvent(ventaID, by uuid.UUID, now time.Time) VentaClienteActualizadoEvent {
	return VentaClienteActualizadoEvent{ventaEditEvent{
		ventaID: ventaID, by: by, occurredAt: now,
		eventType: EventTypeVentaClienteActualizado,
	}}
}

// Payload returns the serializable event payload.
func (e VentaClienteActualizadoEvent) Payload() map[string]any { return e.basePayload() }

// VentaProductosReemplazadosEvent is emitted when productos are replaced.
type VentaProductosReemplazadosEvent struct{ ventaEditEvent }

// NewVentaProductosReemplazadosEvent constructs the event.
func NewVentaProductosReemplazadosEvent(ventaID uuid.UUID, count int, by uuid.UUID, now time.Time) VentaProductosReemplazadosEvent {
	return VentaProductosReemplazadosEvent{ventaEditEvent{
		ventaID: ventaID, by: by, occurredAt: now, itemsCount: count,
		eventType: EventTypeVentaProductosReemplazados,
	}}
}

// Payload returns the serializable event payload.
func (e VentaProductosReemplazadosEvent) Payload() map[string]any {
	p := e.basePayload()
	p["productos_count"] = e.itemsCount
	return p
}

// VentaCombosReemplazadosEvent is emitted when combos are replaced.
type VentaCombosReemplazadosEvent struct{ ventaEditEvent }

// NewVentaCombosReemplazadosEvent constructs the event.
func NewVentaCombosReemplazadosEvent(ventaID uuid.UUID, count int, by uuid.UUID, now time.Time) VentaCombosReemplazadosEvent {
	return VentaCombosReemplazadosEvent{ventaEditEvent{
		ventaID: ventaID, by: by, occurredAt: now, itemsCount: count,
		eventType: EventTypeVentaCombosReemplazados,
	}}
}

// Payload returns the serializable event payload.
func (e VentaCombosReemplazadosEvent) Payload() map[string]any {
	p := e.basePayload()
	p["combos_count"] = e.itemsCount
	return p
}

// VentaVendedoresReemplazadosEvent is emitted when vendedores are replaced.
type VentaVendedoresReemplazadosEvent struct{ ventaEditEvent }

// NewVentaVendedoresReemplazadosEvent constructs the event.
func NewVentaVendedoresReemplazadosEvent(ventaID uuid.UUID, count int, by uuid.UUID, now time.Time) VentaVendedoresReemplazadosEvent {
	return VentaVendedoresReemplazadosEvent{ventaEditEvent{
		ventaID: ventaID, by: by, occurredAt: now, itemsCount: count,
		eventType: EventTypeVentaVendedoresReemplazados,
	}}
}

// Payload returns the serializable event payload.
func (e VentaVendedoresReemplazadosEvent) Payload() map[string]any {
	p := e.basePayload()
	p["vendedores_count"] = e.itemsCount
	return p
}

// VentaEnviadaARevisionEvent is emitted on borrador → revisada.
type VentaEnviadaARevisionEvent struct{ ventaEditEvent }

// NewVentaEnviadaARevisionEvent constructs the event.
func NewVentaEnviadaARevisionEvent(ventaID, by uuid.UUID, now time.Time) VentaEnviadaARevisionEvent {
	return VentaEnviadaARevisionEvent{ventaEditEvent{
		ventaID: ventaID, by: by, occurredAt: now,
		eventType: EventTypeVentaEnviadaARevision,
	}}
}

// Payload returns the serializable event payload.
func (e VentaEnviadaARevisionEvent) Payload() map[string]any { return e.basePayload() }

// VentaAprobadaEvent is emitted on revisada → aprobada.
type VentaAprobadaEvent struct{ ventaEditEvent }

// NewVentaAprobadaEvent constructs the event.
func NewVentaAprobadaEvent(ventaID, by uuid.UUID, now time.Time) VentaAprobadaEvent {
	return VentaAprobadaEvent{ventaEditEvent{
		ventaID: ventaID, by: by, occurredAt: now,
		eventType: EventTypeVentaAprobada,
	}}
}

// Payload returns the serializable event payload.
func (e VentaAprobadaEvent) Payload() map[string]any { return e.basePayload() }

// VentaRegresadaABorradorEvent is emitted on revisada → borrador.
type VentaRegresadaABorradorEvent struct{ ventaEditEvent }

// NewVentaRegresadaABorradorEvent constructs the event.
func NewVentaRegresadaABorradorEvent(ventaID, by uuid.UUID, now time.Time) VentaRegresadaABorradorEvent {
	return VentaRegresadaABorradorEvent{ventaEditEvent{
		ventaID: ventaID, by: by, occurredAt: now,
		eventType: EventTypeVentaRegresadaABorrador,
	}}
}

// Payload returns the serializable event payload.
func (e VentaRegresadaABorradorEvent) Payload() map[string]any { return e.basePayload() }

// VentaAplicadaEvent is emitted when a venta is materialized in Microsip.
type VentaAplicadaEvent struct {
	ventaID    uuid.UUID
	doctoPVID  int
	folio      string
	by         uuid.UUID
	occurredAt time.Time
}

// NewVentaAplicadaEvent constructs the event.
func NewVentaAplicadaEvent(ventaID uuid.UUID, doctoPVID int, folio string, by uuid.UUID, now time.Time) VentaAplicadaEvent {
	return VentaAplicadaEvent{ventaID: ventaID, doctoPVID: doctoPVID, folio: folio, by: by, occurredAt: now}
}

// EventType returns the canonical event identifier.
func (e VentaAplicadaEvent) EventType() string { return EventTypeVentaAplicada }

// AggregateID returns the venta ID.
func (e VentaAplicadaEvent) AggregateID() uuid.UUID { return e.ventaID }

// OccurredAt returns the materialization timestamp.
func (e VentaAplicadaEvent) OccurredAt() time.Time { return e.occurredAt }

// Payload returns the serializable event payload.
func (e VentaAplicadaEvent) Payload() map[string]any {
	return map[string]any{
		"venta_id":             e.ventaID.String(),
		"microsip_docto_pv_id": e.doctoPVID,
		"microsip_folio":       e.folio,
		"applied_by":           e.by.String(),
	}
}
