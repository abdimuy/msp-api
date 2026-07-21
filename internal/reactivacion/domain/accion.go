//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// Accion is the value object identifying what the copiloto LLM proposes to
// do with a Decision — reply directly or hand off to a human. It is a closed
// set of two canonical values; any other string is invalid.
type Accion string

const (
	// AccionResponder proposes replying to the cliente directly.
	AccionResponder Accion = "responder"
	// AccionEscalar proposes handing the conversation off to a human operator.
	AccionEscalar Accion = "escalar"
)

// String returns the underlying string representation.
func (a Accion) String() string { return string(a) }

// Valido reports whether a is one of the canonical accion values.
func (a Accion) Valido() bool {
	switch a {
	case AccionResponder, AccionEscalar:
		return true
	default:
		return false
	}
}

// ParseAccion converts a raw string to an Accion, returning ErrAccionInvalido
// when the value is not one of the canonical constants.
func ParseAccion(raw string) (Accion, error) {
	a := Accion(raw)
	if !a.Valido() {
		return "", ErrAccionInvalido
	}
	return a, nil
}
