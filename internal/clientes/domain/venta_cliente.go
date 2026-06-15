//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// VentaCliente is a read-only projection of a single sale header from the
// native Microsip table DOCTOS_PV, augmented with credit and payment data from
// DOCTOS_PV_COBROS (for tipo) and the balance cache. The module never mutates
// sales; Microsip owns them.
//
// Deliberately deviates from the Type A/B/C entity standards:
//   - No audit embed (read-only Microsip projection).
//   - No Crear constructor and no domain events.
//   - HydrateVentaCliente is the sole constructor; used exclusively by the repository.
type VentaCliente struct {
	doctoPVID  int
	clienteID  int
	fecha      time.Time // UTC
	folio      string
	tipo       string // "contado" or "credito"; derived by repo from FORMAS_COBRO; no enum yet (Task B2)
	total      decimal.Decimal
	saldoVenta decimal.Decimal
	numPagos   int
}

// HydrateVentaClienteParams holds all fields needed to reconstruct a
// VentaCliente from persisted Microsip rows. Used exclusively by the repository.
type HydrateVentaClienteParams struct {
	DoctoPVID  int
	ClienteID  int
	Fecha      time.Time // caller must supply UTC
	Folio      string
	Tipo       string
	Total      decimal.Decimal
	SaldoVenta decimal.Decimal
	NumPagos   int
}

// HydrateVentaCliente reconstructs a VentaCliente from Microsip persistence
// with zero validation. Called only from the repository layer.
// The Fecha field is normalized to UTC inside this constructor.
func HydrateVentaCliente(p HydrateVentaClienteParams) *VentaCliente {
	return &VentaCliente{
		doctoPVID:  p.DoctoPVID,
		clienteID:  p.ClienteID,
		fecha:      p.Fecha.UTC(),
		folio:      p.Folio,
		tipo:       p.Tipo,
		total:      p.Total,
		saldoVenta: p.SaldoVenta,
		numPagos:   p.NumPagos,
	}
}

// ─── Getters ──────────────────────────────────────────────────────────────────

// DoctoPVID returns the Microsip primary key for this sale document.
func (v *VentaCliente) DoctoPVID() int { return v.doctoPVID }

// ClienteID returns the Microsip cliente ID this sale belongs to.
func (v *VentaCliente) ClienteID() int { return v.clienteID }

// Fecha returns the UTC timestamp of the sale.
func (v *VentaCliente) Fecha() time.Time { return v.fecha }

// Folio returns the document folio number as displayed in Microsip.
func (v *VentaCliente) Folio() string { return v.folio }

// Tipo returns the payment modality: "contado" or "credito". The repository
// derives this from DOCTOS_PV_COBROS.FORMA_COBRO_ID (67=contado, 71=credito).
// An enum/value-object representation is deferred to Task B2.
func (v *VentaCliente) Tipo() string { return v.tipo }

// Total returns the net invoice amount (DOCTOS_PV.IMPORTE_NETO).
func (v *VentaCliente) Total() decimal.Decimal { return v.total }

// SaldoVenta returns the outstanding balance remaining on this sale.
func (v *VentaCliente) SaldoVenta() decimal.Decimal { return v.saldoVenta }

// NumPagos returns the count of payments applied to this sale.
func (v *VentaCliente) NumPagos() int { return v.numPagos }
