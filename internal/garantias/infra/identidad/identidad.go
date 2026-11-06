// Package identidad adapta la identidad del usuario al módulo de garantías.
package identidad

import (
	"context"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

// UsuarioFn obtiene el usuario autenticado desde el contexto.
type UsuarioFn func(ctx context.Context) (id, nombre string, ok bool)

// PermisoFn consulta si el usuario actual tiene un permiso.
type PermisoFn func(ctx context.Context, codigo string) (bool, error)

// Identidad adapta las funciones de autenticación al puerto outbound.Identity.
type Identidad struct {
	usuario UsuarioFn
	permiso PermisoFn
}

var _ outbound.Identity = (*Identidad)(nil)

// New crea el adaptador de identidad.
func New(usuario UsuarioFn, permiso PermisoFn) *Identidad {
	if usuario == nil {
		panic("identidad: UsuarioFn no puede ser nil")
	}

	if permiso == nil {
		panic("identidad: PermisoFn no puede ser nil")
	}

	return &Identidad{
		usuario: usuario,
		permiso: permiso,
	}
}

// UsuarioActual devuelve el usuario autenticado.
func (i *Identidad) UsuarioActual(ctx context.Context) (outbound.Usuario, error) {
	id, nombre, ok := i.usuario(ctx)
	if !ok || nombre == "" {
		return outbound.Usuario{}, domain.ErrUsuarioNoAutenticado
	}

	return outbound.Usuario{
		ID:     id,
		Nombre: nombre,
	}, nil
}

// TienePermiso consulta si el usuario tiene el permiso solicitado.
func (i *Identidad) TienePermiso(
	ctx context.Context,
	p domain.Permiso,
) (bool, error) {
	if !p.IsValid() {
		return false, domain.ErrPermisoInvalido
	}

	return i.permiso(ctx, p.String())
}
