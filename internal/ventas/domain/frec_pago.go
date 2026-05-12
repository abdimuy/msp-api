package domain

// FrecPago enumerates the payment frequency on a CREDITO venta.
type FrecPago string

// FrecPago enum values. The string forms match MSP_VENTAS.FREC_PAGO.
const (
	// FrecPagoSemanal denotes weekly payments.
	FrecPagoSemanal FrecPago = "SEMANAL"
	// FrecPagoQuincenal denotes biweekly payments.
	FrecPagoQuincenal FrecPago = "QUINCENAL"
	// FrecPagoMensual denotes monthly payments.
	FrecPagoMensual FrecPago = "MENSUAL"
)

// ParseFrecPago parses a string into a FrecPago or returns ErrFrecPagoInvalida.
func ParseFrecPago(s string) (FrecPago, error) {
	f := FrecPago(s)
	if !f.IsValid() {
		return "", ErrFrecPagoInvalida
	}
	return f, nil
}

// IsValid reports whether f is a recognized FrecPago.
func (f FrecPago) IsValid() bool {
	switch f {
	case FrecPagoSemanal, FrecPagoQuincenal, FrecPagoMensual:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (f FrecPago) String() string { return string(f) }
