//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// Mensaje is one outbound message queued/sent by the reactivación channel. It
// maps 1:1 to a row in MSP_RX_MENSAJES and carries the send state machine
// (see EstadoMensaje): encolado → enviado | fallido | bloqueado.
//
// Type classification: Type B pipeline (state-machine entity) — embeds
// audit.Timestamped, no per-user audit trail (the channel is system-driven).
type Mensaje struct {
	id          uuid.UUID
	clienteID   int
	segmento    Segmento
	telefono    string
	cuerpo      string
	estado      EstadoMensaje
	senderKind  SenderKind
	encoladoEn  time.Time
	enviadoEn   time.Time
	errorMotivo string
	timestamps  audit.Timestamped
}

// ─── Crear constructor ────────────────────────────────────────────────────────

// CrearMensajeParams groups all inputs for CrearMensaje. Pass Now explicitly
// so the constructor is deterministic and unit-testable (domain code must
// never call time.Now() internally).
type CrearMensajeParams struct {
	ClienteID int
	Segmento  Segmento
	Telefono  string
	Cuerpo    string
	Now       time.Time
}

// CrearMensaje validates all invariants, generates a new UUID, and returns a
// fresh Mensaje in EstadoEncolado ready to be persisted.
//
// Invariants:
//   - ClienteID > 0
//   - Segmento must be one of the canonical values
//   - Telefono must not be empty
//   - Cuerpo must not be empty
func CrearMensaje(p CrearMensajeParams) (*Mensaje, error) {
	if p.ClienteID <= 0 {
		return nil, ErrMensajeClienteIDInvalido
	}
	if !p.Segmento.Valido() {
		return nil, ErrSegmentoInvalido
	}
	if p.Telefono == "" {
		return nil, ErrMensajeTelefonoRequerido
	}
	if p.Cuerpo == "" {
		return nil, ErrMensajeCuerpoRequerido
	}

	return &Mensaje{
		id:         uuid.New(),
		clienteID:  p.ClienteID,
		segmento:   p.Segmento,
		telefono:   p.Telefono,
		cuerpo:     p.Cuerpo,
		estado:     EstadoEncolado,
		encoladoEn: p.Now,
		timestamps: audit.NewTimestamped(p.Now),
	}, nil
}

// ─── Hydrate constructor ──────────────────────────────────────────────────────

// HydrateMensajeParams groups all fields for reconstructing a Mensaje from a
// persisted row. Used exclusively by the repository.
type HydrateMensajeParams struct {
	ID         uuid.UUID
	ClienteID  int
	Segmento   Segmento
	Telefono   string
	Cuerpo     string
	Estado     EstadoMensaje
	SenderKind SenderKind
	EncoladoEn time.Time
	EnviadoEn  time.Time
	Error      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// HydrateMensaje reconstructs a Mensaje from persistence with zero
// validation. Called only from the repository layer.
func HydrateMensaje(p HydrateMensajeParams) *Mensaje {
	return &Mensaje{
		id:          p.ID,
		clienteID:   p.ClienteID,
		segmento:    p.Segmento,
		telefono:    p.Telefono,
		cuerpo:      p.Cuerpo,
		estado:      p.Estado,
		senderKind:  p.SenderKind,
		encoladoEn:  p.EncoladoEn,
		enviadoEn:   p.EnviadoEn,
		errorMotivo: p.Error,
		timestamps:  audit.HydrateTimestamped(p.CreatedAt, p.UpdatedAt),
	}
}

// ─── Transiciones ──────────────────────────────────────────────────────────────

// MarcarEnviado transitions the message to EstadoEnviado. Only legal from
// EstadoEncolado — returns ErrMensajeTransicionInvalida otherwise. kind
// records which channel implementation delivered the message (measurement
// integrity: never call this with a fabricated kind).
func (m *Mensaje) MarcarEnviado(kind SenderKind, now time.Time) error {
	if m.estado != EstadoEncolado {
		return ErrMensajeTransicionInvalida
	}
	m.estado = EstadoEnviado
	m.senderKind = kind
	m.enviadoEn = now
	m.markUpdated(now)
	return nil
}

// MarcarFallido transitions the message to EstadoFallido, recording motivo as
// the failure reason. Callable from any state — a failed send never claims a
// sender kind or a sent timestamp.
func (m *Mensaje) MarcarFallido(motivo string, now time.Time) {
	m.estado = EstadoFallido
	m.errorMotivo = motivo
	m.markUpdated(now)
}

// MarcarBloqueado transitions the message to EstadoBloqueado, recording
// motivo as the block reason (e.g. the governor's circuit-breaker tripped).
func (m *Mensaje) MarcarBloqueado(motivo string, now time.Time) {
	m.estado = EstadoBloqueado
	m.errorMotivo = motivo
	m.markUpdated(now)
}

// markUpdated rebuilds the embedded Timestamped with the same createdAt and a
// new updatedAt — audit.Timestamped.MarkUpdated stamps time.Now() internally,
// which domain code must never call, so transitions reconstruct the value
// object with the explicit now passed by the caller instead.
func (m *Mensaje) markUpdated(now time.Time) {
	m.timestamps = audit.HydrateTimestamped(m.timestamps.CreatedAt(), now)
}

// ─── Getters ──────────────────────────────────────────────────────────────────

// ID returns the entity's UUID.
func (m *Mensaje) ID() uuid.UUID { return m.id }

// ClienteID returns the Microsip cliente ID. Always > 0.
func (m *Mensaje) ClienteID() int { return m.clienteID }

// Segmento returns the universe slice this message's cliente belongs to.
func (m *Mensaje) Segmento() Segmento { return m.segmento }

// Telefono returns the destination phone number.
func (m *Mensaje) Telefono() string { return m.telefono }

// Cuerpo returns the message body.
func (m *Mensaje) Cuerpo() string { return m.cuerpo }

// Estado returns the current position in the send state machine.
func (m *Mensaje) Estado() EstadoMensaje { return m.estado }

// SenderKind returns which channel implementation delivered the message.
// Empty until MarcarEnviado runs.
func (m *Mensaje) SenderKind() SenderKind { return m.senderKind }

// EncoladoEn returns when the message was queued. Never zero.
func (m *Mensaje) EncoladoEn() time.Time { return m.encoladoEn }

// EnviadoEn returns when the message was sent. Zero until MarcarEnviado runs.
func (m *Mensaje) EnviadoEn() time.Time { return m.enviadoEn }

// Motivo returns the failure/block reason. Empty unless Estado is fallido or
// bloqueado. Named Motivo (not Error) so *Mensaje never satisfies the built-in
// error interface by accident.
func (m *Mensaje) Motivo() string { return m.errorMotivo }

// CreatedAt returns the UTC timestamp when the message row was first created.
func (m *Mensaje) CreatedAt() time.Time { return m.timestamps.CreatedAt() }

// UpdatedAt returns the UTC timestamp of the last state transition.
func (m *Mensaje) UpdatedAt() time.Time { return m.timestamps.UpdatedAt() }
