package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
)

// listarUsuariosPageSize is the page size used internally to paginate through
// the full usuarios collection when building the cross-module resumen list.
const listarUsuariosPageSize = 200

// var _ auth.UsuariosLister asserts *Service satisfies the cross-module
// contract other modules (e.g. config) depend on to list application users.
var _ auth.UsuariosLister = (*Service)(nil)

// ListarUsuarios returns every usuario in the system, mapped to the
// cross-module auth.UsuarioResumen view. It paginates internally through
// outbound.UsuarioRepo.List until the repo reports no further pages, so
// callers never need to manage cursors themselves.
func (s *Service) ListarUsuarios(ctx context.Context) ([]auth.UsuarioResumen, error) {
	var out []auth.UsuarioResumen
	cursor := ""
	for {
		page, err := s.usuarios.List(ctx, outbound.ListParams{
			Cursor:   cursor,
			PageSize: listarUsuariosPageSize,
		})
		if err != nil {
			return nil, err
		}
		if len(page.Items) == 0 {
			break
		}
		for _, u := range page.Items {
			out = append(out, auth.UsuarioResumen{
				ID:      u.ID(),
				Nombre:  u.Nombre().Value(),
				Email:   u.Email().Value(),
				Estatus: string(u.Estatus()),
			})
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}
	return out, nil
}
