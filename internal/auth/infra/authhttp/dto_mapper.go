package authhttp

import (
	"time"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/app"
	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// toUsuarioResponse projects a domain.Usuario into its JSON DTO.
func toUsuarioResponse(u *domain.Usuario) UsuarioResponse {
	var tel *string
	if u.Telefono() != nil {
		v := u.Telefono().Value()
		tel = &v
	}
	return UsuarioResponse{
		ID:          u.ID().String(),
		FirebaseUID: u.FirebaseUID().Value(),
		Email:       u.Email().Value(),
		Nombre:      u.Nombre().Value(),
		Telefono:    tel,
		AlmacenID:   u.AlmacenID(),
		Activo:      u.Activo(),
		CreatedAt:   u.CreatedAt().UTC().Format(time.RFC3339Nano),
		UpdatedAt:   u.UpdatedAt().UTC().Format(time.RFC3339Nano),
	}
}

// toRolResponse projects a domain.Rol into its JSON DTO.
func toRolResponse(r *domain.Rol) RolResponse {
	return RolResponse{
		ID:          r.ID().String(),
		Nombre:      r.Nombre(),
		Description: r.Description(),
		Inmutable:   r.Inmutable(),
		Activo:      r.Activo(),
		CreatedAt:   r.CreatedAt().UTC().Format(time.RFC3339Nano),
		UpdatedAt:   r.UpdatedAt().UTC().Format(time.RFC3339Nano),
	}
}

// toPermisoResponse projects a domain.Permiso into its JSON DTO.
func toPermisoResponse(p *domain.Permiso) PermisoResponse {
	return PermisoResponse{
		Codigo:      p.Codigo().Code(),
		Description: p.Description(),
		Categoria:   p.Categoria(),
	}
}

// toEnsureVendedoresResponse projects the service-layer results into the JSON
// DTO. Order is preserved.
func toEnsureVendedoresResponse(results []app.VendedorEnsureResult) EnsureVendedoresResponse {
	items := make([]VendedorEnsureResponse, 0, len(results))
	for _, r := range results {
		items = append(items, VendedorEnsureResponse{
			Email:     r.Email,
			UsuarioID: r.UsuarioID.String(),
		})
	}
	return EnsureVendedoresResponse{Vendedores: items}
}

// toCurrentUserResponse projects the cross-module auth.CurrentUser into its
// JSON DTO. The Permisos slice is copied so the response cannot mutate the
// context value.
func toCurrentUserResponse(u auth.CurrentUser) CurrentUserResponse {
	codes := make([]string, len(u.Permisos))
	copy(codes, u.Permisos)
	return CurrentUserResponse{
		ID:          u.ID.String(),
		FirebaseUID: u.FirebaseUID,
		Email:       u.Email,
		Nombre:      u.Nombre,
		AlmacenID:   u.AlmacenID,
		Permisos:    codes,
	}
}
