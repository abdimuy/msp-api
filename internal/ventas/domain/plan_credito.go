package domain

import "github.com/shopspring/decimal"

// PlanCredito models the credit plan attached to a CREDITO venta. All fields
// are required and validated by the constructor.
type PlanCredito struct {
	plazoMeses  int
	enganche    decimal.Decimal
	parcialidad decimal.Decimal
	frecPago    FrecPago
}

// NewPlanCredito validates and constructs a PlanCredito.
func NewPlanCredito(
	plazoMeses int,
	enganche decimal.Decimal,
	parcialidad decimal.Decimal,
	frecPago FrecPago,
) (PlanCredito, error) {
	if plazoMeses <= 0 {
		return PlanCredito{}, ErrPlazoNoPositivo
	}
	if enganche.Sign() < 0 {
		return PlanCredito{}, ErrMontoNegativo
	}
	if parcialidad.Sign() < 0 {
		return PlanCredito{}, ErrMontoNegativo
	}
	if !frecPago.IsValid() {
		return PlanCredito{}, ErrFrecPagoInvalida
	}
	return PlanCredito{
		plazoMeses:  plazoMeses,
		enganche:    enganche,
		parcialidad: parcialidad,
		frecPago:    frecPago,
	}, nil
}

// HydratePlanCredito rebuilds a PlanCredito from persistence without
// validation.
func HydratePlanCredito(
	plazoMeses int,
	enganche decimal.Decimal,
	parcialidad decimal.Decimal,
	frecPago FrecPago,
) PlanCredito {
	return PlanCredito{
		plazoMeses:  plazoMeses,
		enganche:    enganche,
		parcialidad: parcialidad,
		frecPago:    frecPago,
	}
}

// PlazoMeses returns the credit term in months.
func (p PlanCredito) PlazoMeses() int { return p.plazoMeses }

// Enganche returns the down payment amount.
func (p PlanCredito) Enganche() decimal.Decimal { return p.enganche }

// Parcialidad returns the installment amount.
func (p PlanCredito) Parcialidad() decimal.Decimal { return p.parcialidad }

// FrecPago returns the payment frequency.
func (p PlanCredito) FrecPago() FrecPago { return p.frecPago }

// Equals reports whether two PlanCredito values are identical.
func (p PlanCredito) Equals(other PlanCredito) bool {
	return p.plazoMeses == other.plazoMeses &&
		p.enganche.Equal(other.enganche) &&
		p.parcialidad.Equal(other.parcialidad) &&
		p.frecPago == other.frecPago
}
