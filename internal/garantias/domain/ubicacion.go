package domain

// Ubicacion represents the physical location of a warranty item.
type Ubicacion string

// All 8 locations from the design.
const (
	UbicacionDomicilioCliente   Ubicacion = "domicilio_cliente"
	UbicacionEnTransito         Ubicacion = "en_transito" //nolint:misspell // "en_transito" es la forma correcta en español, no error ortográfico
	UbicacionAlmacenRevision    Ubicacion = "almacen_revision"
	UbicacionTaller             Ubicacion = "taller"
	UbicacionProveedor          Ubicacion = "proveedor"
	UbicacionAlmacenSegundaMano Ubicacion = "almacen_segunda_mano"
	UbicacionEntregado          Ubicacion = "entregado"
	UbicacionBaja               Ubicacion = "baja"
)

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
