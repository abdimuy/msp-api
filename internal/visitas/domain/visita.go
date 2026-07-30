//nolint:misspell // "IMPORTES_DOCTOS_CC" is the real Microsip table name (Spanish); not a typo of "IMPORTS".
package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// Column-width limits matching MSP_VISITAS (migration 000047).
const (
	maxCobradorLength   = 150
	maxTipoVisitaLength = 100
	maxNotaLength       = 10000
)

// fechaFuturaTolerancia is how far into the future FECHA may sit relative to
// the reference "now" before it is rejected as implausible client-clock
// drift. Deliberately generous: an offline visit captured by the mobile app
// may be uploaded hours or days late (no connectivity on the route), and the
// domain must NOT reject a late-but-valid old date — only a date that is
// suspiciously ahead of the server's clock.
const fechaFuturaTolerancia = 48 * time.Hour

// Visita is the writable aggregate root for a cobranza visit captured by the
// mobile app and persisted 1:1 into MSP_VISITAS — a PREEXISTING legacy
// Firebird table created by the old Node backend (~226k rows in test/prod;
// see migration 000047's header comment). It records that a cobrador visited
// a cliente on their route, whether or not the visit produced a payment.
//
// Visita is a Tipo-A CRUD entity per family convention and therefore embeds
// audit.Auditable for consistency with the rest of the codebase. BUT:
// MSP_VISITAS has NO CREATED_AT/UPDATED_AT/CREATED_BY/UPDATED_BY columns —
// the backend Node never modeled them, and this migration deliberately does
// not add them to the legacy table. The embedded audit subrecord is
// therefore IN-MEMORY ONLY:
//
//   - NewVisita stamps it from the caller-supplied Now/CreatedBy so a
//     same-request response can echo a "created_at", but nothing persists it.
//   - RehydrateVisita always returns a zero-valued audit subrecord — there is
//     no column to read it back from, so fabricating one would be dishonest.
//   - The repository (Task 2 of this plan) does not write or read any audit
//     column for this entity.
type Visita struct {
	id             uuid.UUID
	cobrador       string
	cobradorID     int
	fecha          time.Time
	formaCobroID   int
	lat            float64
	lng            float64
	nota           string
	tipoVisita     string
	zonaClienteID  int
	clienteID      int
	impteDoctoCCID *int

	audit audit.Auditable
}

// CrearVisitaParams carries the inputs to NewVisita. ID is the
// client-generated UUID and acts as the idempotency key end-to-end (the
// mobile app mints it offline; the repo — Task 2 — rejects a duplicate
// insert with ErrVisitaYaExiste). Now/CreatedBy seed the in-memory audit
// subrecord only — see the Visita doc comment.
type CrearVisitaParams struct {
	ID             uuid.UUID
	Cobrador       string
	CobradorID     int
	Fecha          time.Time
	FormaCobroID   int
	Lat            float64
	Lng            float64
	Nota           string
	TipoVisita     string
	ZonaClienteID  int
	ClienteID      int
	ImpteDoctoCCID *int
	CreatedBy      uuid.UUID
	Now            time.Time
}

// NewVisita validates the inputs and constructs a fresh Visita. Fecha is the
// client-captured business timestamp (when the cobrador visited); Now is the
// reference instant used both to bound Fecha (ErrVisitaFechaFutura) and to
// seed the in-memory audit subrecord — callers should pass their Clock port's
// Now(), never call time.Now() from inside this package
// (docs/module-standards/DATETIME_HANDLING.md).
func NewVisita(p CrearVisitaParams) (*Visita, error) {
	if p.ID == uuid.Nil {
		return nil, ErrVisitaIDRequerido
	}
	if p.ClienteID <= 0 {
		return nil, ErrVisitaClienteRequerido
	}
	cobrador, err := requireBounded(p.Cobrador, maxCobradorLength, ErrVisitaCobradorRequerido, ErrVisitaCobradorDemasiadoLargo)
	if err != nil {
		return nil, err
	}
	tipoVisita, err := requireBounded(p.TipoVisita, maxTipoVisitaLength, ErrVisitaTipoRequerido, ErrVisitaTipoDemasiadoLargo)
	if err != nil {
		return nil, err
	}
	if p.Fecha.IsZero() {
		return nil, ErrVisitaFechaRequerida
	}
	if p.Fecha.After(p.Now.Add(fechaFuturaTolerancia)) {
		return nil, ErrVisitaFechaFutura
	}
	nota, err := trimOptionalBounded(p.Nota, maxNotaLength, ErrVisitaNotaDemasiadoLarga)
	if err != nil {
		return nil, err
	}

	return &Visita{
		id:             p.ID,
		cobrador:       cobrador,
		cobradorID:     p.CobradorID,
		fecha:          p.Fecha,
		formaCobroID:   p.FormaCobroID,
		lat:            p.Lat,
		lng:            p.Lng,
		nota:           nota,
		tipoVisita:     tipoVisita,
		zonaClienteID:  p.ZonaClienteID,
		clienteID:      p.ClienteID,
		impteDoctoCCID: p.ImpteDoctoCCID,
		audit:          audit.NewAuditable(p.Now, p.CreatedBy),
	}, nil
}

// RehydrateVisitaParams carries the persisted shape of a Visita for
// repository reconstruction — the 11 MSP_VISITAS business columns only.
// There are no audit fields: see the Visita doc comment on why.
type RehydrateVisitaParams struct {
	ID             uuid.UUID
	Cobrador       string
	CobradorID     int
	Fecha          time.Time
	FormaCobroID   int
	Lat            float64
	Lng            float64
	Nota           string
	TipoVisita     string
	ZonaClienteID  int
	ClienteID      int
	ImpteDoctoCCID *int
}

// RehydrateVisita rebuilds a Visita from persistence without validation —
// the repo must trust the persisted row was validated on write. The audit
// subrecord is left zero-valued: MSP_VISITAS carries no audit columns to
// rehydrate it from (see the Visita doc comment).
func RehydrateVisita(p RehydrateVisitaParams) *Visita {
	return &Visita{
		id:             p.ID,
		cobrador:       p.Cobrador,
		cobradorID:     p.CobradorID,
		fecha:          p.Fecha,
		formaCobroID:   p.FormaCobroID,
		lat:            p.Lat,
		lng:            p.Lng,
		nota:           p.Nota,
		tipoVisita:     p.TipoVisita,
		zonaClienteID:  p.ZonaClienteID,
		clienteID:      p.ClienteID,
		impteDoctoCCID: p.ImpteDoctoCCID,
	}
}

// ─── Getters ────────────────────────────────────────────────────────────────

// ID returns the visita's UUID (idempotency key end-to-end).
func (v *Visita) ID() uuid.UUID { return v.id }

// Cobrador returns the cobrador's free-text name.
func (v *Visita) Cobrador() string { return v.cobrador }

// CobradorID returns the Microsip cobrador_id, or zero if not resolved.
func (v *Visita) CobradorID() int { return v.cobradorID }

// Fecha returns the client-captured visit timestamp (FECHA in DB).
func (v *Visita) Fecha() time.Time { return v.fecha }

// FormaCobroID returns the Microsip forma_cobro_id, or zero if the visit did
// not produce a payment.
func (v *Visita) FormaCobroID() int { return v.formaCobroID }

// Lat returns the latitude where the visit was captured.
func (v *Visita) Lat() float64 { return v.lat }

// Lng returns the longitude where the visit was captured.
func (v *Visita) Lng() float64 { return v.lng }

// Nota returns the cobrador's free-text note. Empty string means NULL/no
// note (MSP_VISITAS.NOTA is nullable).
func (v *Visita) Nota() string { return v.nota }

// TipoVisita returns the visit type (e.g. "cobro", "no_encontrado").
func (v *Visita) TipoVisita() string { return v.tipoVisita }

// ZonaClienteID returns the cliente's zona at the time of the visit, or zero
// if not resolved.
func (v *Visita) ZonaClienteID() int { return v.zonaClienteID }

// ClienteID returns the Microsip cliente_id visited.
func (v *Visita) ClienteID() int { return v.clienteID }

// ImpteDoctoCCID returns the Microsip IMPORTES_DOCTOS_CC.IMPTE_DOCTO_CC_ID
// linked to this visit's payment, or nil if the visit did not produce one.
func (v *Visita) ImpteDoctoCCID() *int { return v.impteDoctoCCID }

// Audit returns a copy of the audit subrecord. IN-MEMORY ONLY for this
// entity — see the Visita doc comment. A Visita built via RehydrateVisita
// always returns a zero-valued Auditable.
func (v *Visita) Audit() audit.Auditable { return v.audit }
