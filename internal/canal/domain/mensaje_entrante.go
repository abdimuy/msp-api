package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// Field-length bounds, measured in codepoints (requireBounded/optionalBounded),
// not bytes. There is no Firebird column to mirror — canal's mailbox is
// SQLite (see the plan's "desviaciones conscientes" table) — so these are
// this module's own, chosen generously for the shapes Meta actually sends.
const (
	// maxWamidLength bounds Meta's own message id.
	maxWamidLength = 255
	// maxRemitenteLength bounds the sender's phone number.
	maxRemitenteLength = 32
	// maxPhoneNumberIDLength bounds Meta's destination phone_number_id.
	maxPhoneNumberIDLength = 32
	// maxTipoLength bounds the message type string.
	maxTipoLength = 64
	// maxContenidoLength bounds the message body or media reference. 4096 is
	// WhatsApp's own limit on a text message body.
	maxContenidoLength = 4096
	// maxMotivoFalloLength bounds the human-readable failure reason.
	maxMotivoFalloLength = 500
)

// MensajeEntrante is one inbound WhatsApp message received by the VPS edge
// and waiting to be relayed to the store's on-premise server. It is
// deduplicated by Wamid — Meta's own message id — not by its own ID: the
// entity always gets a fresh uuid.New() identity (no logic in the database),
// but the mailbox's idempotency guarantee is keyed on Wamid, because that is
// the value Meta itself uses to tell "the same webhook delivered twice"
// apart from "a new message".
//
// Type classification: Type B pipeline (state-machine entity) — embeds
// audit.Timestamped, no per-user audit trail (the mailbox is driven by Meta
// and by the forwarding worker, never by an authenticated MSP user).
type MensajeEntrante struct {
	id            uuid.UUID
	wamid         string
	remitente     string
	phoneNumberID string
	tipo          string
	contenido     string
	timestampMeta time.Time
	recibidoEn    time.Time
	estado        EstadoReenvio
	motivoFallo   string
	timestamps    audit.Timestamped
	pendingEvents []Event
}

// ─── New constructor ───────────────────────────────────────────────────────

// NewMensajeEntranteParams groups the inputs to NewMensajeEntrante.
//
// TimestampMeta and RecibidoEn are two different instants, kept deliberately
// separate: TimestampMeta is Meta's own timestamp on the message — evidence
// about when the customer sent it — while RecibidoEn is the moment the VPS
// received the webhook, which is what the mailbox orders and retries by.
// RecibidoEn is not drawn from time.Now() inside this constructor — the
// caller draws it from the outbound.Clock port and passes it in, so domain
// code never calls the wall clock directly.
type NewMensajeEntranteParams struct {
	Wamid         string
	Remitente     string
	PhoneNumberID string
	Tipo          string
	Contenido     string
	TimestampMeta time.Time
	RecibidoEn    time.Time
}

// NewMensajeEntrante validates all invariants, generates a new UUID, and
// returns a fresh MensajeEntrante in EstadoReenvioPendiente ready to be
// persisted. It buffers a canal.mensaje_entrante_recibido event.
//
// Invariants:
//   - Wamid, Remitente, PhoneNumberID, Tipo must not be empty
//   - every text field is bounded (see the max*Length constants) and free of
//     unsafe characters
//   - TimestampMeta and RecibidoEn must not be the zero time
//
// Contenido is the one field allowed to be empty: not every message type
// Meta sends carries a text body or a media reference the app layer already
// understands, and the mailbox's job is to relay whatever arrived, not to
// reject a message because one field came back blank.
func NewMensajeEntrante(p NewMensajeEntranteParams) (*MensajeEntrante, error) {
	wamid, err := requireBounded(p.Wamid, maxWamidLength,
		ErrMensajeEntranteWamidRequerido, ErrMensajeEntranteWamidDemasiadoLargo)
	if err != nil {
		return nil, err
	}
	remitente, err := requireBounded(p.Remitente, maxRemitenteLength,
		ErrMensajeEntranteRemitenteRequerido, ErrMensajeEntranteRemitenteDemasiadoLargo)
	if err != nil {
		return nil, err
	}
	phoneNumberID, err := requireBounded(p.PhoneNumberID, maxPhoneNumberIDLength,
		ErrMensajeEntrantePhoneNumberIDRequerido, ErrMensajeEntrantePhoneNumberIDDemasiadoLargo)
	if err != nil {
		return nil, err
	}
	tipo, err := requireBounded(p.Tipo, maxTipoLength,
		ErrMensajeEntranteTipoRequerido, ErrMensajeEntranteTipoDemasiadoLargo)
	if err != nil {
		return nil, err
	}
	contenido, err := optionalBounded(p.Contenido, maxContenidoLength, ErrMensajeEntranteContenidoDemasiadoLargo)
	if err != nil {
		return nil, err
	}
	if p.TimestampMeta.IsZero() {
		return nil, ErrMensajeEntranteTimestampMetaRequerido
	}
	if p.RecibidoEn.IsZero() {
		return nil, ErrMensajeEntranteRecibidoEnRequerido
	}

	id := uuid.New()
	m := &MensajeEntrante{
		id:            id,
		wamid:         wamid,
		remitente:     remitente,
		phoneNumberID: phoneNumberID,
		tipo:          tipo,
		contenido:     contenido,
		timestampMeta: p.TimestampMeta,
		recibidoEn:    p.RecibidoEn,
		estado:        EstadoReenvioPendiente,
		timestamps:    audit.NewTimestamped(p.RecibidoEn),
	}
	m.pendingEvents = []Event{
		NewMensajeEntranteRecibidoEvent(id, wamid, remitente, phoneNumberID, tipo, p.RecibidoEn),
	}
	return m, nil
}

// ─── Rehydrate constructor ─────────────────────────────────────────────────

// RehydrateMensajeEntranteParams groups all fields for reconstructing a
// MensajeEntrante from a persisted row. Used exclusively by the repository.
type RehydrateMensajeEntranteParams struct {
	ID            uuid.UUID
	Wamid         string
	Remitente     string
	PhoneNumberID string
	Tipo          string
	Contenido     string
	TimestampMeta time.Time
	RecibidoEn    time.Time
	Estado        EstadoReenvio
	MotivoFallo   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// RehydrateMensajeEntrante reconstructs a MensajeEntrante from persistence
// with zero validation and no buffered events. Called only from the
// repository layer.
func RehydrateMensajeEntrante(p RehydrateMensajeEntranteParams) *MensajeEntrante {
	return &MensajeEntrante{
		id:            p.ID,
		wamid:         p.Wamid,
		remitente:     p.Remitente,
		phoneNumberID: p.PhoneNumberID,
		tipo:          p.Tipo,
		contenido:     p.Contenido,
		timestampMeta: p.TimestampMeta,
		recibidoEn:    p.RecibidoEn,
		estado:        p.Estado,
		motivoFallo:   p.MotivoFallo,
		timestamps:    audit.HydrateTimestamped(p.CreatedAt, p.UpdatedAt),
	}
}

// ─── Transiciones ───────────────────────────────────────────────────────────

// transitionTo moves the entity to target if the state machine allows it, or
// returns ErrMensajeEntranteTransicionInvalida. It does not stamp updatedAt
// or buffer an event — callers do both after any of their own validation
// succeeds, so a rejected call never leaves partial state behind.
func (m *MensajeEntrante) transitionTo(target EstadoReenvio) error {
	if !m.estado.CanTransitionTo(target) {
		return ErrMensajeEntranteTransicionInvalida
	}
	m.estado = target
	return nil
}

// MarcarReenviado transitions the message to EstadoReenvioReenviado. Only
// legal from EstadoReenvioPendiente. Buffers a canal.mensaje_reenviado
// event.
func (m *MensajeEntrante) MarcarReenviado(now time.Time) error {
	if err := m.transitionTo(EstadoReenvioReenviado); err != nil {
		return err
	}
	m.timestamps.MarkUpdatedAt(now)
	m.pendingEvents = append(m.pendingEvents, NewMensajeReenviadoEvent(m.id, m.wamid, now))
	return nil
}

// MarcarFallido transitions the message to EstadoReenvioFallido, recording
// motivo as the failure reason. Only legal from EstadoReenvioPendiente.
// motivo is required and bounded — it is persisted and a human reads it.
// Buffers a canal.reenvio_fallido event.
func (m *MensajeEntrante) MarcarFallido(motivo string, now time.Time) error {
	motivoLimpio, err := requireBounded(motivo, maxMotivoFalloLength,
		ErrMensajeEntranteMotivoFalloRequerido, ErrMensajeEntranteMotivoFalloDemasiadoLargo)
	if err != nil {
		return err
	}
	if err := m.transitionTo(EstadoReenvioFallido); err != nil {
		return err
	}
	m.motivoFallo = motivoLimpio
	m.timestamps.MarkUpdatedAt(now)
	m.pendingEvents = append(m.pendingEvents, NewReenvioFallidoEvent(m.id, m.wamid, motivoLimpio, now))
	return nil
}

// MarcarPendiente retries a failed forward, transitioning the message back
// to EstadoReenvioPendiente. Only legal from EstadoReenvioFallido. No event
// is buffered — the retry is an internal mailbox mechanic, not something a
// downstream consumer needs to react to.
//
// 🔴 This method has NO automatic caller, by design. EstadoReenvioFallido
// is terminal along app.Service's own drain path (DrenarCola/reenviarUno):
// once a message is marked fallido, nothing in app/ or infra/ ever calls
// MarcarPendiente to requeue it, and that is deliberate, not an oversight —
// a message only reaches fallido after being classified a PERMANENT
// forwarding failure (see domain.IsTransient and DrenarResultado.Fallidos'
// doc comment), and a permanent failure retrying itself on a timer would
// just burn attempts against a store that already said no.
//
// This edge exists for OPERATOR-DRIVEN recovery instead: a message can be
// permanent for a reason that later stops being true — a phone that was
// unresolvable until the pilot's cohorte was rebuilt is the canonical
// example — and an operator who has confirmed that changed needs a way to
// put the message back in front of the forwarder without inventing a new
// mechanism. Nothing here builds that requeue path (no HTTP handler, no
// CLI, no cron) — MarcarPendiente only keeps the domain transition legal
// so one can be added later without a schema or state-machine change.
func (m *MensajeEntrante) MarcarPendiente(now time.Time) error {
	if err := m.transitionTo(EstadoReenvioPendiente); err != nil {
		return err
	}
	m.timestamps.MarkUpdatedAt(now)
	return nil
}

// ─── Getters ────────────────────────────────────────────────────────────────

// ID returns the entity's own UUID. Never used for deduplication — see
// Wamid.
func (m *MensajeEntrante) ID() uuid.UUID { return m.id }

// Wamid returns Meta's own message id — the mailbox's idempotency key.
func (m *MensajeEntrante) Wamid() string { return m.wamid }

// Remitente returns the sender's phone number, as Meta sent it.
func (m *MensajeEntrante) Remitente() string { return m.remitente }

// PhoneNumberID returns Meta's destination phone_number_id — which of the
// store's WhatsApp numbers received the message.
func (m *MensajeEntrante) PhoneNumberID() string { return m.phoneNumberID }

// Tipo returns the message type, exactly as Meta reported it. This is a
// plain bounded string, not a closed enum: the mailbox must accept and
// relay message types it does not yet understand, including ones Meta
// introduces after this module ships.
func (m *MensajeEntrante) Tipo() string { return m.tipo }

// Contenido returns the message body for text messages, or a media
// reference for media messages. May be empty for message types that carry
// neither.
func (m *MensajeEntrante) Contenido() string { return m.contenido }

// TimestampMeta returns Meta's own timestamp on the message — evidence
// about when the customer sent it.
func (m *MensajeEntrante) TimestampMeta() time.Time { return m.timestampMeta }

// RecibidoEn returns when the VPS received the webhook. This is what the
// mailbox orders and retries by — never TimestampMeta.
func (m *MensajeEntrante) RecibidoEn() time.Time { return m.recibidoEn }

// Estado returns the current position in the forwarding state machine.
func (m *MensajeEntrante) Estado() EstadoReenvio { return m.estado }

// MotivoFallo returns the last recorded failure reason. Empty unless Estado
// is (or was) fallido.
func (m *MensajeEntrante) MotivoFallo() string { return m.motivoFallo }

// CreatedAt returns the UTC timestamp when the row was first created —
// the same instant as RecibidoEn.
func (m *MensajeEntrante) CreatedAt() time.Time { return m.timestamps.CreatedAt() }

// UpdatedAt returns the UTC timestamp of the last state transition.
func (m *MensajeEntrante) UpdatedAt() time.Time { return m.timestamps.UpdatedAt() }

// PendingEvents returns a defensive copy of the events buffered since
// construction or the last ClearPendingEvents call.
func (m *MensajeEntrante) PendingEvents() []Event {
	out := make([]Event, len(m.pendingEvents))
	copy(out, m.pendingEvents)
	return out
}

// ClearPendingEvents drops every buffered event.
func (m *MensajeEntrante) ClearPendingEvents() { m.pendingEvents = nil }
