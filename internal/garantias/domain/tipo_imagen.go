package domain

// TipoImagen says what an image proves. Evidence is any photo of the item
// (spec §3.4); a signature is the client's conformity at delivery (spec §4.1)
// and only Entregar creates one. The value is stored in MSP_GA_IMAGEN.TIPO,
// written by Go on every insert; rows written before the column existed read
// back as evidencia (migración 000071).
type TipoImagen string

// The two image types.
const (
	TipoImagenEvidencia TipoImagen = "evidencia"
	TipoImagenFirma     TipoImagen = "firma"
)

// ParseTipoImagen validates and returns a TipoImagen.
// Returns ErrTipoImagenInvalido if s is not "evidencia" or "firma".
func ParseTipoImagen(s string) (TipoImagen, error) {
	t := TipoImagen(s)
	if !t.IsValid() {
		return "", ErrTipoImagenInvalido
	}
	return t, nil
}

// IsValid reports whether t is a known TipoImagen value.
func (t TipoImagen) IsValid() bool {
	switch t {
	case TipoImagenEvidencia, TipoImagenFirma:
		return true
	}
	return false
}

// String returns the string representation of t.
func (t TipoImagen) String() string { return string(t) }
