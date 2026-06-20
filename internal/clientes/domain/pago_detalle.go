//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// PagoDetalle is the rich detail projection of a single payment document
// (DOCTOS_CC abono). It aggregates amounts across IMPORTES_DOCTOS_CC rows,
// resolves the primary cargo and the linked PV sale, and merges MSP-primary
// enrichment when available.
//
// Read-only Microsip projection: no audit embed, no domain events.
// HydratePagoDetalle is the sole constructor.
type PagoDetalle struct {
	doctoCCID      int
	fecha          time.Time // UTC
	folio          string
	cancelado      bool
	aplicado       bool
	importe        decimal.Decimal
	iva            decimal.Decimal
	conceptoCCID   int
	concepto       string // UTF-8/NFC
	categoria      Categoria
	cobradorID     int
	cobrador       string // UTF-8/NFC
	formaCObroID   int
	formaCobro     string // UTF-8/NFC
	referencia     string // UTF-8/NFC
	aplicaACargoID int
	saldoCargo     *decimal.Decimal // nil when absent from cache
	doctoPVID      int
	lat            *decimal.Decimal // nil when absent
	lon            *decimal.Decimal // nil when absent
	recibidoAt     time.Time        // zero when absent
	aplicadoAt     time.Time        // zero when absent
	origen         string           // "app" | "microsip"
}

// HydratePagoDetalleParams holds all fields needed to reconstruct a PagoDetalle.
type HydratePagoDetalleParams struct {
	DoctoCCID      int
	Fecha          time.Time
	Folio          string
	Cancelado      bool
	Aplicado       bool
	Importe        decimal.Decimal
	IVA            decimal.Decimal
	ConceptoCCID   int
	Concepto       string
	Categoria      Categoria
	CobradorID     int
	Cobrador       string
	FormaCobroID   int
	FormaCobro     string
	Referencia     string
	AplicaACargoID int
	SaldoCargo     *decimal.Decimal
	DoctoPVID      int
	Lat            *decimal.Decimal
	Lon            *decimal.Decimal
	RecibidoAt     time.Time
	AplicadoAt     time.Time
	Origen         string
}

// HydratePagoDetalle reconstructs a PagoDetalle from persistence with zero validation.
// Called only from the repository layer. Fecha is normalized to UTC.
func HydratePagoDetalle(p HydratePagoDetalleParams) PagoDetalle {
	return PagoDetalle{
		doctoCCID:      p.DoctoCCID,
		fecha:          p.Fecha.UTC(),
		folio:          p.Folio,
		cancelado:      p.Cancelado,
		aplicado:       p.Aplicado,
		importe:        p.Importe,
		iva:            p.IVA,
		conceptoCCID:   p.ConceptoCCID,
		concepto:       p.Concepto,
		categoria:      p.Categoria,
		cobradorID:     p.CobradorID,
		cobrador:       p.Cobrador,
		formaCObroID:   p.FormaCobroID,
		formaCobro:     p.FormaCobro,
		referencia:     p.Referencia,
		aplicaACargoID: p.AplicaACargoID,
		saldoCargo:     p.SaldoCargo,
		doctoPVID:      p.DoctoPVID,
		lat:            p.Lat,
		lon:            p.Lon,
		recibidoAt:     p.RecibidoAt,
		aplicadoAt:     p.AplicadoAt,
		origen:         p.Origen,
	}
}

// ─── Getters ──────────────────────────────────────────────────────────────────

// DoctoCCID returns the Microsip primary key of the abono document (DOCTOS_CC).
func (p PagoDetalle) DoctoCCID() int { return p.doctoCCID }

// Fecha returns the UTC document date of the abono.
func (p PagoDetalle) Fecha() time.Time { return p.fecha }

// Folio returns the document folio string.
func (p PagoDetalle) Folio() string { return p.folio }

// Cancelado returns true when the document was cancelled in Microsip.
func (p PagoDetalle) Cancelado() bool { return p.cancelado }

// Aplicado returns true when the payment has been applied.
func (p PagoDetalle) Aplicado() bool { return p.aplicado }

// Importe returns the gross payment amount (IMPORTE + IMPUESTO) from IMPORTES_DOCTOS_CC.
func (p PagoDetalle) Importe() decimal.Decimal { return p.importe }

// IVA returns the tax portion of the payment amount.
func (p PagoDetalle) IVA() decimal.Decimal { return p.iva }

// ConceptoCCID returns the CONCEPTO_CC_ID from DOCTOS_CC.
func (p PagoDetalle) ConceptoCCID() int { return p.conceptoCCID }

// Concepto returns the display name of the concepto (UTF-8/NFC).
func (p PagoDetalle) Concepto() string { return p.concepto }

// Categoria returns the server-derived payment category.
func (p PagoDetalle) Categoria() Categoria { return p.categoria }

// CobradorID returns the cobrador ID; 0 when absent.
func (p PagoDetalle) CobradorID() int { return p.cobradorID }

// Cobrador returns the cobrador display name (UTF-8/NFC).
func (p PagoDetalle) Cobrador() string { return p.cobrador }

// FormaCobroID returns the payment method ID; 0 when absent.
func (p PagoDetalle) FormaCobroID() int { return p.formaCObroID }

// FormaCobro returns the payment method name (UTF-8/NFC).
func (p PagoDetalle) FormaCobro() string { return p.formaCobro }

// Referencia returns the payment reference string (UTF-8/NFC).
func (p PagoDetalle) Referencia() string { return p.referencia }

// AplicaACargoID returns the DOCTO_CC_ID of the cargo (debt document) this payment applies to.
func (p PagoDetalle) AplicaACargoID() int { return p.aplicaACargoID }

// SaldoCargo returns the outstanding balance of the associated cargo from
// MSP_SALDOS_VENTAS; nil when the cargo is not in the balance cache.
func (p PagoDetalle) SaldoCargo() *decimal.Decimal { return p.saldoCargo }

// DoctoPVID returns the DOCTO_PV_ID of the originating sale; 0 when not resolvable.
func (p PagoDetalle) DoctoPVID() int { return p.doctoPVID }

// Lat returns the GPS latitude of the payment location; nil when unavailable.
func (p PagoDetalle) Lat() *decimal.Decimal { return p.lat }

// Lon returns the GPS longitude of the payment location; nil when unavailable.
func (p PagoDetalle) Lon() *decimal.Decimal { return p.lon }

// RecibidoAt returns the app reception timestamp; zero when the payment is native Microsip.
func (p PagoDetalle) RecibidoAt() time.Time { return p.recibidoAt }

// AplicadoAt returns the app application timestamp; zero when the payment is native Microsip.
func (p PagoDetalle) AplicadoAt() time.Time { return p.aplicadoAt }

// Origen returns the data origin: "app" when MSP_PAGOS_RECIBIDOS has a record,
// "microsip" otherwise.
func (p PagoDetalle) Origen() string { return p.origen }
