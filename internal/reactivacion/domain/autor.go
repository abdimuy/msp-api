//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// Autor is the value object identifying who produced one Turno in a
// conversation. It is a closed set of three canonical values; any other
// string is invalid.
type Autor string

const (
	// AutorCliente marks a turno written by the cliente.
	AutorCliente Autor = "cliente"
	// AutorIA marks a turno drafted or sent by the copiloto LLM.
	AutorIA Autor = "ia"
	// AutorHumano marks a turno written by a human operator.
	AutorHumano Autor = "humano"
)

// String returns the underlying string representation.
func (a Autor) String() string { return string(a) }

// Valido reports whether a is one of the canonical autor values.
func (a Autor) Valido() bool {
	switch a {
	case AutorCliente, AutorIA, AutorHumano:
		return true
	default:
		return false
	}
}

// ParseAutor converts a raw string to an Autor, returning ErrAutorInvalido
// when the value is not one of the canonical constants.
func ParseAutor(raw string) (Autor, error) {
	a := Autor(raw)
	if !a.Valido() {
		return "", ErrAutorInvalido
	}
	return a, nil
}
