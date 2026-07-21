//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// DireccionTurno is the value object identifying whether a Turno was
// received from the cliente or sent to the cliente. It is a closed set of
// two canonical values; any other string is invalid.
type DireccionTurno string

const (
	// DireccionEntrante marks a turno received from the cliente.
	DireccionEntrante DireccionTurno = "entrante"
	// DireccionSaliente marks a turno sent to the cliente.
	DireccionSaliente DireccionTurno = "saliente"
)

// String returns the underlying string representation.
func (d DireccionTurno) String() string { return string(d) }

// Valido reports whether d is one of the canonical direccion_turno values.
func (d DireccionTurno) Valido() bool {
	switch d {
	case DireccionEntrante, DireccionSaliente:
		return true
	default:
		return false
	}
}

// ParseDireccionTurno converts a raw string to a DireccionTurno, returning
// ErrDireccionTurnoInvalido when the value is not one of the canonical
// constants.
func ParseDireccionTurno(raw string) (DireccionTurno, error) {
	d := DireccionTurno(raw)
	if !d.Valido() {
		return "", ErrDireccionTurnoInvalido
	}
	return d, nil
}
