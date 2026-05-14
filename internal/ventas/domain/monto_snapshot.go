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

// Equals reports whether two MontoSnapshot values are equal.
func (m MontoSnapshot) Equals(other MontoSnapshot) bool {
	return m.anual.Equal(other.anual) &&
		m.cortoPlazo.Equal(other.cortoPlazo) &&
		m.contado.Equal(other.contado)
}
