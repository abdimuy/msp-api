// Package outbound defines the interfaces the config module needs from
// outside: its own MSP_CFG_VENDEDOR_MICROSIP repo, the read-only Microsip
// LISTAS_ATRIBUTOS catalog, and the cross-module usuarios reader (backed by
// auth.UsuariosLister).
package outbound

import (
	"context"

	"github.com/google/uuid"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
)

// ConfigRepo persists the vendedor→Microsip mapping in
// MSP_CFG_VENDEDOR_MICROSIP.
type ConfigRepo interface {
	// ListarVendedorMappings returns every row currently stored.
	ListarVendedorMappings(ctx context.Context) ([]configdomain.VendedorMapping, error)
	// UpsertVendedorMapping inserts or updates the mapping for m.UsuarioID.
	UpsertVendedorMapping(ctx context.Context, m configdomain.VendedorMapping) error
	// DeleteVendedorMapping removes the row for usuarioID entirely.
	DeleteVendedorMapping(ctx context.Context, usuarioID uuid.UUID) error
}

// CatalogoReader reads the Microsip LISTAS_ATRIBUTOS catalog for the
// credit-vendor attributes (19985/19986/19987). Read-only — Microsip's own
// tables are never written by this module.
type CatalogoReader interface {
	// ResolverNombresLista returns lista_id → VALOR_DESPLEGADO for the given ids.
	ResolverNombresLista(ctx context.Context, listaIDs []int) (map[int]string, error)
	// ListarIdentidadesMicrosip groups LISTAS_ATRIBUTOS rows for the three
	// credit-vendor attributes by VALOR_DESPLEGADO.
	ListarIdentidadesMicrosip(ctx context.Context) ([]configdomain.IdentidadMicrosip, error)
	// ListaIDPerteneceAtributo reports whether listaID exists under atributoID.
	ListaIDPerteneceAtributo(ctx context.Context, listaID, atributoID int) (bool, error)
}

// UsuariosReader lists application usuarios for the vendedores screen. The
// production implementation (infra/clients) wraps auth.UsuariosLister.
type UsuariosReader interface {
	ListarUsuarios(ctx context.Context) ([]configdomain.AppUsuario, error)
}
