//nolint:misspell // domain vocabulary is Spanish (cantidad, etc.) per project convention.
package domain

import "github.com/shopspring/decimal"

// Cantidad is a value object wrapping a strictly positive decimal.Decimal with
// at most 4 decimal places. It is used for traspaso detalle line quantities.
type Cantidad struct{ value decimal.Decimal }

// NewCantidad validates and constructs a Cantidad. Rejects values ≤ 0 with
// ErrCantidadInvalida and values with more than 4 decimal places with
// ErrCantidadEscalaInvalida.
func NewCantidad(d decimal.Decimal) (Cantidad, error) {
	if d.Sign() <= 0 {
		return Cantidad{}, ErrCantidadInvalida
	}
	if err := validateCantidadScale(d); err != nil {
		return Cantidad{}, err
	}
	return Cantidad{value: d}, nil
}

// HydrateCantidad rebuilds a Cantidad from persistence without validation.
// Intended for repository use only.
func HydrateCantidad(d decimal.Decimal) Cantidad { return Cantidad{value: d} }

// Value returns the underlying decimal value.
func (c Cantidad) Value() decimal.Decimal { return c.value }

// String returns the decimal string representation.
func (c Cantidad) String() string { return c.value.String() }

// Equals reports whether two Cantidad values are numerically equal.
func (c Cantidad) Equals(other Cantidad) bool { return c.value.Equal(other.value) }

// IsZero reports whether the Cantidad has its zero value (uninitialized).
func (c Cantidad) IsZero() bool { return c.value.IsZero() }
