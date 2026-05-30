package domain

// EstadoRegistro is the technical existence dimension of a venta: whether the
// row is logically present or soft-deleted. It is independent of the business
// SITUACION and the SINCRONIZACION dimensions. Mirrors MSP_VENTAS.STATUS.
//
// 'deleted' is reserved for records created by mistake; the normal business
// lifecycle (borrador → revisada → aprobada → cancelada) lives in Situacion.
type EstadoRegistro string

// Canonical EstadoRegistro values. The literals match MSP_VENTAS.STATUS — do
// not rename without a migration.
const (
	// EstadoActive marks a logically present venta.
	EstadoActive EstadoRegistro = "active"
	// EstadoDeleted marks a soft-deleted venta (created by mistake).
	EstadoDeleted EstadoRegistro = "deleted"
)

// IsValid reports whether e is one of the canonical values.
func (e EstadoRegistro) IsValid() bool {
	switch e {
	case EstadoActive, EstadoDeleted:
		return true
	}
	return false
}

// String returns the underlying string.
func (e EstadoRegistro) String() string { return string(e) }

// ParseEstadoRegistro validates the input against the canonical set. Returns
// ErrEstadoRegistroInvalido on miss.
func ParseEstadoRegistro(in string) (EstadoRegistro, error) {
	e := EstadoRegistro(in)
	if !e.IsValid() {
		return "", ErrEstadoRegistroInvalido
	}
	return e, nil
}
