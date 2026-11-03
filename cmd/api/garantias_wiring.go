package main

import (
	"context"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/garantias/infra/identidad"
)

// provideGarantiasUsuarioFn adapta el usuario autenticado que auth dejó
// plantado en el contexto al contrato que consume el módulo de garantías.
func provideGarantiasUsuarioFn() identidad.UsuarioFn {
	return func(ctx context.Context) (string, string, bool) {
		usuario, ok := auth.CurrentUserFromContext(ctx)
		if !ok {
			return "", "", false
		}

		return usuario.ID.String(), usuario.Nombre, true
	}
}

// provideGarantiasPermisoFn consulta los permisos efectivos del usuario
// autenticado sin acoplar el módulo de garantías al dominio de auth.
func provideGarantiasPermisoFn() identidad.PermisoFn {
	return func(ctx context.Context, codigo string) (bool, error) {
		usuario, ok := auth.CurrentUserFromContext(ctx)
		if !ok {
			return false, nil
		}

		for _, permiso := range usuario.Permisos {
			if permiso == codigo {
				return true, nil
			}
		}

		return false, nil
	}
}
