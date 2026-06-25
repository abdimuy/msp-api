//nolint:misspell // domain vocabulary is Spanish (productos, etc.) per project convention.
package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// maxComboNombreLength is the byte width of the combo name column.
const maxComboNombreLength = 200

// Combo is a bundle of productos sold together under a single named offer.
// It is a child entity of the Venta aggregate root; its lifecycle is fully
// managed through Venta — external callers obtain Combos via Venta.Combos().
//
// A combo is tracked as a physical unit (it has its own cantidad and
// origin/destination warehouses) — productos inside the combo inherit those
// warehouses and do not carry their own.
type Combo struct {
	id             uuid.UUID
	nombre         string
	precios        MontoSnapshot
	cantidad       decimal.Decimal
	almacenOrigen  int
	almacenDestino int
	audit          audit.Auditable
}

// NewComboParams carries the inputs to newCombo.
type NewComboParams struct {
	ID             uuid.UUID
	Nombre         string
	Precios        MontoSnapshot
	Cantidad       decimal.Decimal
	AlmacenOrigen  int
	AlmacenDestino int
	CreatedBy      uuid.UUID
	Now            time.Time
}

// newCombo validates and constructs a Combo. Package-private: external
// callers must go through the aggregate root.
func newCombo(p NewComboParams) (*Combo, error) {
	nombre := strings.TrimSpace(p.Nombre)
	if nombre == "" {
		return nil, ErrComboNombreRequerido
	}
	if len(nombre) > maxComboNombreLength {
		return nil, ErrComboNombreDemasiadoLargo
	}
	if err := validateSafeChars(nombre); err != nil {
		return nil, err
	}
	if p.Cantidad.Sign() <= 0 {
		return nil, ErrComboCantidadNoPositiva
	}
	if err := validateCantidadScale(p.Cantidad); err != nil {
		return nil, err
	}
	if err := validateMontoSnapshotScale(p.Precios); err != nil {
		return nil, err
	}
	if p.AlmacenOrigen <= 0 {
		return nil, ErrComboAlmacenOrigenRequerido
	}
	if p.AlmacenDestino <= 0 {
		return nil, ErrComboAlmacenDestinoRequerido
	}
	// NOTE: origen == destino is intentionally allowed — the client's
	// almacen_destino is vestigial; the real traspaso destination is the
	// configured almacén de exhibición at apply time. See validateProductoAlmacenes.
	return &Combo{
		id:             p.ID,
		nombre:         nombre,
		precios:        p.Precios,
		cantidad:       p.Cantidad,
		almacenOrigen:  p.AlmacenOrigen,
		almacenDestino: p.AlmacenDestino,
		audit:          audit.NewAuditable(p.Now, p.CreatedBy),
	}, nil
}

// HydrateComboParams carries the persisted shape of a Combo for repository
// reconstruction.
type HydrateComboParams struct {
	ID             uuid.UUID
	Nombre         string
	Precios        MontoSnapshot
	Cantidad       decimal.Decimal
	AlmacenOrigen  int
	AlmacenDestino int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CreatedBy      uuid.UUID
	UpdatedBy      uuid.UUID
}

// HydrateCombo rebuilds a Combo from persistence without validation.
func HydrateCombo(p HydrateComboParams) *Combo {
	return &Combo{
		id:             p.ID,
		nombre:         p.Nombre,
		precios:        p.Precios,
		cantidad:       p.Cantidad,
		almacenOrigen:  p.AlmacenOrigen,
		almacenDestino: p.AlmacenDestino,
		audit:          audit.HydrateAuditable(p.CreatedAt, p.UpdatedAt, p.CreatedBy, p.UpdatedBy),
	}
}

// ID returns the combo's primary key.
func (c *Combo) ID() uuid.UUID { return c.id }

// Nombre returns the combo's display name.
func (c *Combo) Nombre() string { return c.nombre }

// Precios returns the three-price snapshot for the combo.
func (c *Combo) Precios() MontoSnapshot { return c.precios }

// Cantidad returns the combo's quantity (number of physical bundles).
func (c *Combo) Cantidad() decimal.Decimal { return c.cantidad }

// AlmacenOrigen returns the origin warehouse for the combo.
func (c *Combo) AlmacenOrigen() int { return c.almacenOrigen }

// AlmacenDestino returns the destination warehouse for the combo.
func (c *Combo) AlmacenDestino() int { return c.almacenDestino }

// Audit returns a copy of the combo's audit subrecord.
func (c *Combo) Audit() audit.Auditable { return c.audit }
