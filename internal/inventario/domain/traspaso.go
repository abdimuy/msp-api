//nolint:misspell // domain vocabulary is Spanish (traspaso, almacén, descripción, etc.) per project convention.
package domain

import (
	"iter"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// maxDescripcionLength is the maximum allowed length (in runes) for the
// descripcion field.
const maxDescripcionLength = 200

// reversoPrefijo is the prefix added to descripcion when a traspaso is
// reversed.
const reversoPrefijo = "REVERSO: "

// Traspaso is the aggregate root of the inventario module. It represents a
// physical stock movement (transfer) between two Microsip warehouses. Each
// traspaso owns a collection of TraspasoDetalle child entities that enumerate
// the articles and quantities being moved.
//
// Type A CRUD entity per CLAUDE.md — msp-only, no Microsip origin.
type Traspaso struct {
	id             uuid.UUID
	folio          Folio
	almacenOrigen  int
	almacenDestino int
	fecha          time.Time
	descripcion    string
	ventaID        *uuid.UUID
	tipoReverso    bool
	reversado      bool // true when this directo has been superseded by a reverso
	doctoInID      *int // set after Microsip insert; nil until applied
	detalles       []*TraspasoDetalle
	audit          audit.Auditable
	pendingEvents  []Event
}

// CrearTraspasoDetalleInput is one article line submitted to CrearTraspaso.
type CrearTraspasoDetalleInput struct {
	ID         uuid.UUID
	ArticuloID int
	Cantidad   Cantidad
}

// CrearTraspasoParams aggregates every field needed to build a fresh Traspaso.
type CrearTraspasoParams struct {
	ID             uuid.UUID
	Folio          Folio
	AlmacenOrigen  int
	AlmacenDestino int
	Fecha          time.Time
	Descripcion    string
	VentaID        *uuid.UUID
	Detalles       []CrearTraspasoDetalleInput
	CreatedBy      uuid.UUID
	Now            time.Time
}

// CrearTraspaso validates the inputs, builds the aggregate, and emits a
// TraspasoCreadoEvent.
func CrearTraspaso(p CrearTraspasoParams) (*Traspaso, error) {
	if p.AlmacenOrigen <= 0 {
		return nil, ErrAlmacenOrigenInvalido
	}
	if p.AlmacenDestino <= 0 {
		return nil, ErrAlmacenDestinoInvalido
	}
	if p.AlmacenOrigen == p.AlmacenDestino {
		return nil, ErrAlmacenesIguales
	}
	if len(p.Detalles) == 0 {
		return nil, ErrTraspasoSinDetalles
	}
	descripcion := strings.TrimSpace(p.Descripcion)
	if len([]rune(descripcion)) > maxDescripcionLength {
		return nil, ErrTraspasoDescripcionDemasiadoLarga
	}
	detalles, err := buildDetalles(p.Detalles)
	if err != nil {
		return nil, err
	}
	t := &Traspaso{
		id:             p.ID,
		folio:          p.Folio,
		almacenOrigen:  p.AlmacenOrigen,
		almacenDestino: p.AlmacenDestino,
		fecha:          p.Fecha,
		descripcion:    descripcion,
		ventaID:        p.VentaID,
		tipoReverso:    false,
		reversado:      false,
		doctoInID:      nil,
		detalles:       detalles,
		audit:          audit.NewAuditable(p.Now, p.CreatedBy),
	}
	t.pendingEvents = []Event{NewTraspasoCreadoEvent(
		t.id,
		t.folio.Value(),
		t.almacenOrigen,
		t.almacenDestino,
		t.ventaID,
		t.tipoReverso,
		len(t.detalles),
		p.Now,
	)}
	return t, nil
}

// buildDetalles materializes the child entities from the input rows.
func buildDetalles(in []CrearTraspasoDetalleInput) ([]*TraspasoDetalle, error) {
	out := make([]*TraspasoDetalle, 0, len(in))
	for _, d := range in {
		det, err := newDetalle(d.ID, d.ArticuloID, d.Cantidad)
		if err != nil {
			return nil, err
		}
		out = append(out, det)
	}
	return out, nil
}

// HydrateTraspasoParams carries the persisted shape of a Traspaso for
// repository reconstruction.
type HydrateTraspasoParams struct {
	ID             uuid.UUID
	Folio          Folio
	AlmacenOrigen  int
	AlmacenDestino int
	Fecha          time.Time
	Descripcion    string
	VentaID        *uuid.UUID
	TipoReverso    bool
	Reversado      bool
	DoctoInID      *int
	Detalles       []*TraspasoDetalle
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CreatedBy      uuid.UUID
	UpdatedBy      uuid.UUID
}

// HydrateTraspaso rebuilds a Traspaso from persistence without validation.
func HydrateTraspaso(p HydrateTraspasoParams) *Traspaso {
	return &Traspaso{
		id:             p.ID,
		folio:          p.Folio,
		almacenOrigen:  p.AlmacenOrigen,
		almacenDestino: p.AlmacenDestino,
		fecha:          p.Fecha,
		descripcion:    p.Descripcion,
		ventaID:        p.VentaID,
		tipoReverso:    p.TipoReverso,
		reversado:      p.Reversado,
		doctoInID:      p.DoctoInID,
		detalles:       p.Detalles,
		audit:          audit.HydrateAuditable(p.CreatedAt, p.UpdatedAt, p.CreatedBy, p.UpdatedBy),
	}
}

// ─── Accessors ─────────────────────────────────────────────────────────────

// ID returns the traspaso's primary key.
func (t *Traspaso) ID() uuid.UUID { return t.id }

// Folio returns the folio VO.
func (t *Traspaso) Folio() Folio { return t.folio }

// AlmacenOrigen returns the origin warehouse identifier.
func (t *Traspaso) AlmacenOrigen() int { return t.almacenOrigen }

// AlmacenDestino returns the destination warehouse identifier.
func (t *Traspaso) AlmacenDestino() int { return t.almacenDestino }

// Fecha returns the movement date/time (UTC).
func (t *Traspaso) Fecha() time.Time { return t.fecha }

// Descripcion returns the movement description.
func (t *Traspaso) Descripcion() string { return t.descripcion }

// VentaID returns the optional linked venta UUID.
func (t *Traspaso) VentaID() *uuid.UUID { return t.ventaID }

// TipoReverso reports whether this traspaso is itself a reversal of another.
func (t *Traspaso) TipoReverso() bool { return t.tipoReverso }

// Reversado reports whether this directo traspaso has been superseded by a
// reverso. Always false for traspasos of tipo='reverso'.
func (t *Traspaso) Reversado() bool { return t.reversado }

// DoctoInID returns the Microsip DOCTOS_IN id after application, or nil until
// then.
func (t *Traspaso) DoctoInID() *int { return t.doctoInID }

// Audit returns a copy of the audit subrecord.
func (t *Traspaso) Audit() audit.Auditable { return t.audit }

// ─── Child iterators ───────────────────────────────────────────────────────

// Detalles returns an iterator over the detalle child entities.
func (t *Traspaso) Detalles() iter.Seq[*TraspasoDetalle] {
	return func(yield func(*TraspasoDetalle) bool) {
		for _, d := range t.detalles {
			if !yield(d) {
				return
			}
		}
	}
}

// DetallesForRepo returns the live detalles slice. Intended for the repository
// layer only — callers must not mutate the returned slice.
func (t *Traspaso) DetallesForRepo() []*TraspasoDetalle { return t.detalles }

// ─── Behavior methods ──────────────────────────────────────────────────────

// Reversar creates a new Traspaso that is the logical inverse of this one:
// almacenes are swapped, tipoReverso is set to true, the descripcion is
// prefixed with "REVERSO: ", and the detalles are deep-copied. A
// TraspasoReversadoEvent is emitted on the returned aggregate.
//
// Returns ErrTraspasoYaReversado if this traspaso is itself already a reverso
// (tipoReverso=true) or if it has already been superseded by a previous reverso
// (reversado=true). Only active directos (tipoReverso=false AND reversado=false)
// can be reversed.
func (t *Traspaso) Reversar(now time.Time, by, newID uuid.UUID, newFolio Folio) (*Traspaso, error) {
	if t.tipoReverso {
		return nil, ErrTraspasoYaReversado
	}
	if t.reversado {
		return nil, ErrTraspasoYaReversado
	}
	// Deep-copy detalles.
	copiedDetalles := make([]*TraspasoDetalle, len(t.detalles))
	for i, d := range t.detalles {
		cloned := *d
		copiedDetalles[i] = &cloned
	}
	descripcion := reversoPrefijo + t.descripcion
	reversed := &Traspaso{
		id:             newID,
		folio:          newFolio,
		almacenOrigen:  t.almacenDestino, // swap
		almacenDestino: t.almacenOrigen,  // swap
		fecha:          now,
		descripcion:    descripcion,
		ventaID:        t.ventaID,
		tipoReverso:    true,
		reversado:      false, // a reverso is never itself reversed
		doctoInID:      nil,
		detalles:       copiedDetalles,
		audit:          audit.NewAuditable(now, by),
	}
	reversed.pendingEvents = []Event{NewTraspasoReversadoEvent(
		reversed.id,
		reversed.folio.Value(),
		reversed.almacenOrigen,
		reversed.almacenDestino,
		reversed.ventaID,
		reversed.tipoReverso,
		len(reversed.detalles),
		now,
	)}
	return reversed, nil
}

// MarcarAplicado records the Microsip DOCTOS_IN id produced after the
// traspaso is applied to Microsip. Returns ErrTraspasoYaAplicado if
// doctoInID is already set.
func (t *Traspaso) MarcarAplicado(doctoInID int) error {
	if t.doctoInID != nil {
		return ErrTraspasoYaAplicado
	}
	t.doctoInID = &doctoInID
	return nil
}

// ─── Events buffer ─────────────────────────────────────────────────────────

// PendingEvents returns a defensive copy of the events buffered since
// construction or the last ClearPendingEvents call.
func (t *Traspaso) PendingEvents() []Event {
	out := make([]Event, len(t.pendingEvents))
	copy(out, t.pendingEvents)
	return out
}

// ClearPendingEvents drops every buffered event.
func (t *Traspaso) ClearPendingEvents() { t.pendingEvents = nil }
