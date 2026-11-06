package domain

import "slices"

// Ubicacion represents the physical location of a warranty item.
type Ubicacion string

// All 8 locations from the design.
const (
	UbicacionDomicilioCliente   Ubicacion = "domicilio_cliente"
	UbicacionEnTransito         Ubicacion = "en_transito"
	UbicacionAlmacenRevision    Ubicacion = "almacen_revision"
	UbicacionTaller             Ubicacion = "taller"
	UbicacionProveedor          Ubicacion = "proveedor"
	UbicacionAlmacenSegundaMano Ubicacion = "almacen_segunda_mano"
	UbicacionEntregado          Ubicacion = "entregado"
	UbicacionBaja               Ubicacion = "baja"
)

// ubicacionesOrdenadas lists every location once, in the order the design
// presents (the declaration order above). It is the stable order the screen
// catalog hand out; kept next to the constants so adding a location forces this
// list to be updated too (R11).
var ubicacionesOrdenadas = []Ubicacion{
	UbicacionDomicilioCliente, UbicacionEnTransito, UbicacionAlmacenRevision,
	UbicacionTaller, UbicacionProveedor, UbicacionAlmacenSegundaMano,
	UbicacionEntregado, UbicacionBaja,
}

// Ubicaciones returns every location in a stable order, as a fresh slice the
// caller may keep or reorder. It exposes the catalog without handing out the
// internal slice, so the frontend does not reimplement the set (R11).
func Ubicaciones() []Ubicacion { return slices.Clone(ubicacionesOrdenadas) }

// ParseUbicacion validates and returns an Ubicacion.
// Returns ErrUbicacionInvalida if s is not one of the 8 recognized locations.
func ParseUbicacion(s string) (Ubicacion, error) {
	u := Ubicacion(s)
	if !u.IsValid() {
		return "", ErrUbicacionInvalida
	}
	return u, nil
}

// IsValid reports whether u is a known Ubicacion value.
func (u Ubicacion) IsValid() bool {
	switch u {
	case UbicacionDomicilioCliente, UbicacionEnTransito,
		UbicacionAlmacenRevision, UbicacionTaller,
		UbicacionProveedor, UbicacionAlmacenSegundaMano,
		UbicacionEntregado, UbicacionBaja:
		return true
	}
	return false
}

// String returns the string representation of u.
func (u Ubicacion) String() string { return string(u) }
