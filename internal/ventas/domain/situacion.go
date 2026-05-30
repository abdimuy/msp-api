package domain

// Situacion is the business-lifecycle dimension of a venta. Mirrors
// MSP_VENTAS.SITUACION. It is independent of EstadoRegistro (technical
// existence) and Sincronizacion (Microsip integration).
//
// Valid transitions:
//
//	borrador  → revisada            (EnviarARevision)
//	revisada  → aprobada            (Aprobar)
//	revisada  → borrador            (RegresarABorrador)
//	borrador|revisada|aprobada → cancelada (Cancelar)
//
// 'aplicada' is NOT a Situacion value — materialization in Microsip lives in
// the Sincronizacion dimension.
type Situacion string

// Canonical Situacion values. The literals match MSP_VENTAS.SITUACION — do not
// rename without a migration.
const (
	// SituacionBorrador is the initial state; the venta is freely editable.
	SituacionBorrador Situacion = "borrador"
	// SituacionRevisada means the venta was sent for review.
	SituacionRevisada Situacion = "revisada"
	// SituacionAprobada means the venta was approved and may be materialized.
	SituacionAprobada Situacion = "aprobada"
	// SituacionCancelada means the venta was canceled.
	SituacionCancelada Situacion = "cancelada"
)

// IsValid reports whether s is one of the canonical values.
func (s Situacion) IsValid() bool {
	switch s {
	case SituacionBorrador, SituacionRevisada, SituacionAprobada, SituacionCancelada:
		return true
	}
	return false
}

// String returns the underlying string.
func (s Situacion) String() string { return string(s) }

// ParseSituacion validates the input against the canonical set. Returns
// ErrSituacionInvalida on miss.
func ParseSituacion(in string) (Situacion, error) {
	s := Situacion(in)
	if !s.IsValid() {
		return "", ErrSituacionInvalida
	}
	return s, nil
}
