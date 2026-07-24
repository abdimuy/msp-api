//nolint:misspell // Spanish domain vocabulary (enganche, parcialidad) by project convention.
package domain

import (
	"errors"

	"github.com/shopspring/decimal"
)

// Errores del cálculo de plan de pago.
var (
	// ErrPrecioInvalido: el precio del producto debe ser positivo.
	ErrPrecioInvalido = errors.New("reactivacion: el precio del producto debe ser positivo")
	// ErrPeriodosInvalido: el número de parcialidades debe ser positivo.
	ErrPeriodosInvalido = errors.New("reactivacion: el número de parcialidades debe ser positivo")
)

// enganchePct is the fixed down-payment fraction (10% of the price) per the
// decided pricing rule. It is intentionally a constant, not configurable: the
// copiloto never negotiates terms (see §2 of the interaction-design spec).
var enganchePct = decimal.RequireFromString("0.10")

// cincuenta is the rounding unit — every stated amount is a multiple of $50.
var cincuenta = decimal.NewFromInt(50)

// PlanPago is the immutable, DETERMINISTIC payment plan the copiloto may
// enunciate for a suggested product. It is computed in Go (never by the LLM —
// the LLM must not do arithmetic) from the product price: enganche = 10% of
// price rounded to $50, parcialidad = (price − enganche) / periodos rounded to
// $50. Every amount is a whole multiple of $50.
type PlanPago struct {
	enganche    decimal.Decimal
	parcialidad decimal.Decimal
	periodos    int
	cadencia    string // "semanal" | "quincenal" | "mensual"
}

// Enganche returns the down payment (a $50 multiple).
func (p PlanPago) Enganche() decimal.Decimal { return p.enganche }

// Parcialidad returns the per-period installment (a $50 multiple).
func (p PlanPago) Parcialidad() decimal.Decimal { return p.parcialidad }

// Periodos returns the number of installments.
func (p PlanPago) Periodos() int { return p.periodos }

// Cadencia returns the payment cadence label ("semanal"/"quincenal"/"mensual").
func (p PlanPago) Cadencia() string { return p.cadencia }

// CalcularPlanPago computes the deterministic plan from precio over periodos
// installments at the given cadencia. Rules (fixed, non-negotiable):
//   - enganche = redondear_50(0.10 × precio)
//   - parcialidad = redondear_50((precio − enganche) / periodos)
//
// precio must be positive and periodos must be > 0.
func CalcularPlanPago(precio decimal.Decimal, periodos int, cadencia string) (PlanPago, error) {
	if !precio.IsPositive() {
		return PlanPago{}, ErrPrecioInvalido
	}
	if periodos <= 0 {
		return PlanPago{}, ErrPeriodosInvalido
	}
	enganche := redondear50(precio.Mul(enganchePct))
	parcialidad := redondear50(precio.Sub(enganche).Div(decimal.NewFromInt(int64(periodos))))
	return PlanPago{
		enganche:    enganche,
		parcialidad: parcialidad,
		periodos:    periodos,
		cadencia:    cadencia,
	}, nil
}

// redondear50 rounds x to the nearest multiple of 50 (half away from zero).
func redondear50(x decimal.Decimal) decimal.Decimal {
	return x.Div(cincuenta).Round(0).Mul(cincuenta)
}
