package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// Vendedor is the linkage between a venta and one of the usuarios that sold
// it, with a snapshot of email/nombre captured at the moment of sale.
type Vendedor struct {
	id       uuid.UUID
	snapshot VendedorSnapshot
	audit    audit.Auditable
}

// NewVendedorParams carries the inputs to newVendedor.
type NewVendedorParams struct {
	ID        uuid.UUID
	Snapshot  VendedorSnapshot
	CreatedBy uuid.UUID
	Now       time.Time
}

// newVendedor constructs a Vendedor. Package-private: external callers must
// pass the snapshot through Venta.
func newVendedor(p NewVendedorParams) *Vendedor {
	return &Vendedor{
		id:       p.ID,
		snapshot: p.Snapshot,
		audit:    audit.NewAuditable(p.Now, p.CreatedBy),
	}
}

// HydrateVendedorParams carries the persisted shape of a Vendedor.
type HydrateVendedorParams struct {
	ID        uuid.UUID
	Snapshot  VendedorSnapshot
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy uuid.UUID
	UpdatedBy uuid.UUID
}

// HydrateVendedor rebuilds a Vendedor from persistence without validation.
func HydrateVendedor(p HydrateVendedorParams) *Vendedor {
	return &Vendedor{
		id:       p.ID,
		snapshot: p.Snapshot,
		audit:    audit.HydrateAuditable(p.CreatedAt, p.UpdatedAt, p.CreatedBy, p.UpdatedBy),
	}
}

// ID returns the vendedor row ID.
func (v *Vendedor) ID() uuid.UUID { return v.id }

// Snapshot returns the vendedor's snapshot data.
func (v *Vendedor) Snapshot() VendedorSnapshot { return v.snapshot }

// UsuarioID is a convenience accessor for the underlying usuario UUID.
func (v *Vendedor) UsuarioID() uuid.UUID { return v.snapshot.UsuarioID() }

// Audit returns a copy of the vendedor's audit subrecord.
func (v *Vendedor) Audit() audit.Auditable { return v.audit }
