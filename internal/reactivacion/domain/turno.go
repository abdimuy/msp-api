//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Turno is one message exchanged in a copiloto conversation — entrante (from
// the cliente) or saliente (to the cliente, from ia or humano). It maps 1:1
// to a row in MSP_RX_TURNO and is append-only: once created, only
// MensajeRef may change (linking it to the Fase 2 send record once the
// saliente turno is actually enqueued/sent).
//
// Type classification: append-only log entry — no state machine, no audit
// trail beyond CreatedAt (the channel is system-driven).
type Turno struct {
	id         string
	clienteID  int
	direccion  DireccionTurno
	autor      Autor
	cuerpo     string
	mensajeRef string
	createdAt  time.Time
}

// ─── Crear constructor ────────────────────────────────────────────────────────

// CrearTurnoParams groups all inputs for CrearTurno. Pass Now explicitly so
// the constructor is deterministic and unit-testable (domain code must never
// call time.Now() internally).
type CrearTurnoParams struct {
	ClienteID int
	Direccion DireccionTurno
	Autor     Autor
	Cuerpo    string
	Now       time.Time
}

// CrearTurno validates all invariants, generates a new ID, and returns a
// fresh Turno ready to be persisted.
//
// Invariants:
//   - ClienteID > 0
//   - Direccion must be one of the canonical values
//   - Autor must be one of the canonical values
//   - Cuerpo must not be empty or whitespace-only
func CrearTurno(p CrearTurnoParams) (*Turno, error) {
	if p.ClienteID <= 0 {
		return nil, ErrTurnoClienteIDInvalido
	}
	if !p.Direccion.Valido() {
		return nil, ErrDireccionTurnoInvalido
	}
	if !p.Autor.Valido() {
		return nil, ErrAutorInvalido
	}
	if strings.TrimSpace(p.Cuerpo) == "" {
		return nil, ErrTurnoCuerpoRequerido
	}

	return &Turno{
		id:        uuid.New().String(),
		clienteID: p.ClienteID,
		direccion: p.Direccion,
		autor:     p.Autor,
		cuerpo:    p.Cuerpo,
		createdAt: p.Now,
	}, nil
}

// ─── Hydrate constructor ──────────────────────────────────────────────────────

// HydrateTurnoParams groups all fields for reconstructing a Turno from a
// persisted row. Used exclusively by the repository.
type HydrateTurnoParams struct {
	ID         string
	ClienteID  int
	Direccion  DireccionTurno
	Autor      Autor
	Cuerpo     string
	MensajeRef string
	CreatedAt  time.Time
}

// HydrateTurno reconstructs a Turno from persistence with zero validation.
// Called only from the repository layer.
func HydrateTurno(p HydrateTurnoParams) *Turno {
	return &Turno{
		id:         p.ID,
		clienteID:  p.ClienteID,
		direccion:  p.Direccion,
		autor:      p.Autor,
		cuerpo:     p.Cuerpo,
		mensajeRef: p.MensajeRef,
		createdAt:  p.CreatedAt,
	}
}

// ─── Mutadores ────────────────────────────────────────────────────────────────

// SetMensajeRef links this turno to a Fase 2 send record once it has been
// enqueued through the channel.
func (t *Turno) SetMensajeRef(ref string) { t.mensajeRef = ref }

// ─── Getters ──────────────────────────────────────────────────────────────────

// ID returns the entity's identifier.
func (t *Turno) ID() string { return t.id }

// ClienteID returns the Microsip cliente ID.
func (t *Turno) ClienteID() int { return t.clienteID }

// Direccion returns whether this turno came from the cliente or was sent to
// them.
func (t *Turno) Direccion() DireccionTurno { return t.direccion }

// Autor returns who produced this turno.
func (t *Turno) Autor() Autor { return t.autor }

// Cuerpo returns the turno's message body.
func (t *Turno) Cuerpo() string { return t.cuerpo }

// MensajeRef returns the linked Fase 2 send record ID, or empty when this
// turno has no associated send (e.g. an entrante turno).
func (t *Turno) MensajeRef() string { return t.mensajeRef }

// CreatedAt returns the UTC timestamp when the turno was recorded.
func (t *Turno) CreatedAt() time.Time { return t.createdAt }
