package domain

// RolDecisor values. Who decides whether a repair is "fast enough" to skip
// a cambio físico — or who authorizes one — varies by situation: it can be
// carpintería, oficina, or the technical area. The permission
// (garantias:actualizar, garantias:autorizar) controls whether the actor
// may act; RolDecisor records which role they acted from, so an audit six
// months later can tell who actually made the call.
const (
	RolDecisorCarpinteria = "carpinteria"
	RolDecisorOficina     = "oficina"
	RolDecisorTecnica     = "tecnica"
)

// RolDecisor is a value object wrapping the role from which a decision
// event was recorded. Only the three roles above are valid. Nullable at the
// entity level: only decision events carry a RolDecisor.
type RolDecisor struct{ value string }

// NewRolDecisor validates and constructs a RolDecisor. Rejects anything
// outside the three recognized roles with ErrRolDecisorInvalido.
func NewRolDecisor(s string) (RolDecisor, error) {
	switch s {
	case RolDecisorCarpinteria, RolDecisorOficina, RolDecisorTecnica:
		return RolDecisor{value: s}, nil
	default:
		return RolDecisor{}, ErrRolDecisorInvalido
	}
}

// HydrateRolDecisor rebuilds a RolDecisor from persistence without
// validation. Intended for repository use only.
func HydrateRolDecisor(s string) RolDecisor { return RolDecisor{value: s} }

// Value returns the raw decider role string.
func (r RolDecisor) Value() string { return r.value }

// String returns the decider role string representation.
func (r RolDecisor) String() string { return r.value }

// Equals reports whether two RolDecisor values are identical.
func (r RolDecisor) Equals(other RolDecisor) bool { return r.value == other.value }

// IsZero reports whether the RolDecisor has its zero value (empty string).
func (r RolDecisor) IsZero() bool { return r.value == "" }
