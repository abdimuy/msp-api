package domain

// RutaReparacion represents the repair route an artículo follows after diagnosis.
type RutaReparacion string

// RutaReparacion values represent the repair route an artículo follows.
const (
	RutaReparacionProveedor RutaReparacion = "proveedor"
	RutaReparacionTaller    RutaReparacion = "taller"
)

// ParseRutaReparacion validates and returns a RutaReparacion.
// Returns ErrRutaReparacionInvalida if s is not "proveedor" or "taller".
func ParseRutaReparacion(s string) (RutaReparacion, error) {
	r := RutaReparacion(s)
	if !r.IsValid() {
		return "", ErrRutaReparacionInvalida
	}
	return r, nil
}

// IsValid reports whether r is a known RutaReparacion value.
func (r RutaReparacion) IsValid() bool {
	switch r {
	case RutaReparacionProveedor, RutaReparacionTaller:
		return true
	}
	return false
}

// String returns the string representation of r.
func (r RutaReparacion) String() string { return string(r) }

// EsProveedor reports whether this is the external-vendor repair route.
func (r RutaReparacion) EsProveedor() bool { return r == RutaReparacionProveedor }

// EsTaller reports whether this is the in-house workshop repair route.
func (r RutaReparacion) EsTaller() bool { return r == RutaReparacionTaller }
