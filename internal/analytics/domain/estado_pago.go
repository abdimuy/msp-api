//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// EstadoPago classifies a client's payment behaviour based on their outstanding
// balance (saldo) and last-payment date. Values are UPPERCASE to match the
// Microsip-style convention used throughout the analytics module.
//
// Computed at read time in the app layer via estadoPagoFor; never stored directly.
type EstadoPago string

const (
	// EstadoPagoSinCredito denotes a contado-only client: saldo == 0 and no
	// payment history (fechaUltimoPago is zero).
	EstadoPagoSinCredito EstadoPago = "SIN_CREDITO"

	// EstadoPagoLiquidado denotes a client who had credit and is fully paid
	// (saldo == 0 but has at least one historical payment).
	EstadoPagoLiquidado EstadoPago = "LIQUIDADO"

	// EstadoPagoAlCorriente denotes a client with outstanding balance who paid
	// recently (within umbralAlCorrienteDias days).
	EstadoPagoAlCorriente EstadoPago = "AL_CORRIENTE"

	// EstadoPagoAtrasado denotes a client with outstanding balance whose last
	// payment was moderately overdue (between umbralAlCorrienteDias and
	// umbralAtrasadoDias days ago).
	EstadoPagoAtrasado EstadoPago = "ATRASADO"

	// EstadoPagoMoroso denotes a client with outstanding balance who has not
	// paid in a long time (more than umbralAtrasadoDias days, or never).
	EstadoPagoMoroso EstadoPago = "MOROSO"
)

// ParseEstadoPago parses s into an EstadoPago or returns ErrEstadoPagoInvalido.
// Input must match the exact UPPERCASE canonical form.
func ParseEstadoPago(s string) (EstadoPago, error) {
	ep := EstadoPago(s)
	if !ep.IsValid() {
		return "", ErrEstadoPagoInvalido
	}
	return ep, nil
}

// IsValid reports whether ep is a recognized EstadoPago value.
func (ep EstadoPago) IsValid() bool {
	switch ep {
	case EstadoPagoSinCredito,
		EstadoPagoLiquidado,
		EstadoPagoAlCorriente,
		EstadoPagoAtrasado,
		EstadoPagoMoroso:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (ep EstadoPago) String() string { return string(ep) }
