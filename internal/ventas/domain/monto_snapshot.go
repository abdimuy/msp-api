//nolint:misspell // domain vocabulary is Spanish (anual, etc.) per project convention.
package domain

import "github.com/shopspring/decimal"

// MontoSnapshot captures the three pricing snapshots a venta carries:
// anual, corto plazo, and contado. All are required and must be ≥ 0.
type MontoSnapshot struct {
	anual      decimal.Decimal
	cortoPlazo decimal.Decimal
	contado    decimal.Decimal
}

// NewMontoSnapshot validates and constructs a MontoSnapshot. Every value
// must be non-negative, ≤ MaxMontoVenta, and have ≤ 2 decimal places (the
// declared scale of the NUMERIC(14,2) storage column).
func NewMontoSnapshot(anual, cortoPlazo, contado decimal.Decimal) (MontoSnapshot, error) {
	if anual.Sign() < 0 || cortoPlazo.Sign() < 0 || contado.Sign() < 0 {
		return MontoSnapshot{}, ErrMontoNegativo
	}
	for _, v := range [...]decimal.Decimal{anual, cortoPlazo, contado} {
		if err := validateMontoScale(v); err != nil {
			return MontoSnapshot{}, err
		}
		if err := validateMontoCap(v); err != nil {
			return MontoSnapshot{}, err
		}
	}
	return MontoSnapshot{anual: anual, cortoPlazo: cortoPlazo, contado: contado}, nil
}

// HydrateMontoSnapshot rebuilds a MontoSnapshot from persistence without
// validation.
func HydrateMontoSnapshot(anual, cortoPlazo, contado decimal.Decimal) MontoSnapshot {
	return MontoSnapshot{anual: anual, cortoPlazo: cortoPlazo, contado: contado}
}

// Anual returns the yearly-plan price snapshot.
func (m MontoSnapshot) Anual() decimal.Decimal { return m.anual }

// CortoPlazo returns the short-term-plan price snapshot.
func (m MontoSnapshot) CortoPlazo() decimal.Decimal { return m.cortoPlazo }

// Contado returns the cash price snapshot.
func (m MontoSnapshot) Contado() decimal.Decimal { return m.contado }

// validateMontoTierOrder enforces contado ≤ corto plazo ≤ anual.
//
// The three tiers are the same goods priced by how long the customer takes
// to pay, so cash is always the cheapest and the yearly plan the dearest.
// Any other order cannot be a real price list; it is a capture mistake.
//
// The rule deliberately does NOT live in NewMontoSnapshot: that constructor
// also builds header totals, and it is bypassed entirely by the combo path,
// which reaches the domain through HydrateMontoSnapshot. The two entity
// constructors are the choke point every captured line actually crosses.
//
// violation is the error to return, so a line-level breach stays
// distinguishable from a header-level one in the failed-intent screen.
func validateMontoTierOrder(m MontoSnapshot, violation error) error {
	if m.contado.GreaterThan(m.cortoPlazo) || m.cortoPlazo.GreaterThan(m.anual) {
		return violation
	}
	return nil
}

// Equals reports whether two MontoSnapshot values are equal.
func (m MontoSnapshot) Equals(other MontoSnapshot) bool {
	return m.anual.Equal(other.anual) &&
		m.cortoPlazo.Equal(other.cortoPlazo) &&
		m.contado.Equal(other.contado)
}
