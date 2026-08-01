//nolint:dupl // all value objects follow the same pattern; duplication is intentional and unavoidable.
package domain

// RolArticuloOriginal represents the item as originally received into
// custody under a warranty folio.
const RolArticuloOriginal = "original"

// RolArticuloReemplazo represents an item created when a cambio físico
// swaps the original for a replacement. The replacement carries this role;
// the original continues its own path (standby, segunda_mano, desarmado,
// or merma) under the same folio.
const RolArticuloReemplazo = "reemplazo"

// RolArticulo is a value object wrapping the role of an artículo row within
// a warranty folio. Only "original" and "reemplazo" are valid.
type RolArticulo struct{ value string }

// NewRolArticulo validates and constructs a RolArticulo. Accepts only
// "original" or "reemplazo"; rejects anything else with
// ErrRolArticuloInvalido.
func NewRolArticulo(s string) (RolArticulo, error) {
	if s != RolArticuloOriginal && s != RolArticuloReemplazo {
		return RolArticulo{}, ErrRolArticuloInvalido
	}
	return RolArticulo{value: s}, nil
}

// HydrateRolArticulo rebuilds a RolArticulo from persistence without
// validation. Intended for repository use only.
func HydrateRolArticulo(s string) RolArticulo { return RolArticulo{value: s} }

// Value returns the raw role string ("original" or "reemplazo").
func (r RolArticulo) Value() string { return r.value }

// String returns the role string representation.
func (r RolArticulo) String() string { return r.value }

// Equals reports whether two RolArticulo values are identical.
func (r RolArticulo) Equals(other RolArticulo) bool { return r.value == other.value }

// IsZero reports whether the RolArticulo has its zero value (empty string).
func (r RolArticulo) IsZero() bool { return r.value == "" }

// EsOriginal reports whether this row is the originally received item.
func (r RolArticulo) EsOriginal() bool { return r.value == RolArticuloOriginal }

// EsReemplazo reports whether this row is a replacement created by a
// cambio físico.
func (r RolArticulo) EsReemplazo() bool { return r.value == RolArticuloReemplazo }
