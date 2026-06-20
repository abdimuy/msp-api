//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Pago is a read-only projection of a single payment from the native Microsip
// tables DOCTOS_CC (naturaleza 'R') and IMPORTES_DOCTOS_CC. The module never
// mutates payments; Microsip owns them.
//
// Deliberately deviates from the Type A/B/C entity standards:
//   - No audit embed (read-only Microsip projection).
//   - No Crear constructor and no domain events.
//   - HydratePago is the sole constructor; used exclusively by the repository.
type Pago struct {
	doctoCCID      int
	fecha          time.Time // UTC
	importe        decimal.Decimal
	formaCobro     string
	aplicaACargoID int // 0 when the target cargo DOCTO_CC_ID is unknown
	conceptoCCID   int
	concepto       string    // UTF-8/NFC display name decoded from Win1252
	categoria      Categoria // server-derived from conceptoCCID
	cobrador       string    // UTF-8/NFC; COBRADORES.NOMBRE or DOCTOS_CC.DESCRIPCION fallback
}

// HydratePagoParams holds all fields needed to reconstruct a Pago from
// persisted Microsip rows. Used exclusively by the repository.
type HydratePagoParams struct {
	DoctoCCID      int
	Fecha          time.Time // caller must supply UTC
	Importe        decimal.Decimal
	FormaCobro     string
	AplicaACargoID int
	ConceptoCCID   int
	Concepto       string    // UTF-8/NFC display name
	Categoria      Categoria // must be set via ClasificarConcepto
	Cobrador       string    // UTF-8/NFC; empty when no cobrador applies
}

// HydratePago reconstructs a Pago from Microsip persistence with zero
// validation. Called only from the repository layer.
// The Fecha field is normalized to UTC inside this constructor.
func HydratePago(p HydratePagoParams) *Pago {
	return &Pago{
		doctoCCID:      p.DoctoCCID,
		fecha:          p.Fecha.UTC(),
		importe:        p.Importe,
		formaCobro:     p.FormaCobro,
		aplicaACargoID: p.AplicaACargoID,
		conceptoCCID:   p.ConceptoCCID,
		concepto:       p.Concepto,
		categoria:      p.Categoria,
		cobrador:       p.Cobrador,
	}
}

// ─── Getters ──────────────────────────────────────────────────────────────────

// DoctoCCID returns the Microsip primary key of the payment document (DOCTOS_CC).
func (p *Pago) DoctoCCID() int { return p.doctoCCID }

// Fecha returns the UTC timestamp of the payment.
func (p *Pago) Fecha() time.Time { return p.fecha }

// Importe returns the real payment amount from IMPORTES_DOCTOS_CC.IMPORTE.
func (p *Pago) Importe() decimal.Decimal { return p.importe }

// FormaCobro returns the payment method name from FORMAS_COBRO.NOMBRE.
func (p *Pago) FormaCobro() string { return p.formaCobro }

// AplicaACargoID returns the DOCTO_CC_ID of the cargo (debt document) this
// payment is applied against. Returns 0 when the target cargo is unknown.
func (p *Pago) AplicaACargoID() int { return p.aplicaACargoID }

// ConceptoCCID returns the CONCEPTO_CC_ID from DOCTOS_CC.
func (p *Pago) ConceptoCCID() int { return p.conceptoCCID }

// Concepto returns the display name of the concepto (UTF-8/NFC).
func (p *Pago) Concepto() string { return p.concepto }

// Categoria returns the server-derived category for this payment movement.
func (p *Pago) Categoria() Categoria { return p.categoria }

// Cobrador returns the cobrador name (UTF-8/NFC), or empty when not applicable.
func (p *Pago) Cobrador() string { return p.cobrador }
