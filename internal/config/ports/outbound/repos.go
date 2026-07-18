// Package outbound defines the interfaces the config module needs from
// outside: its own MSP_CFG_VENDEDOR_MICROSIP / MSP_CFG_ZONA_CAJA repo, the
// read-only Microsip catalogs (LISTAS_ATRIBUTOS, ZONAS_CLIENTES, CAJAS,
// CAJEROS, VENDEDORES, COBRADORES), and the cross-module usuarios reader
// (backed by auth.UsuariosLister).
package outbound

import (
	"context"

	"github.com/google/uuid"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
)

// ConfigRepo persists the vendedor→Microsip mapping in
// MSP_CFG_VENDEDOR_MICROSIP, and the zona→caja/cajero/vendedor/cobrador
// mapping in MSP_CFG_ZONA_CAJA.
type ConfigRepo interface {
	// ListarVendedorMappings returns every row currently stored.
	ListarVendedorMappings(ctx context.Context) ([]configdomain.VendedorMapping, error)
	// UpsertVendedorMapping inserts or updates the mapping for m.UsuarioID.
	UpsertVendedorMapping(ctx context.Context, m configdomain.VendedorMapping) error
	// DeleteVendedorMapping removes the row for usuarioID entirely.
	DeleteVendedorMapping(ctx context.Context, usuarioID uuid.UUID) error

	// ListarZonaCajaConfigs returns every row currently stored in
	// MSP_CFG_ZONA_CAJA.
	ListarZonaCajaConfigs(ctx context.Context) ([]configdomain.ZonaCajaConfig, error)
	// UpsertZonaCajaConfig inserts or updates the mapping for c.ZonaClienteID.
	UpsertZonaCajaConfig(ctx context.Context, c configdomain.ZonaCajaConfig) error
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

// ZonaCajaCatalogoLister reads the five Microsip catalog tables that back
// the zonas/cajas administration screen's dropdowns. Split out from
// CatalogoReader (rather than merged into one interface) to stay under the
// project's interfacebloat limit (max 8 methods per interface).
type ZonaCajaCatalogoLister interface {
	// ListarZonas returns every ZONAS_CLIENTES row (id + NOMBRE).
	ListarZonas(ctx context.Context) ([]configdomain.CatalogoRef, error)
	// ListarCajas returns every CAJAS row (id + NOMBRE).
	ListarCajas(ctx context.Context) ([]configdomain.CatalogoRef, error)
	// ListarCajeros returns every CAJEROS row (id + NOMBRE).
	ListarCajeros(ctx context.Context) ([]configdomain.CatalogoRef, error)
	// ListarVendedoresCatalogo returns every VENDEDORES row (id + NOMBRE).
	ListarVendedoresCatalogo(ctx context.Context) ([]configdomain.CatalogoRef, error)
	// ListarCobradores returns every COBRADORES row (id + NOMBRE).
	ListarCobradores(ctx context.Context) ([]configdomain.CatalogoRef, error)
}

// ZonaCajaCatalogoExistence validates the four slot ids AsignarZonaCaja
// receives against their Microsip catalogs, plus the target zona itself.
// Split out from ZonaCajaCatalogoLister for the same interfacebloat reason.
type ZonaCajaCatalogoExistence interface {
	// ZonaExiste reports whether id exists in ZONAS_CLIENTES.
	ZonaExiste(ctx context.Context, id int) (bool, error)
	// CajaExiste reports whether id exists in CAJAS.
	CajaExiste(ctx context.Context, id int) (bool, error)
	// CajeroExiste reports whether id exists in CAJEROS.
	CajeroExiste(ctx context.Context, id int) (bool, error)
	// VendedorExiste reports whether id exists in VENDEDORES.
	VendedorExiste(ctx context.Context, id int) (bool, error)
	// CobradorExiste reports whether id exists in COBRADORES.
	CobradorExiste(ctx context.Context, id int) (bool, error)
}

// UsuariosReader lists application usuarios for the vendedores screen. The
// production implementation (infra/clients) wraps auth.UsuariosLister.
type UsuariosReader interface {
	ListarUsuarios(ctx context.Context) ([]configdomain.AppUsuario, error)
}
