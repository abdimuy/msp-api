package domain

// Desenlace represents the final outcome for a warranty item.
type Desenlace string

// All 6 outcomes from the design.
const (
	DesenlaceReparado    Desenlace = "reparado"
	DesenlaceReemplazado Desenlace = "reemplazado"
	DesenlaceDevuelto    Desenlace = "devuelto"
	DesenlaceSegundaMano Desenlace = "segunda_mano"
	DesenlaceDesarmado   Desenlace = "desarmado"
	DesenlaceMerma       Desenlace = "merma"
)

// ParseDesenlace validates and returns a Desenlace.
// Returns ErrDesenlaceInvalido if s is not one of the 6 recognized outcomes.
func ParseDesenlace(s string) (Desenlace, error) {
	d := Desenlace(s)
	if !d.IsValid() {
		return "", ErrDesenlaceInvalido
	}
	return d, nil
}

// IsValid reports whether d is a known Desenlace value.
func (d Desenlace) IsValid() bool {
	switch d {
	case DesenlaceReparado, DesenlaceReemplazado, DesenlaceDevuelto,
		DesenlaceSegundaMano, DesenlaceDesarmado, DesenlaceMerma:
		return true
	}
	return false
}

// String returns the string representation of d.
func (d Desenlace) String() string { return string(d) }
