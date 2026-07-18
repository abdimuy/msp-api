package auth

import (
	"context"

	"github.com/google/uuid"
)

// UsuarioResumen is the cross-module read view of an application user, exposed
// so other modules (e.g. config) can list users without importing auth/domain.
type UsuarioResumen struct {
	ID      uuid.UUID
	Nombre  string
	Email   string
	Estatus string
}

// UsuariosLister lists application users. Satisfied by the auth app Service.
type UsuariosLister interface {
	ListarUsuarios(ctx context.Context) ([]UsuarioResumen, error)
}
