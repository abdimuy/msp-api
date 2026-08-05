package domain

// EstadoEnvioEnEspera identifies a delivery still inside the cancellation
// window. It can still be stopped.
const EstadoEnvioEnEspera = "en_espera"

// EstadoEnvioEnviando identifies a delivery already committed to the channel.
// It can no longer be stopped.
const EstadoEnvioEnviando = "enviando"

// EstadoEnvioEnviado identifies a delivery accepted by WhatsApp.
const EstadoEnvioEnviado = "enviado"

// EstadoEnvioDetenido identifies a delivery stopped in time by a user.
const EstadoEnvioDetenido = "detenido"

// EstadoEnvioFallido identifies a delivery rejected by the channel.
const EstadoEnvioFallido = "fallido"

// EstadoEnvioSinTelefono identifies a delivery discarded because the client
// has no usable phone number. It is terminal but not a failure.
const EstadoEnvioSinTelefono = "sin_telefono"

// EstadoEnvio is a value object wrapping the delivery state machine. Only the
// six documented values are valid.
type EstadoEnvio struct{ value string }

// NewEstadoEnvio validates and constructs a EstadoEnvio. Rejects anything
// else with ErrEstadoEnvioInvalido.
func NewEstadoEnvio(s string) (EstadoEnvio, error) {
	switch s {
	case EstadoEnvioEnEspera, EstadoEnvioEnviando, EstadoEnvioEnviado,
		EstadoEnvioDetenido, EstadoEnvioFallido, EstadoEnvioSinTelefono:
		return EstadoEnvio{value: s}, nil
	default:
		return EstadoEnvio{}, ErrEstadoEnvioInvalido
	}
}

// HydrateEstadoEnvio rebuilds one from persistence without validation.
// Intended for repository use only.
func HydrateEstadoEnvio(s string) EstadoEnvio { return EstadoEnvio{value: s} }

// Value returns the raw delivery state string.
func (e EstadoEnvio) Value() string { return e.value }

// String returns the delivery state string representation.
func (e EstadoEnvio) String() string { return e.value }

// Equals reports whether two EstadoEnvio values are identical.
func (e EstadoEnvio) Equals(other EstadoEnvio) bool { return e.value == other.value }

// IsZero reports whether the EstadoEnvio has its zero value (empty string).
func (e EstadoEnvio) IsZero() bool { return e.value == "" }

// EsDetenible reports whether the delivery can still be stopped. True only
// for en_espera, the question the UI asks before rendering the stop button.
func (e EstadoEnvio) EsDetenible() bool { return e.value == EstadoEnvioEnEspera }

// EsTerminal reports whether the delivery reached a final state. True for
// enviado, detenido, fallido and sin_telefono; false for en_espera and
// enviando.
func (e EstadoEnvio) EsTerminal() bool {
	switch e.value {
	case EstadoEnvioEnviado, EstadoEnvioDetenido, EstadoEnvioFallido,
		EstadoEnvioSinTelefono:
		return true
	default:
		return false
	}
}

// EsFalla reports whether the delivery failed. True only for fallido;
// sin_telefono is deliberately not a failure so retries never target clients
// without a usable phone.
func (e EstadoEnvio) EsFalla() bool { return e.value == EstadoEnvioFallido }
