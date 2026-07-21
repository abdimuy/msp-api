//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Decision is one copiloto LLM analysis of an inbound Turno, together with
// the outcome once an operator (or the auto-send policy) acts on it. It maps
// 1:1 to a row in MSP_RX_DECISION — an audit trail of what the LLM
// proposed and what actually happened.
//
// Type classification: append-only log entry with a single post-creation
// mutation (Resultado) — no full state machine, no timestamps beyond
// CreatedAt (the row is never "updated" except for that one field).
type Decision struct {
	id                string
	clienteID         int
	turnoRef          string
	intencion         string
	confianza         int
	senales           []string
	accionPropuesta   Accion
	borrador          string
	evidencia         []string
	razonEscalamiento string
	resultado         ResultadoDecision
	createdAt         time.Time
}

// ─── Crear constructor ────────────────────────────────────────────────────────

// CrearDecisionParams groups all inputs for CrearDecision. Pass Now
// explicitly so the constructor is deterministic and unit-testable (domain
// code must never call time.Now() internally).
type CrearDecisionParams struct {
	ClienteID         int
	TurnoRef          string
	Intencion         string
	Confianza         int
	Senales           []string
	Accion            Accion
	Borrador          string
	Evidencia         []string
	RazonEscalamiento string
	Resultado         ResultadoDecision
	Now               time.Time
}

// CrearDecision validates all invariants, generates a new ID, and returns a
// fresh Decision ready to be persisted.
//
// Invariants:
//   - ClienteID > 0
//   - Accion must be one of the canonical values
//   - Resultado must be one of the canonical values
//   - Confianza must be in [0, 100]
//
// Senales and Evidencia default to a non-nil empty slice when nil.
func CrearDecision(p CrearDecisionParams) (*Decision, error) {
	if p.ClienteID <= 0 {
		return nil, ErrDecisionClienteIDInvalido
	}
	if !p.Accion.Valido() {
		return nil, ErrAccionInvalido
	}
	if !p.Resultado.Valido() {
		return nil, ErrResultadoDecisionInvalido
	}
	if p.Confianza < 0 || p.Confianza > 100 {
		return nil, ErrDecisionConfianzaInvalida
	}

	senales := p.Senales
	if senales == nil {
		senales = []string{}
	}
	evidencia := p.Evidencia
	if evidencia == nil {
		evidencia = []string{}
	}

	return &Decision{
		id:                uuid.New().String(),
		clienteID:         p.ClienteID,
		turnoRef:          p.TurnoRef,
		intencion:         p.Intencion,
		confianza:         p.Confianza,
		senales:           senales,
		accionPropuesta:   p.Accion,
		borrador:          p.Borrador,
		evidencia:         evidencia,
		razonEscalamiento: p.RazonEscalamiento,
		resultado:         p.Resultado,
		createdAt:         p.Now,
	}, nil
}

// ─── Hydrate constructor ──────────────────────────────────────────────────────

// HydrateDecisionParams groups all fields for reconstructing a Decision from
// a persisted row. Used exclusively by the repository.
type HydrateDecisionParams struct {
	ID                string
	ClienteID         int
	TurnoRef          string
	Intencion         string
	Confianza         int
	Senales           []string
	AccionPropuesta   Accion
	Borrador          string
	Evidencia         []string
	RazonEscalamiento string
	Resultado         ResultadoDecision
	CreatedAt         time.Time
}

// HydrateDecision reconstructs a Decision from persistence with zero
// validation. Called only from the repository layer.
func HydrateDecision(p HydrateDecisionParams) *Decision {
	return &Decision{
		id:                p.ID,
		clienteID:         p.ClienteID,
		turnoRef:          p.TurnoRef,
		intencion:         p.Intencion,
		confianza:         p.Confianza,
		senales:           p.Senales,
		accionPropuesta:   p.AccionPropuesta,
		borrador:          p.Borrador,
		evidencia:         p.Evidencia,
		razonEscalamiento: p.RazonEscalamiento,
		resultado:         p.Resultado,
		createdAt:         p.CreatedAt,
	}
}

// ─── Mutadores ────────────────────────────────────────────────────────────────

// SetResultado records the operator's action on this decision (approved,
// edited, escalated) — or the auto-send policy's outcome.
func (d *Decision) SetResultado(resultado ResultadoDecision) { d.resultado = resultado }

// ─── Getters ──────────────────────────────────────────────────────────────────

// ID returns the entity's identifier.
func (d *Decision) ID() string { return d.id }

// ClienteID returns the Microsip cliente ID.
func (d *Decision) ClienteID() int { return d.clienteID }

// TurnoRef returns the ID of the inbound Turno this decision analyzed.
func (d *Decision) TurnoRef() string { return d.turnoRef }

// Intencion returns the LLM's free-text read of the cliente's intent.
func (d *Decision) Intencion() string { return d.intencion }

// Confianza returns the LLM's confidence score, in [0, 100].
func (d *Decision) Confianza() int { return d.confianza }

// Senales returns a defensive copy of the signals the LLM detected.
func (d *Decision) Senales() []string {
	out := make([]string, len(d.senales))
	copy(out, d.senales)
	return out
}

// AccionPropuesta returns what the LLM proposed to do.
func (d *Decision) AccionPropuesta() Accion { return d.accionPropuesta }

// Borrador returns the LLM's drafted reply. Typically empty when
// AccionPropuesta is AccionEscalar (the app layer usually has nothing to
// draft when escalating), but this is NOT an enforced invariant: the raw
// LLM draft is sometimes stored on an escalated Decision too, for audit
// purposes only — it is never sent in that case.
func (d *Decision) Borrador() string { return d.borrador }

// Evidencia returns a defensive copy of the evidence snippets backing this
// decision.
func (d *Decision) Evidencia() []string {
	out := make([]string, len(d.evidencia))
	copy(out, d.evidencia)
	return out
}

// RazonEscalamiento returns why the LLM proposed escalating, empty when
// AccionPropuesta is AccionResponder.
func (d *Decision) RazonEscalamiento() string { return d.razonEscalamiento }

// Resultado returns the current outcome of this decision.
func (d *Decision) Resultado() ResultadoDecision { return d.resultado }

// CreatedAt returns the UTC timestamp when the decision was recorded.
func (d *Decision) CreatedAt() time.Time { return d.createdAt }
