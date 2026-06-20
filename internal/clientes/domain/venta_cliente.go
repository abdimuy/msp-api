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
	doctoPVID      int
	clienteID      int
	fecha          time.Time // UTC
	folio          string
	tipo           TipoVenta
	total          decimal.Decimal
	saldoVenta     decimal.Decimal
	numPagos       int
	hora           string // "HH:MM:SS" wall-clock local time-of-day (not a UTC instant)
	almacen        string // ALMACENES.NOMBRE (UTF-8/NFC decoded from Win1252)
	primerArticulo string // ARTICULOS.NOMBRE of first J/N line (UTF-8/NFC decoded from Win1252)
	numArticulos   int    // count of J/N lines in DOCTOS_PV_DET
}

// HydrateVentaClienteParams holds all fields needed to reconstruct a
// VentaCliente from persisted Microsip rows. Used exclusively by the repository.
type HydrateVentaClienteParams struct {
	DoctoPVID      int
	ClienteID      int
	Fecha          time.Time // caller must supply UTC
	Folio          string
	Tipo           TipoVenta
	Total          decimal.Decimal
	SaldoVenta     decimal.Decimal
	NumPagos       int
	Hora           string // "HH:MM:SS" wall-clock local time-of-day display string
	Almacen        string // almacén name (UTF-8)
	PrimerArticulo string // first J/N article name (UTF-8)
	NumArticulos   int    // count of J/N lines
}

// HydrateVentaCliente reconstructs a VentaCliente from Microsip persistence
// with zero validation. Called only from the repository layer.
// The Fecha field is normalized to UTC inside this constructor.
func HydrateVentaCliente(p HydrateVentaClienteParams) *VentaCliente {
	return &VentaCliente{
		doctoPVID:      p.DoctoPVID,
		clienteID:      p.ClienteID,
		fecha:          p.Fecha.UTC(),
		folio:          p.Folio,
		tipo:           p.Tipo,
		total:          p.Total,
		saldoVenta:     p.SaldoVenta,
		numPagos:       p.NumPagos,
		hora:           p.Hora,
		almacen:        p.Almacen,
		primerArticulo: p.PrimerArticulo,
		numArticulos:   p.NumArticulos,
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

// Tipo returns the payment modality (CONTADO or CREDITO). The repository
// derives this from DOCTOS_PV_COBROS.FORMA_COBRO_ID (67=contado, 71=credito).
func (v *VentaCliente) Tipo() TipoVenta { return v.tipo }

// Total returns the net invoice amount (DOCTOS_PV.IMPORTE_NETO).
func (v *VentaCliente) Total() decimal.Decimal { return v.total }

// SaldoVenta returns the outstanding balance remaining on this sale.
func (v *VentaCliente) SaldoVenta() decimal.Decimal { return v.saldoVenta }

// NumPagos returns the count of payments applied to this sale.
func (v *VentaCliente) NumPagos() int { return v.numPagos }

// Hora returns the wall-clock local time-of-day of the sale as a display string
// "HH:MM:SS". This is NOT a UTC instant — it is the raw DOCTOS_PV.HORA column
// from Microsip, which stores the local time of sale entry. Expose as a display
// string only; do not parse it as a timezone-aware instant.
func (v *VentaCliente) Hora() string { return v.hora }

// Almacen returns the warehouse/branch name where the sale was made.
// Sourced from ALMACENES.NOMBRE via DOCTOS_PV.ALMACEN_ID.
func (v *VentaCliente) Almacen() string { return v.almacen }

// PrimerArticulo returns the name of the first J/N line item in the sale.
// Empty when the sale has no J/N detail lines.
func (v *VentaCliente) PrimerArticulo() string { return v.primerArticulo }

// NumArticulos returns the count of J/N (normal + kit-header) lines in the sale.
func (v *VentaCliente) NumArticulos() int { return v.numArticulos }
