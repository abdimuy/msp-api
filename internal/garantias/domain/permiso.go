package domain

// Permiso is the code of one guarantee-module permission, as listed in spec
// §7. The module is sealed and cannot import auth, whose catalog is a static
// list, so the codes are declared here as a closed enum and the check travels
// through the Identity port (ports/outbound/identity.go).
//
// The code is the wire value persisted in MSP_ROLES_PERMISOS: it is stable and
// must not be renamed once shipped.
type Permiso string

// The five permissions of spec §7.
const (
	// PermisoLeer grants the bandeja and every read.
	PermisoLeer Permiso = "garantias:leer"
	// PermisoCrear grants opening a folio and adding articles to it.
	PermisoCrear Permiso = "garantias:crear"
	// PermisoActualizar grants advancing an article, recording a diagnosis
	// or a dictamen, and attaching evidence.
	PermisoActualizar Permiso = "garantias:actualizar"
	// PermisoAutorizar grants a physical swap and the standby outcomes. It
	// is a separate code on purpose: a physical swap is decided by the
	// office, and a cobrador must not authorize a replacement from the
	// phone.
	PermisoAutorizar Permiso = "garantias:autorizar"
	// PermisoCerrar grants delivering, closing and canceling a folio.
	PermisoCerrar Permiso = "garantias:cerrar"
)

// ParsePermiso validates and returns a Permiso.
// Returns ErrPermisoInvalido if s is not one of the five recognized codes.
func ParsePermiso(s string) (Permiso, error) {
	p := Permiso(s)
	if !p.IsValid() {
		return "", ErrPermisoInvalido
	}
	return p, nil
}

// IsValid reports whether p is a known Permiso value.
func (p Permiso) IsValid() bool {
	switch p {
	case PermisoLeer, PermisoCrear, PermisoActualizar,
		PermisoAutorizar, PermisoCerrar:
		return true
	}
	return false
}

// String returns the string representation of p.
func (p Permiso) String() string { return string(p) }
