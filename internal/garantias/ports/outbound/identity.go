package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// Usuario is the authenticated operator behind a request. The name is what
// MSP_GA_EVENTO.USUARIO and MSP_GA_GARANTIA.ABIERTO_POR record; the id is
// carried so an adapter can resolve the name itself.
type Usuario struct {
	ID     string
	Nombre string
}

// Identity is the ONLY way the module learns who is calling and what they may
// do. Garantías is a sealed module (spec §2.2) and cannot import auth, whose
// permission catalog is a static list: the codes live here as domain.Permiso
// and the check goes through this port. Registering the five codes in the auth
// catalog is a line in the composition root, read from garantias.Permisos().
//
// The permission rule lives in app, not in http: the command knows which
// action is being asked for, and the handler only knows the route.
type Identity interface {
	// UsuarioActual returns the authenticated user, or
	// domain.ErrUsuarioNoAutenticado.
	UsuarioActual(ctx context.Context) (Usuario, error)
	// TienePermiso reports whether the current user holds p.
	TienePermiso(ctx context.Context, p domain.Permiso) (bool, error)
}
