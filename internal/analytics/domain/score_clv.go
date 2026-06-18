package domain

import "github.com/shopspring/decimal"

// MontoCLV is a validated non-negative decimal representing the risk-adjusted
// customer lifetime value in pesos. The app layer computes the value via the
// Gamma-Gamma + BG/BB DET formula and wraps it here. Zero-value = 0 pesos
// (no aplica or brand-new client with no purchase history to project from).
type MontoCLV struct {
	value decimal.Decimal
}

// NewMontoCLV constructs a MontoCLV, returning ErrMontoCLVNegativo if v is negative.
func NewMontoCLV(v decimal.Decimal) (MontoCLV, error) {
	if v.IsNegative() {
		return MontoCLV{}, ErrMontoCLVNegativo
	}
	return MontoCLV{value: v}, nil
}

// Decimal returns the underlying decimal value (pesos, 2 d.p.).
func (m MontoCLV) Decimal() decimal.Decimal { return m.value }
