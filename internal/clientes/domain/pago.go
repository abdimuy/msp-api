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
}

// HydratePagoParams holds all fields needed to reconstruct a Pago from
// persisted Microsip rows. Used exclusively by the repository.
type HydratePagoParams struct {
	DoctoCCID      int
	Fecha          time.Time // caller must supply UTC
	Importe        decimal.Decimal
	FormaCobro     string
	AplicaACargoID int
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
