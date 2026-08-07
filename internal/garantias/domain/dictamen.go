package domain

// Dictamen represents the vendor's verdict on an artículo sent down the
// ruta proveedor.
type Dictamen string

// Dictamen values represent the vendor's verdict.
const (
	DictamenAceptada  Dictamen = "aceptada"
	DictamenRechazada Dictamen = "rechazada"
	DictamenSinFalla  Dictamen = "sin_falla"
)

// ParseDictamen validates and returns a Dictamen.
// Returns ErrDictamenInvalido if s is not "aceptada", "rechazada", or "sin_falla".
func ParseDictamen(s string) (Dictamen, error) {
	d := Dictamen(s)
	if !d.IsValid() {
		return "", ErrDictamenInvalido
	}
	return d, nil
}

// IsValid reports whether d is a known Dictamen value.
func (d Dictamen) IsValid() bool {
	switch d {
	case DictamenAceptada, DictamenRechazada, DictamenSinFalla:
		return true
	}
	return false
}

// String returns the string representation of d.
func (d Dictamen) String() string { return string(d) }

// EsAceptada reports whether the vendor confirmed and accepted the defect.
func (d Dictamen) EsAceptada() bool { return d == DictamenAceptada }

// EsRechazada reports whether the vendor rejected coverage of the defect.
func (d Dictamen) EsRechazada() bool { return d == DictamenRechazada }

// EsSinFalla reports whether the vendor found no defect.
func (d Dictamen) EsSinFalla() bool { return d == DictamenSinFalla }
