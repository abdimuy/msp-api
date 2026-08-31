package domain

import (
	"time"

	"github.com/google/uuid"
)

// Event is the contract every domain event in the canal module satisfies.
// Events are emitted by MensajeEntrante's mutators into a buffer that the
// app layer drains after a successful transaction — see
// docs/module-standards/AGGREGATE_PATTERNS.md § Domain events.
type Event interface {
	// EventType returns the canonical event identifier.
	EventType() string
	// AggregateID returns the id of the MensajeEntrante that produced the
	// event.
	AggregateID() uuid.UUID
	// OccurredAt returns the moment the event was produced.
	OccurredAt() time.Time
	// Payload returns the event-specific data as a serializable map.
	Payload() map[string]any
}

// Event type constants, under the canal.* namespace. Stored alongside
// outbox/messaging payloads.
const (
	// EventTypeMensajeEntranteRecibido is emitted when a new MensajeEntrante
	// is accepted into the mailbox.
	EventTypeMensajeEntranteRecibido = "canal.mensaje_entrante_recibido"
	// EventTypeMensajeReenviado is emitted when a MensajeEntrante is
	// forwarded to the store's on-premise server.
	EventTypeMensajeReenviado = "canal.mensaje_reenviado"
	// EventTypeReenvioFallido is emitted when a forward attempt fails.
	EventTypeReenvioFallido = "canal.reenvio_fallido"
)

// MensajeEntranteRecibidoEvent is emitted by NewMensajeEntrante.
type MensajeEntranteRecibidoEvent struct {
	mensajeID     uuid.UUID
	wamid         string
	remitente     string
	phoneNumberID string
	tipo          string
	occurredAt    time.Time
}

// NewMensajeEntranteRecibidoEvent constructs a MensajeEntranteRecibidoEvent.
func NewMensajeEntranteRecibidoEvent(
	mensajeID uuid.UUID,
	wamid, remitente, phoneNumberID, tipo string,
	now time.Time,
) MensajeEntranteRecibidoEvent {
	return MensajeEntranteRecibidoEvent{
		mensajeID:     mensajeID,
		wamid:         wamid,
		remitente:     remitente,
		phoneNumberID: phoneNumberID,
		tipo:          tipo,
		occurredAt:    now,
	}
}

// EventType returns the canonical event identifier.
func (e MensajeEntranteRecibidoEvent) EventType() string {
	return EventTypeMensajeEntranteRecibido
}

// AggregateID returns the MensajeEntrante id.
func (e MensajeEntranteRecibidoEvent) AggregateID() uuid.UUID { return e.mensajeID }

// OccurredAt returns when the message was accepted into the mailbox.
func (e MensajeEntranteRecibidoEvent) OccurredAt() time.Time { return e.occurredAt }

// Payload returns the serializable event payload.
func (e MensajeEntranteRecibidoEvent) Payload() map[string]any {
	return map[string]any{
		"wamid":           e.wamid,
		"remitente":       e.remitente,
		"phone_number_id": e.phoneNumberID,
		"tipo":            e.tipo,
	}
}

// MensajeReenviadoEvent is emitted by MarcarReenviado.
type MensajeReenviadoEvent struct {
	mensajeID  uuid.UUID
	wamid      string
	occurredAt time.Time
}

// NewMensajeReenviadoEvent constructs a MensajeReenviadoEvent.
func NewMensajeReenviadoEvent(mensajeID uuid.UUID, wamid string, now time.Time) MensajeReenviadoEvent {
	return MensajeReenviadoEvent{mensajeID: mensajeID, wamid: wamid, occurredAt: now}
}

// EventType returns the canonical event identifier.
func (e MensajeReenviadoEvent) EventType() string { return EventTypeMensajeReenviado }

// AggregateID returns the MensajeEntrante id.
func (e MensajeReenviadoEvent) AggregateID() uuid.UUID { return e.mensajeID }

// OccurredAt returns when the forward succeeded.
func (e MensajeReenviadoEvent) OccurredAt() time.Time { return e.occurredAt }

// Payload returns the serializable event payload.
func (e MensajeReenviadoEvent) Payload() map[string]any {
	return map[string]any{"wamid": e.wamid}
}

// ReenvioFallidoEvent is emitted by MarcarFallido.
type ReenvioFallidoEvent struct {
	mensajeID  uuid.UUID
	wamid      string
	motivo     string
	occurredAt time.Time
}

// NewReenvioFallidoEvent constructs a ReenvioFallidoEvent.
func NewReenvioFallidoEvent(mensajeID uuid.UUID, wamid, motivo string, now time.Time) ReenvioFallidoEvent {
	return ReenvioFallidoEvent{mensajeID: mensajeID, wamid: wamid, motivo: motivo, occurredAt: now}
}

// EventType returns the canonical event identifier.
func (e ReenvioFallidoEvent) EventType() string { return EventTypeReenvioFallido }

// AggregateID returns the MensajeEntrante id.
func (e ReenvioFallidoEvent) AggregateID() uuid.UUID { return e.mensajeID }

// OccurredAt returns when the forward attempt failed.
func (e ReenvioFallidoEvent) OccurredAt() time.Time { return e.occurredAt }

// Payload returns the serializable event payload.
func (e ReenvioFallidoEvent) Payload() map[string]any {
	return map[string]any{"wamid": e.wamid, "motivo": e.motivo}
}
