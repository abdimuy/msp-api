package domain

// Dictamen values returned by the external vendor on the ruta proveedor
// path: aceptada (the reported defect is confirmed and covered), rechazada
// (not covered — the client keeps the item as-is), sin_falla (no defect
// found).
const (
	DictamenAceptada  = "aceptada"
	DictamenRechazada = "rechazada"
	DictamenSinFalla  = "sin_falla"
)

// Dictamen is a value object wrapping the vendor's verdict on an artículo
// sent down the ruta proveedor. Only applies to that route.
type Dictamen struct{ value string }

// NewDictamen validates and constructs a Dictamen. Rejects anything outside
// the three recognized verdicts with ErrDictamenInvalido.
func NewDictamen(s string) (Dictamen, error) {
	switch s {
	case DictamenAceptada, DictamenRechazada, DictamenSinFalla:
		return Dictamen{value: s}, nil
	default:
		return Dictamen{}, ErrDictamenInvalido
	}
}

// HydrateDictamen rebuilds a Dictamen from persistence without validation.
// Intended for repository use only.
func HydrateDictamen(s string) Dictamen { return Dictamen{value: s} }

// Value returns the raw verdict string.
func (d Dictamen) Value() string { return d.value }

// String returns the verdict string representation.
func (d Dictamen) String() string { return d.value }

// Equals reports whether two Dictamen values are identical.
func (d Dictamen) Equals(other Dictamen) bool { return d.value == other.value }

// IsZero reports whether the Dictamen has its zero value (empty string) —
// i.e. the vendor has not responded yet.
func (d Dictamen) IsZero() bool { return d.value == "" }

// EsAceptada reports whether the vendor confirmed and accepted the defect.
func (d Dictamen) EsAceptada() bool { return d.value == DictamenAceptada }

// EsRechazada reports whether the vendor rejected coverage of the defect.
func (d Dictamen) EsRechazada() bool { return d.value == DictamenRechazada }

// EsSinFalla reports whether the vendor found no defect.
func (d Dictamen) EsSinFalla() bool { return d.value == DictamenSinFalla }
