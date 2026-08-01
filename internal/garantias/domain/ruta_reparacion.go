//nolint:dupl // all value objects follow the same pattern; duplication is intentional and unavoidable.
package domain

// RutaReparacionProveedor represents the external-vendor repair route.
const RutaReparacionProveedor = "proveedor"

// RutaReparacionTaller represents the in-house workshop repair route.
const RutaReparacionTaller = "taller"

// RutaReparacion is a value object wrapping the repair route an artículo
// follows after diagnosis. Only "proveedor" and "taller" are valid. Nil
// until the diagnosis step fixes the route.
type RutaReparacion struct{ value string }

// NewRutaReparacion validates and constructs a RutaReparacion. Accepts only
// "proveedor" or "taller"; rejects anything else with
// ErrRutaReparacionInvalida.
func NewRutaReparacion(s string) (RutaReparacion, error) {
	if s != RutaReparacionProveedor && s != RutaReparacionTaller {
		return RutaReparacion{}, ErrRutaReparacionInvalida
	}
	return RutaReparacion{value: s}, nil
}

// HydrateRutaReparacion rebuilds a RutaReparacion from persistence without
// validation. Intended for repository use only.
func HydrateRutaReparacion(s string) RutaReparacion { return RutaReparacion{value: s} }

// Value returns the raw repair route string ("proveedor" or "taller").
func (r RutaReparacion) Value() string { return r.value }

// String returns the repair route string representation.
func (r RutaReparacion) String() string { return r.value }

// Equals reports whether two RutaReparacion values are identical.
func (r RutaReparacion) Equals(other RutaReparacion) bool { return r.value == other.value }

// IsZero reports whether the RutaReparacion has its zero value (empty
// string) — i.e. the diagnosis step has not fixed a route yet.
func (r RutaReparacion) IsZero() bool { return r.value == "" }

// EsProveedor reports whether this is the external-vendor repair route.
func (r RutaReparacion) EsProveedor() bool { return r.value == RutaReparacionProveedor }

// EsTaller reports whether this is the in-house workshop repair route.
func (r RutaReparacion) EsTaller() bool { return r.value == RutaReparacionTaller }
