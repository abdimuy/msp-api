package domain

// EstadoReenvio is the delivery state machine of a MensajeEntrante: whether
// it still needs to be forwarded to the store's on-premise server, already
// was, or the last attempt failed.
type EstadoReenvio string

// EstadoReenvio values.
const (
	// EstadoReenvioPendiente is the initial state: not yet forwarded.
	EstadoReenvioPendiente EstadoReenvio = "pendiente"
	// EstadoReenvioReenviado identifies a message the store has acknowledged.
	// Terminal — a message is never forwarded twice.
	EstadoReenvioReenviado EstadoReenvio = "reenviado"
	// EstadoReenvioFallido identifies a message whose last forward attempt
	// failed. Not terminal: a retry moves it back to pendiente.
	EstadoReenvioFallido EstadoReenvio = "fallido"
)

// validEstadoReenvioTransitions is the mailbox's forwarding state machine:
// pendiente → reenviado | fallido, and fallido → pendiente for a retry.
// reenviado has no outgoing transitions.
var validEstadoReenvioTransitions = map[EstadoReenvio][]EstadoReenvio{
	EstadoReenvioPendiente: {EstadoReenvioReenviado, EstadoReenvioFallido},
	EstadoReenvioFallido:   {EstadoReenvioPendiente},
	EstadoReenvioReenviado: {},
}

// ParseEstadoReenvio validates and returns an EstadoReenvio.
// Returns ErrEstadoReenvioInvalido if s is not one of the three recognized
// states.
func ParseEstadoReenvio(s string) (EstadoReenvio, error) {
	e := EstadoReenvio(s)
	if !e.IsValid() {
		return "", ErrEstadoReenvioInvalido
	}
	return e, nil
}

// IsValid reports whether e is a known EstadoReenvio value.
func (e EstadoReenvio) IsValid() bool {
	switch e {
	case EstadoReenvioPendiente, EstadoReenvioReenviado, EstadoReenvioFallido:
		return true
	}
	return false
}

// String returns the string representation of e.
func (e EstadoReenvio) String() string { return string(e) }

// CanTransitionTo reports whether e can transition to target according to
// validEstadoReenvioTransitions.
func (e EstadoReenvio) CanTransitionTo(target EstadoReenvio) bool {
	for _, allowed := range validEstadoReenvioTransitions[e] {
		if allowed == target {
			return true
		}
	}
	return false
}

// IsTerminal reports whether e is a terminal state. True only for
// reenviado — fallido always has a way out via retry.
func (e EstadoReenvio) IsTerminal() bool {
	return e == EstadoReenvioReenviado
}
