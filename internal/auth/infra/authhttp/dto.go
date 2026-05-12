// Package authhttp hosts the auth module's HTTP transport: handlers,
// middleware, DTOs, and the chi router mount point. It is the outermost
// adapter layer — nothing inside the auth module imports it.
package authhttp

// LoginRequest is the body accepted by POST /auth/login. The id_token is the
// Firebase ID token issued by the client SDK; the server verifies its
// signature, looks up (or creates) the matching usuario, and returns the
// authenticated principal projection.
type LoginRequest struct {
	IDToken string `json:"id_token" validate:"required"`
}

// ActualizarUsuarioRequest is the body accepted by PATCH /usuarios/{id}.
type ActualizarUsuarioRequest struct {
	Email     string  `json:"email"                validate:"required,email"`
	Nombre    string  `json:"nombre"               validate:"required,min=1,max=200"`
	Telefono  *string `json:"telefono,omitempty"`
	AlmacenID *int    `json:"almacen_id,omitempty"`
}

// AsignarRolRequest is the body accepted by POST /usuarios/{id}/roles.
type AsignarRolRequest struct {
	RolID string `json:"rol_id" validate:"required,uuid"`
}

// CrearRolRequest is the body accepted by POST /roles.
type CrearRolRequest struct {
	Nombre      string  `json:"nombre"                validate:"required,min=1,max=50"`
	Description *string `json:"description,omitempty"`
}

// ActualizarRolRequest is the body accepted by PATCH /roles/{id}.
type ActualizarRolRequest struct {
	Nombre      string  `json:"nombre"                validate:"required,min=1,max=50"`
	Description *string `json:"description,omitempty"`
}

// AsignarPermisoRequest is the body accepted by POST /roles/{id}/permisos.
type AsignarPermisoRequest struct {
	Codigo string `json:"codigo" validate:"required,min=1,max=60"`
}

// UsuarioResponse is the JSON projection of a domain.Usuario.
type UsuarioResponse struct {
	ID          string  `json:"id"`
	FirebaseUID string  `json:"firebase_uid"`
	Email       string  `json:"email"`
	Nombre      string  `json:"nombre"`
	Telefono    *string `json:"telefono,omitempty"`
	AlmacenID   *int    `json:"almacen_id,omitempty"`
	Activo      bool    `json:"activo"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// RolResponse is the JSON projection of a domain.Rol.
type RolResponse struct {
	ID          string  `json:"id"`
	Nombre      string  `json:"nombre"`
	Description *string `json:"description,omitempty"`
	Inmutable   bool    `json:"inmutable"`
	Activo      bool    `json:"activo"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// PermisoResponse is the JSON projection of a domain.Permiso.
type PermisoResponse struct {
	Codigo      string `json:"codigo"`
	Description string `json:"description"`
	Categoria   string `json:"categoria"`
}

// CurrentUserResponse is the JSON projection of the cross-module auth
// CurrentUser, returned by GET /me and POST /auth/login.
type CurrentUserResponse struct {
	ID          string   `json:"id"`
	FirebaseUID string   `json:"firebase_uid"`
	Email       string   `json:"email"`
	Nombre      string   `json:"nombre"`
	AlmacenID   *int     `json:"almacen_id,omitempty"`
	Permisos    []string `json:"permisos"`
}

// ListResponse is the generic cursor-paginated envelope returned by the List
// endpoints. NextCursor is the empty string when there are no more pages.
type ListResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}
