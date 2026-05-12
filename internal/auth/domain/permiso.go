package domain

import "strings"

// Column-width caps mirrored from the Firebird schema for MSP_PERMISOS.
const (
	maxPermisoCodigoLength      = 60
	maxPermisoDescriptionLength = 255
	maxPermisoCategoriaLength   = 30
)

// Permiso is a catalog entry persisted in MSP_PERMISOS. Unlike Usuario and
// Rol it is a flat value with no audit trail — the catalog is regenerated
// from code on every boot (see PermisoRepo.UpsertCatalog), so per-row
// timestamps would carry no information.
//
// Permiso is conceptually a value object but is implemented as a struct
// with private fields so we can validate at construction and force callers
// through the typed accessors.
type Permiso struct {
	codigo      Permission
	description string
	categoria   string
}

// NewPermiso validates and constructs a Permiso. The description and
// categoria are trimmed; an empty post-trim value is rejected.
func NewPermiso(codigo Permission, description, categoria string) (Permiso, error) {
	codigoStr := string(codigo)
	if codigoStr == "" {
		return Permiso{}, ErrPermisoCodigoRequerido
	}
	if len(codigoStr) > maxPermisoCodigoLength {
		return Permiso{}, ErrPermisoCodigoDemasiadoLargo
	}

	description = strings.TrimSpace(description)
	if description == "" {
		return Permiso{}, ErrPermisoDescripcionRequerida
	}
	if len(description) > maxPermisoDescriptionLength {
		return Permiso{}, ErrPermisoDescripcionDemasiadoLarga
	}

	categoria = strings.TrimSpace(categoria)
	if categoria == "" {
		return Permiso{}, ErrPermisoCategoriaRequerida
	}
	if len(categoria) > maxPermisoCategoriaLength {
		return Permiso{}, ErrPermisoCategoriaDemasiadoLarga
	}

	return Permiso{codigo: codigo, description: description, categoria: categoria}, nil
}

// HydratePermiso rebuilds a Permiso from persistence without validation.
func HydratePermiso(codigo Permission, description, categoria string) Permiso {
	return Permiso{codigo: codigo, description: description, categoria: categoria}
}

// Codigo returns the typed permission code.
func (p Permiso) Codigo() Permission { return p.codigo }

// Description returns the human-readable description.
func (p Permiso) Description() string { return p.description }

// Categoria returns the grouping category for UI display.
func (p Permiso) Categoria() string { return p.categoria }

// Equals reports whether two permisos are identical.
func (p Permiso) Equals(other Permiso) bool {
	return p.codigo == other.codigo &&
		p.description == other.description &&
		p.categoria == other.categoria
}

// ToMeta converts the Permiso into a PermissionMeta, useful for translating
// repository rows into the catalog shape expected by UpsertCatalog callers.
func (p Permiso) ToMeta() PermissionMeta {
	return PermissionMeta{Code: p.codigo, Description: p.description, Categoria: p.categoria}
}
