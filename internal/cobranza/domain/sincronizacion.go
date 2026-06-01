package domain

// Sincronizacion is the Microsip-integration dimension of a pago: whether the
// pago has been materialized into Microsip's DOCTOS_CC ledger. Mirrors
// MSP_PAGOS_RECIBIDOS.SINCRONIZACION.
//
// The only transition is pendiente → aplicada, performed by MarcarAplicada
// once materialization succeeds. On failure the pago stays 'pendiente' and is
// retryable.
type Sincronizacion string

// Canonical Sincronizacion values. The literals match MSP_PAGOS_RECIBIDOS.SINCRONIZACION
// — do not rename without a migration.
const (
	// SincronizacionPendiente means the pago has not been materialized yet.
	SincronizacionPendiente Sincronizacion = "pendiente"
	// SincronizacionAplicada means the pago was materialized in Microsip; the
	// MICROSIP_* artifact columns are populated.
	SincronizacionAplicada Sincronizacion = "aplicada"
)

// IsValid reports whether s is one of the canonical values.
func (s Sincronizacion) IsValid() bool {
	switch s {
	case SincronizacionPendiente, SincronizacionAplicada:
		return true
	}
	return false
}

// String returns the underlying string.
func (s Sincronizacion) String() string { return string(s) }

// ParseSincronizacion validates the input against the canonical set. Returns
// ErrSincronizacionInvalida on miss.
func ParseSincronizacion(in string) (Sincronizacion, error) {
	s := Sincronizacion(in)
	if !s.IsValid() {
		return "", ErrSincronizacionInvalida
	}
	return s, nil
}
