//nolint:misspell // domain vocabulary is Spanish (movimiento, etc.) per project convention.
package domain

// TipoMovimientoSalida represents a stock-out movement (items leave an
// almacen).
const TipoMovimientoSalida = "S"

// TipoMovimientoEntrada represents a stock-in movement (items enter an
// almacen).
const TipoMovimientoEntrada = "E"

// TipoMovimiento is a value object wrapping the movement direction used in
// Microsip DOCTOS_IN records. Only "S" (salida) and "E" (entrada) are valid.
type TipoMovimiento struct{ value string }

// NewTipoMovimiento validates and constructs a TipoMovimiento. Accepts only
// "S" or "E"; rejects anything else with ErrTipoMovimientoInvalido.
func NewTipoMovimiento(s string) (TipoMovimiento, error) {
	if s != TipoMovimientoSalida && s != TipoMovimientoEntrada {
		return TipoMovimiento{}, ErrTipoMovimientoInvalido
	}
	return TipoMovimiento{value: s}, nil
}

// HydrateTipoMovimiento rebuilds a TipoMovimiento from persistence without
// validation. Intended for repository use only.
func HydrateTipoMovimiento(s string) TipoMovimiento { return TipoMovimiento{value: s} }

// Value returns the raw movement type string ("S" or "E").
func (t TipoMovimiento) Value() string { return t.value }

// String returns the movement type string representation.
func (t TipoMovimiento) String() string { return t.value }

// Equals reports whether two TipoMovimiento values are identical.
func (t TipoMovimiento) Equals(other TipoMovimiento) bool { return t.value == other.value }

// IsZero reports whether the TipoMovimiento has its zero value (empty string).
func (t TipoMovimiento) IsZero() bool { return t.value == "" }

// IsSalida reports whether this is a stock-out movement.
func (t TipoMovimiento) IsSalida() bool { return t.value == TipoMovimientoSalida }

// IsEntrada reports whether this is a stock-in movement.
func (t TipoMovimiento) IsEntrada() bool { return t.value == TipoMovimientoEntrada }
