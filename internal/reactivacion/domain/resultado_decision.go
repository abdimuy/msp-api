//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// ResultadoDecision is the value object identifying the outcome of one
// copiloto Decision after an operator (or the auto-send policy) acts on it.
// It is a closed set of four canonical values; any other string is invalid.
type ResultadoDecision string

const (
	// ResultadoPropuesto marks a decision the LLM proposed but no one has
	// acted on yet.
	ResultadoPropuesto ResultadoDecision = "propuesto"
	// ResultadoAprobado marks a decision an operator approved as-is.
	ResultadoAprobado ResultadoDecision = "aprobado"
	// ResultadoEditado marks a decision an operator approved after editing
	// the draft.
	ResultadoEditado ResultadoDecision = "editado"
	// ResultadoEscalado marks a decision an operator escalated instead of
	// sending.
	ResultadoEscalado ResultadoDecision = "escalado"
)

// String returns the underlying string representation.
func (r ResultadoDecision) String() string { return string(r) }

// Valido reports whether r is one of the canonical resultado_decision values.
func (r ResultadoDecision) Valido() bool {
	switch r {
	case ResultadoPropuesto, ResultadoAprobado, ResultadoEditado, ResultadoEscalado:
		return true
	default:
		return false
	}
}

// ParseResultadoDecision converts a raw string to a ResultadoDecision,
// returning ErrResultadoDecisionInvalido when the value is not one of the
// canonical constants.
func ParseResultadoDecision(raw string) (ResultadoDecision, error) {
	r := ResultadoDecision(raw)
	if !r.Valido() {
		return "", ErrResultadoDecisionInvalido
	}
	return r, nil
}
