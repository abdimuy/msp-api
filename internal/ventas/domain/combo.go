//nolint:misspell // domain vocabulary is Spanish (productos, etc.) per project convention.
package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// maxComboNombreLength is the byte width of the combo name column.
const maxComboNombreLength = 200

// Combo is a bundle of productos sold together under a single named offer.
// It is a child entity of the Venta aggregate root; its lifecycle is fully
// managed through Venta — external callers obtain Combos via Venta.Combos().
type Combo struct {
	id      uuid.UUID
	nombre  string
	precios MontoSnapshot
	audit   audit.Auditable
}

// NewComboParams carries the inputs to newCombo.
type NewComboParams struct {
	ID        uuid.UUID
	Nombre    string
	Precios   MontoSnapshot
	CreatedBy uuid.UUID
	Now       time.Time
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
	return &Combo{
		id:      p.ID,
		nombre:  nombre,
		precios: p.Precios,
		audit:   audit.NewAuditable(p.Now, p.CreatedBy),
	}, nil
}

// HydrateComboParams carries the persisted shape of a Combo for repository
// reconstruction.
type HydrateComboParams struct {
	ID        uuid.UUID
	Nombre    string
	Precios   MontoSnapshot
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy uuid.UUID
	UpdatedBy uuid.UUID
}

// HydrateCombo rebuilds a Combo from persistence without validation.
func HydrateCombo(p HydrateComboParams) *Combo {
	return &Combo{
		id:      p.ID,
		nombre:  p.Nombre,
		precios: p.Precios,
		audit:   audit.HydrateAuditable(p.CreatedAt, p.UpdatedAt, p.CreatedBy, p.UpdatedBy),
	}
}

// ID returns the combo's primary key.
func (c *Combo) ID() uuid.UUID { return c.id }

// Nombre returns the combo's display name.
func (c *Combo) Nombre() string { return c.nombre }

// Precios returns the three-price snapshot for the combo.
func (c *Combo) Precios() MontoSnapshot { return c.precios }

// Audit returns a copy of the combo's audit subrecord.
func (c *Combo) Audit() audit.Auditable { return c.audit }
