//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// EstadoConversacion is the value object identifying where a copiloto
// conversation sits in the escalation state machine. It is a closed set of
// seven canonical values; any other string is invalid.
//
//	contactado  → the channel just reached the cliente (Fase 2 baseline).
//	respondio   → the cliente answered the outbound message for the first time.
//	conversando → an active back-and-forth is underway.
//	escalado    → a human operator (asignadoA) took over.
//	interesado  → the cliente showed buying intent.
//	enganche    → terminal: the cliente converted to a new sale.
//	descartado  → terminal: the conversation was closed without conversion.
type EstadoConversacion string

const (
	// EstadoContactado marks a conversation just reached by the channel.
	EstadoContactado EstadoConversacion = "contactado"
	// EstadoRespondio marks a conversation where the cliente answered.
	EstadoRespondio EstadoConversacion = "respondio"
	// EstadoConversando marks an active back-and-forth.
	EstadoConversando EstadoConversacion = "conversando"
	// EstadoEscalado marks a conversation handed off to a human operator.
	EstadoEscalado EstadoConversacion = "escalado"
	// EstadoInteresado marks a conversation showing buying intent.
	EstadoInteresado EstadoConversacion = "interesado"
	// EstadoEnganche marks a conversation that converted to a new sale. Terminal.
	EstadoEnganche EstadoConversacion = "enganche"
	// EstadoDescartado marks a conversation closed without conversion. Terminal.
	EstadoDescartado EstadoConversacion = "descartado"
)

// String returns the underlying string representation.
func (e EstadoConversacion) String() string { return string(e) }

// Valido reports whether e is one of the canonical estado_conversacion values.
func (e EstadoConversacion) Valido() bool {
	switch e {
	case EstadoContactado, EstadoRespondio, EstadoConversando, EstadoEscalado,
		EstadoInteresado, EstadoEnganche, EstadoDescartado:
		return true
	default:
		return false
	}
}

// EsTerminal reports whether e is a terminal state (enganche or descartado) —
// no further transition is allowed once a Conversacion reaches one of these.
func (e EstadoConversacion) EsTerminal() bool {
	return e == EstadoEnganche || e == EstadoDescartado
}

// ParseEstadoConversacion converts a raw string to an EstadoConversacion,
// returning ErrEstadoConversacionInvalido when the value is not one of the
// canonical constants.
func ParseEstadoConversacion(raw string) (EstadoConversacion, error) {
	e := EstadoConversacion(raw)
	if !e.Valido() {
		return "", ErrEstadoConversacionInvalido
	}
	return e, nil
}
