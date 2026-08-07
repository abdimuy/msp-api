package domain

// RolArticulo represents the role of an artículo row within a warranty folio.
type RolArticulo string

// RolArticulo values represent the role of an artículo row.
const (
	RolArticuloOriginal  RolArticulo = "original"
	RolArticuloReemplazo RolArticulo = "reemplazo"
)

// ParseRolArticulo validates and returns a RolArticulo.
// Returns ErrRolArticuloInvalido if s is not "original" or "reemplazo".
func ParseRolArticulo(s string) (RolArticulo, error) {
	r := RolArticulo(s)
	if !r.IsValid() {
		return "", ErrRolArticuloInvalido
	}
	return r, nil
}

// IsValid reports whether r is a known RolArticulo value.
func (r RolArticulo) IsValid() bool {
	switch r {
	case RolArticuloOriginal, RolArticuloReemplazo:
		return true
	}
	return false
}

// String returns the string representation of r.
func (r RolArticulo) String() string { return string(r) }

// EsOriginal reports whether this row is the originally received item.
func (r RolArticulo) EsOriginal() bool { return r == RolArticuloOriginal }

// EsReemplazo reports whether this row is a replacement created by a cambio físico.
func (r RolArticulo) EsReemplazo() bool { return r == RolArticuloReemplazo }
