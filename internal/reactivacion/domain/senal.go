//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// Senal is the value object identifying one signal the copiloto LLM detected
// in a cliente's inbound turn. It is a closed set of seven canonical values;
// any other string is invalid. A Decision may carry zero or more senales.
type Senal string

const (
	// SenalCompra marks buying INTEREST — the cliente is interested in or asks
	// about products, prices or plans. The copiloto RESPONDS and sells; this
	// signal does NOT escalate (it is informational, useful for funnel
	// telemetry). Contrast with SenalCierre.
	SenalCompra Senal = "senal_compra"
	// SenalCierre marks the cliente being READY TO BUY / close now (e.g. "lo
	// quiero", "¿cómo le hago?", "¿cómo pago?", "sí le entro"). Distinct from
	// SenalCompra (mere interest): a cierre ESCALATES to a human closer
	// (escalamiento invertido — the human closes at the good moment), interest
	// does not.
	SenalCierre Senal = "senal_cierre"
	// SenalDeuda marks the cliente raising an outstanding-balance concern.
	SenalDeuda Senal = "deuda"
	// SenalConfianzaBaja marks a low-confidence LLM read of the message.
	SenalConfianzaBaja Senal = "confianza_baja"
	// SenalPideHumano marks the cliente explicitly asking for a human.
	SenalPideHumano Senal = "pide_humano"
	// SenalEnojoLoop marks a repeated-frustration pattern across turns.
	SenalEnojoLoop Senal = "enojo_loop"
	// SenalFueraAllowlist marks content outside the approved response allowlist.
	SenalFueraAllowlist Senal = "fuera_allowlist"
)

// String returns the underlying string representation.
func (s Senal) String() string { return string(s) }

// Valido reports whether s is one of the canonical senal values.
func (s Senal) Valido() bool {
	switch s {
	case SenalCompra, SenalCierre, SenalDeuda, SenalConfianzaBaja, SenalPideHumano,
		SenalEnojoLoop, SenalFueraAllowlist:
		return true
	default:
		return false
	}
}

// ParseSenal converts a raw string to a Senal, returning ErrSenalInvalido
// when the value is not one of the canonical constants.
func ParseSenal(raw string) (Senal, error) {
	s := Senal(raw)
	if !s.Valido() {
		return "", ErrSenalInvalido
	}
	return s, nil
}
