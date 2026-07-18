// Package clients holds the config module's cross-module outbound adapters.
package clients

import (
	"context"

	"github.com/abdimuy/msp-api/internal/auth"
	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
	"github.com/abdimuy/msp-api/internal/config/ports/outbound"
)

// authUsuariosClient adapts auth.UsuariosLister (satisfied by the auth
// module's app.Service) to the config module's outbound.UsuariosReader port.
type authUsuariosClient struct {
	lister auth.UsuariosLister
}

// NewAuthUsuariosClient wraps lister as a config outbound.UsuariosReader.
func NewAuthUsuariosClient(lister auth.UsuariosLister) outbound.UsuariosReader {
	return &authUsuariosClient{lister: lister}
}

// Compile-time assertion: authUsuariosClient satisfies the outbound port.
var _ outbound.UsuariosReader = (*authUsuariosClient)(nil)

// ListarUsuarios lists every application usuario, mapping the auth module's
// cross-module contract type into config's own AppUsuario read-model.
func (c *authUsuariosClient) ListarUsuarios(ctx context.Context) ([]configdomain.AppUsuario, error) {
	resumenes, err := c.lister.ListarUsuarios(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]configdomain.AppUsuario, len(resumenes))
	for i, r := range resumenes {
		out[i] = configdomain.AppUsuario{
			ID:      r.ID,
			Nombre:  r.Nombre,
			Email:   r.Email,
			Estatus: r.Estatus,
		}
	}
	return out, nil
}
