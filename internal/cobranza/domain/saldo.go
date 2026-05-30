//nolint:misspell // domain vocabulary is Spanish (conceptos, enganches, condonaciones, etc.) per project convention.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Saldo is a read-only projection of a MSP_SALDOS_VENTAS row. It is a value
// object: the materialized cache is the source of truth (computed by Microsip
// triggers); the entity merely carries the values into Go so the app layer can
// reason about them without issuing additional DB queries.
//
// All fields are private; callers use the getter methods.
type Saldo struct {
	doctoCCID      int
	doctoPVID      *int // nil when the cargo did not originate from a PV document
	clienteID      int
	zonaClienteID  *int // nil when the cliente has no zona assigned
	folio          string
	fechaCargo     time.Time
	precioTotal    decimal.Decimal
	totalImporte   decimal.Decimal // cobros (conceptos 87327, 155, 11)
	impteRest      decimal.Decimal // otros (enganches viejos, condonaciones, fugas)
	numPagos       int
	fechaUltPago   *time.Time
	saldo          decimal.Decimal // = precioTotal - totalImporte - impteRest
	cargoCancelado bool
	updatedAt      time.Time
}

// HydrateSaldoParams carries the persisted shape of a Saldo for repository
// reconstruction. Fields are assigned directly without recomputation;
// repositories must guarantee the values were written by the Microsip trigger.
type HydrateSaldoParams struct {
	DoctoCCID      int
	DoctoPVID      *int
	ClienteID      int
	ZonaClienteID  *int
	Folio          string
	FechaCargo     time.Time
	PrecioTotal    decimal.Decimal
	TotalImporte   decimal.Decimal
	ImpteRest      decimal.Decimal
	Saldo          decimal.Decimal
	NumPagos       int
	FechaUltPago   *time.Time
	CargoCancelado bool
	UpdatedAt      time.Time
}

// HydrateSaldo reconstructs an existing Saldo from persistent storage. It does
// NOT recompute or validate fields against each other — the cache row is the
// source of truth. Used by repos when reading MSP_SALDOS_VENTAS rows.
func HydrateSaldo(p HydrateSaldoParams) Saldo {
	return Saldo{
		doctoCCID:      p.DoctoCCID,
		doctoPVID:      p.DoctoPVID,
		clienteID:      p.ClienteID,
		zonaClienteID:  p.ZonaClienteID,
		folio:          p.Folio,
		fechaCargo:     p.FechaCargo,
		precioTotal:    p.PrecioTotal,
		totalImporte:   p.TotalImporte,
		impteRest:      p.ImpteRest,
		numPagos:       p.NumPagos,
		fechaUltPago:   p.FechaUltPago,
		saldo:          p.Saldo,
		cargoCancelado: p.CargoCancelado,
		updatedAt:      p.UpdatedAt,
	}
}

// ─── Getters ────────────────────────────────────────────────────────────────

// DoctoCCID returns the primary key of the cargo in DOCTOS_CC.
func (s Saldo) DoctoCCID() int { return s.doctoCCID }

// DoctoPVID returns the originating PV document ID, or nil when the cargo
// was created independently of a PV document.
func (s Saldo) DoctoPVID() *int { return s.doctoPVID }

// ClienteID returns the Microsip cliente ID.
func (s Saldo) ClienteID() int { return s.clienteID }

// ZonaClienteID returns the zona assigned to the cliente, or nil when none.
func (s Saldo) ZonaClienteID() *int { return s.zonaClienteID }

// Folio returns the document folio string.
func (s Saldo) Folio() string { return s.folio }

// FechaCargo returns the date the cargo was created in Microsip.
func (s Saldo) FechaCargo() time.Time { return s.fechaCargo }

// PrecioTotal returns the original total price of the cargo.
func (s Saldo) PrecioTotal() decimal.Decimal { return s.precioTotal }

// TotalImporte returns the sum of cobros (payment concepts 87327, 155, 11).
func (s Saldo) TotalImporte() decimal.Decimal { return s.totalImporte }

// ImpteRest returns the sum of other deductions (enganches viejos,
// condonaciones, fugas).
func (s Saldo) ImpteRest() decimal.Decimal { return s.impteRest }

// NumPagos returns how many payment transactions have been applied.
func (s Saldo) NumPagos() int { return s.numPagos }

// FechaUltPago returns the date of the most recent payment, or nil when no
// payments have been applied.
func (s Saldo) FechaUltPago() *time.Time { return s.fechaUltPago }

// Saldo returns the outstanding balance (precioTotal - totalImporte - impteRest).
func (s Saldo) Saldo() decimal.Decimal { return s.saldo }

// CargoCancelado reports whether the cargo has been cancelled in Microsip.
func (s Saldo) CargoCancelado() bool { return s.cargoCancelado }

// UpdatedAt returns the timestamp of the last cache refresh.
func (s Saldo) UpdatedAt() time.Time { return s.updatedAt }
