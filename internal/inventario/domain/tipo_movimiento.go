//nolint:misspell // domain vocabulary is Spanish (movimiento, etc.) per project convention.
package domain

// TipoMovimiento is a value object wrapping the movement direction used in
// Microsip DOCTOS_IN records. Only "S" (salida) and "E" (entrada) are valid.
//
// Enum VO of Categoría 1 per docs/module-standards/02-value-objects-errors.md:
// string-backed named type with exactly Parse/IsValid/String.
type TipoMovimiento string

// TipoMovimiento enum values. The string forms match the persisted column
// values in Microsip DOCTOS_IN.
const (
	// TipoMovimientoSalida represents a stock-out movement (items leave an
	// almacen).
	TipoMovimientoSalida TipoMovimiento = "S"
	// TipoMovimientoEntrada represents a stock-in movement (items enter an
	// almacen).
	TipoMovimientoEntrada TipoMovimiento = "E"
)

// ParseTipoMovimiento parses a string into a TipoMovimiento or returns
// ErrTipoMovimientoInvalido.
func ParseTipoMovimiento(s string) (TipoMovimiento, error) {
	t := TipoMovimiento(s)
	if !t.IsValid() {
		return "", ErrTipoMovimientoInvalido
	}
	return t, nil
}

// IsValid reports whether t is a recognized TipoMovimiento.
func (t TipoMovimiento) IsValid() bool {
	switch t {
	case TipoMovimientoSalida, TipoMovimientoEntrada:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (t TipoMovimiento) String() string { return string(t) }

// IsSalida reports whether this is a stock-out movement.
func (t TipoMovimiento) IsSalida() bool { return t == TipoMovimientoSalida }

// IsEntrada reports whether this is a stock-in movement.
func (t TipoMovimiento) IsEntrada() bool { return t == TipoMovimientoEntrada }
