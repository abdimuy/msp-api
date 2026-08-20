package domain

// EstadoEnvio is a value object wrapping the delivery state machine. Only the
// six documented values are valid.
type EstadoEnvio string

// Canonical EstadoEnvio values. The literals match MSP_CM_ENVIO.ESTADO — do
// not rename without a migration.
const (
	// EstadoEnvioEnEspera identifies a delivery still inside the cancellation
	// window. It can still be stopped.
	EstadoEnvioEnEspera EstadoEnvio = "en_espera"
	// EstadoEnvioEnviando identifies a delivery already committed to the
	// channel. It can no longer be stopped.
	EstadoEnvioEnviando EstadoEnvio = "enviando"
	// EstadoEnvioEnviado identifies a delivery accepted by WhatsApp.
	EstadoEnvioEnviado EstadoEnvio = "enviado"
	// EstadoEnvioDetenido identifies a delivery stopped in time by a user.
	EstadoEnvioDetenido EstadoEnvio = "detenido"
	// EstadoEnvioFallido identifies a delivery rejected by the channel. It is
	// the only state with a manual exit (POST /reenviar, spec §8).
	EstadoEnvioFallido EstadoEnvio = "fallido"
	// EstadoEnvioSinTelefono identifies a delivery discarded because the
	// client has no usable phone number. It is terminal but not a failure.
	EstadoEnvioSinTelefono EstadoEnvio = "sin_telefono"
)

// validEstadoEnvioTransitions is the delivery state machine from spec §4.3,
// plus the manual fallido → en_espera exit required by POST /reenviar (spec
// §8): MSP_CM_ENVIO has UNIQUE (TIPO, REFERENCIA) (§5.1), so a resend reuses
// the row instead of inserting a new one.
var validEstadoEnvioTransitions = map[EstadoEnvio][]EstadoEnvio{
	EstadoEnvioEnEspera:    {EstadoEnvioEnviando, EstadoEnvioDetenido, EstadoEnvioSinTelefono},
	EstadoEnvioEnviando:    {EstadoEnvioEnviado, EstadoEnvioFallido},
	EstadoEnvioFallido:     {EstadoEnvioEnEspera},
	EstadoEnvioEnviado:     {},
	EstadoEnvioDetenido:    {},
	EstadoEnvioSinTelefono: {},
}

// ParseEstadoEnvio parses a string into a EstadoEnvio or returns
// ErrEstadoEnvioInvalido.
func ParseEstadoEnvio(s string) (EstadoEnvio, error) {
	e := EstadoEnvio(s)
	if !e.IsValid() {
		return "", ErrEstadoEnvioInvalido
	}
	return e, nil
}

// IsValid reports whether e is one of the six canonical states.
func (e EstadoEnvio) IsValid() bool {
	switch e {
	case EstadoEnvioEnEspera, EstadoEnvioEnviando, EstadoEnvioEnviado,
		EstadoEnvioDetenido, EstadoEnvioFallido, EstadoEnvioSinTelefono:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (e EstadoEnvio) String() string { return string(e) }

// CanTransitionTo reports whether the delivery may move from e to target
// according to the spec §4.3 state machine.
func (e EstadoEnvio) CanTransitionTo(target EstadoEnvio) bool {
	for _, a := range validEstadoEnvioTransitions[e] {
		if a == target {
			return true
		}
	}
	return false
}

// IsTerminal reports whether the delivery reached a final state. True for
// enviado, detenido and sin_telefono; false for en_espera, enviando and
// fallido (fallido can be resent via POST /reenviar, spec §8).
func (e EstadoEnvio) IsTerminal() bool {
	var terminal bool
	switch e {
	case EstadoEnvioEnviado, EstadoEnvioDetenido, EstadoEnvioSinTelefono:
		terminal = true
	case EstadoEnvioEnEspera, EstadoEnvioEnviando, EstadoEnvioFallido:
		terminal = false
	}
	return terminal
}

// EsDetenible reports whether the delivery can still be stopped. True only
// for en_espera, the question the UI asks before rendering the stop button.
func (e EstadoEnvio) EsDetenible() bool { return e == EstadoEnvioEnEspera }

// EsFalla reports whether the delivery failed. True only for fallido;
// sin_telefono is deliberately not a failure so retries never target clients
// without a usable phone.
func (e EstadoEnvio) EsFalla() bool { return e == EstadoEnvioFallido }
