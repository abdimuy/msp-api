//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// EstadoMensaje is the value object identifying where a MSP_RX_MENSAJES row
// sits in the send state machine. It is a closed set of four canonical
// values; any other string is invalid.
//
//	encolado  → queued, waiting for the governor / auto_send to allow the send.
//	enviado   → the sender accepted the message (success).
//	fallido   → the sender rejected the message (error, eligible for retry logic
//	            in a later phase).
//	bloqueado → the governor's circuit-breaker paused the channel (reserved for
//	            Fase 3; Fase 2's fake sender never produces this state).
type EstadoMensaje string

const (
	// EstadoEncolado marks a message waiting to be sent.
	EstadoEncolado EstadoMensaje = "encolado"
	// EstadoEnviado marks a message the sender accepted.
	EstadoEnviado EstadoMensaje = "enviado"
	// EstadoFallido marks a message the sender rejected.
	EstadoFallido EstadoMensaje = "fallido"
	// EstadoBloqueado marks a message the circuit-breaker paused.
	EstadoBloqueado EstadoMensaje = "bloqueado"
)

// String returns the underlying string representation.
func (e EstadoMensaje) String() string { return string(e) }

// Valido reports whether e is one of the canonical estado values.
func (e EstadoMensaje) Valido() bool {
	switch e {
	case EstadoEncolado, EstadoEnviado, EstadoFallido, EstadoBloqueado:
		return true
	default:
		return false
	}
}

// ParseEstadoMensaje converts a raw string to an EstadoMensaje, returning
// ErrEstadoMensajeInvalido when the value is not one of the canonical constants.
func ParseEstadoMensaje(raw string) (EstadoMensaje, error) {
	e := EstadoMensaje(raw)
	if !e.Valido() {
		return "", ErrEstadoMensajeInvalido
	}
	return e, nil
}

// SenderKind identifies which channel implementation delivered a message —
// the measurement-integrity tag that keeps a simulated send from ever
// counting as a real contact in the attribution (Atribucion).
type SenderKind string

const (
	// SenderSimulado marks a message delivered by the FakeSender.
	SenderSimulado SenderKind = "simulado"
	// SenderReal marks a message delivered by a real channel (whatsmeow, Fase 3).
	SenderReal SenderKind = "real"
)

// String returns the underlying string representation.
func (s SenderKind) String() string { return string(s) }

// Valido reports whether s is one of the canonical sender-kind values.
func (s SenderKind) Valido() bool {
	switch s {
	case SenderSimulado, SenderReal:
		return true
	default:
		return false
	}
}

// ParseSenderKind converts a raw string to a SenderKind, returning
// ErrSenderKindInvalido when the value is not one of the canonical constants.
func ParseSenderKind(raw string) (SenderKind, error) {
	s := SenderKind(raw)
	if !s.Valido() {
		return "", ErrSenderKindInvalido
	}
	return s, nil
}
