//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// Conversacion is the copiloto's per-cliente conversation state machine. It
// maps 1:1 to a row in MSP_RX_CONVERSACION and tracks where the
// cliente/IA/humano exchange sits (see EstadoConversacion), plus the memory
// cache the LLM needs on every turn (ResumenMemoria, ContextoNota, Banderas,
// NotaHash — set by the app layer in Slice B after reading the cobrador's
// note).
//
// Type classification: Type B pipeline (state-machine entity) — embeds
// audit.Timestamped, no per-user audit trail (transitions are driven by the
// cliente/IA/governor, not by an authenticated MSP user).
type Conversacion struct {
	id             string
	clienteID      int
	estado         EstadoConversacion
	asignadoA      string
	resumenMemoria string
	contextoNota   string
	banderas       []string
	notaHash       string
	timestamps     audit.Timestamped
}

// ─── Crear constructor ────────────────────────────────────────────────────────

// CrearConversacion validates clienteID and returns a fresh Conversacion for
// it in EstadoContactado — the state a cliente starts in once the Fase 2
// channel reaches them. now is passed explicitly so the constructor is
// deterministic and unit-testable (domain code must never call time.Now()
// internally).
//
// Invariants:
//   - ClienteID > 0
func CrearConversacion(clienteID int, now time.Time) (*Conversacion, error) {
	if clienteID <= 0 {
		return nil, ErrConversacionClienteIDInvalido
	}

	return &Conversacion{
		id:         uuid.New().String(),
		clienteID:  clienteID,
		estado:     EstadoContactado,
		banderas:   []string{},
		timestamps: audit.NewTimestamped(now),
	}, nil
}

// ─── Hydrate constructor ──────────────────────────────────────────────────────

// HydrateConversacionParams groups all fields for reconstructing a
// Conversacion from a persisted row. Used exclusively by the repository.
type HydrateConversacionParams struct {
	ID             string
	ClienteID      int
	Estado         EstadoConversacion
	AsignadoA      string
	ResumenMemoria string
	ContextoNota   string
	Banderas       []string
	NotaHash       string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// HydrateConversacion reconstructs a Conversacion from persistence with zero
// validation. Called only from the repository layer.
func HydrateConversacion(p HydrateConversacionParams) *Conversacion {
	return &Conversacion{
		id:             p.ID,
		clienteID:      p.ClienteID,
		estado:         p.Estado,
		asignadoA:      p.AsignadoA,
		resumenMemoria: p.ResumenMemoria,
		contextoNota:   p.ContextoNota,
		banderas:       p.Banderas,
		notaHash:       p.NotaHash,
		timestamps:     audit.HydrateTimestamped(p.CreatedAt, p.UpdatedAt),
	}
}

// ─── Transiciones ──────────────────────────────────────────────────────────────

// MarcarRespondio transitions the conversation to EstadoRespondio. Only
// legal from EstadoContactado.
func (c *Conversacion) MarcarRespondio(now time.Time) error {
	if c.estado != EstadoContactado {
		return ErrConversacionTransicionInvalida
	}
	c.estado = EstadoRespondio
	c.markUpdated(now)
	return nil
}

// MarcarConversando transitions the conversation to EstadoConversando. Legal
// from EstadoRespondio or EstadoConversando (re-entrant: another inbound turn
// while already conversando keeps the same state).
func (c *Conversacion) MarcarConversando(now time.Time) error {
	if c.estado != EstadoRespondio && c.estado != EstadoConversando {
		return ErrConversacionTransicionInvalida
	}
	c.estado = EstadoConversando
	c.markUpdated(now)
	return nil
}

// MarcarEscalada transitions the conversation to EstadoEscalado and assigns
// it to asignadoA. Legal from EstadoContactado, EstadoRespondio,
// EstadoConversando, or EstadoEscalado itself (re-escalating to change the
// assignee is allowed).
func (c *Conversacion) MarcarEscalada(asignadoA string, now time.Time) error {
	switch c.estado {
	case EstadoContactado, EstadoRespondio, EstadoConversando, EstadoEscalado:
		// allowed — fall through to the transition below.
	case EstadoInteresado, EstadoEnganche, EstadoDescartado:
		return ErrConversacionTransicionInvalida
	default:
		return ErrConversacionTransicionInvalida
	}
	c.asignadoA = asignadoA
	c.estado = EstadoEscalado
	c.markUpdated(now)
	return nil
}

// MarcarInteresado transitions the conversation to EstadoInteresado. Legal
// from EstadoConversando or EstadoEscalado.
func (c *Conversacion) MarcarInteresado(now time.Time) error {
	if c.estado != EstadoConversando && c.estado != EstadoEscalado {
		return ErrConversacionTransicionInvalida
	}
	c.estado = EstadoInteresado
	c.markUpdated(now)
	return nil
}

// MarcarEnganche transitions the conversation to EstadoEnganche — the
// cliente converted to a new sale. Legal from any non-terminal state.
func (c *Conversacion) MarcarEnganche(now time.Time) error {
	if c.estado.EsTerminal() {
		return ErrConversacionTransicionInvalida
	}
	c.estado = EstadoEnganche
	c.markUpdated(now)
	return nil
}

// MarcarDescartado transitions the conversation to EstadoDescartado — closed
// without conversion. Legal from any non-terminal state.
func (c *Conversacion) MarcarDescartado(now time.Time) error {
	if c.estado.EsTerminal() {
		return ErrConversacionTransicionInvalida
	}
	c.estado = EstadoDescartado
	c.markUpdated(now)
	return nil
}

// ─── Mutadores de caché (memoria/nota) ─────────────────────────────────────────

// SetResumenMemoria replaces the LLM's rolling conversation summary.
func (c *Conversacion) SetResumenMemoria(resumenMemoria string, now time.Time) {
	c.resumenMemoria = resumenMemoria
	c.markUpdated(now)
}

// SetContextoNota replaces the cached distillation of the cobrador's note
// (contexto + banderas + the hash used to detect a stale cache) in one shot.
func (c *Conversacion) SetContextoNota(contexto string, banderas []string, notaHash string, now time.Time) {
	c.contextoNota = contexto
	c.banderas = banderas
	c.notaHash = notaHash
	c.markUpdated(now)
}

// markUpdated rebuilds the embedded Timestamped with the same createdAt and a
// new updatedAt — domain code must never call time.Now() internally, so
// transitions reconstruct the value object with the explicit now passed by
// the caller instead (see Mensaje.markUpdated for the same convention).
func (c *Conversacion) markUpdated(now time.Time) {
	c.timestamps = audit.HydrateTimestamped(c.timestamps.CreatedAt(), now)
}

// ─── Getters ──────────────────────────────────────────────────────────────────

// ID returns the entity's identifier.
func (c *Conversacion) ID() string { return c.id }

// ClienteID returns the Microsip cliente ID.
func (c *Conversacion) ClienteID() int { return c.clienteID }

// Estado returns the current position in the escalation state machine.
func (c *Conversacion) Estado() EstadoConversacion { return c.estado }

// AsignadoA returns the human operator assigned to this conversation, or
// empty when unassigned.
func (c *Conversacion) AsignadoA() string { return c.asignadoA }

// ResumenMemoria returns the LLM's rolling conversation summary.
func (c *Conversacion) ResumenMemoria() string { return c.resumenMemoria }

// ContextoNota returns the cached distillation of the cobrador's note.
func (c *Conversacion) ContextoNota() string { return c.contextoNota }

// Banderas returns a defensive copy of the cached flags derived from the
// cobrador's note, so callers cannot mutate the entity's internal slice.
func (c *Conversacion) Banderas() []string {
	out := make([]string, len(c.banderas))
	copy(out, c.banderas)
	return out
}

// NotaHash returns the hash of the cobrador note last distilled into
// ContextoNota/Banderas — used to detect a stale cache.
func (c *Conversacion) NotaHash() string { return c.notaHash }

// CreatedAt returns the UTC timestamp when the conversation row was first
// created.
func (c *Conversacion) CreatedAt() time.Time { return c.timestamps.CreatedAt() }

// UpdatedAt returns the UTC timestamp of the last transition or cache update.
func (c *Conversacion) UpdatedAt() time.Time { return c.timestamps.UpdatedAt() }
