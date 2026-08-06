package domain

// RolDecisor represents the role from which a decision event was recorded.
type RolDecisor string

// RolDecisor values represent the role from which a decision was recorded.
const (
	RolDecisorCarpinteria RolDecisor = "carpinteria"
	RolDecisorOficina     RolDecisor = "oficina"
	RolDecisorTecnica     RolDecisor = "tecnica"
)

// ParseRolDecisor validates and returns a RolDecisor.
// Returns ErrRolDecisorInvalido if s is not one of the three recognized roles.
func ParseRolDecisor(s string) (RolDecisor, error) {
	r := RolDecisor(s)
	if !r.IsValid() {
		return "", ErrRolDecisorInvalido
	}
	return r, nil
}

// IsValid reports whether r is a known RolDecisor value.
func (r RolDecisor) IsValid() bool {
	switch r {
	case RolDecisorCarpinteria, RolDecisorOficina, RolDecisorTecnica:
		return true
	}
	return false
}

// String returns the string representation of r.
func (r RolDecisor) String() string { return string(r) }
