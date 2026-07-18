package domain

import "github.com/google/uuid"

// AppUsuario is the config module's own view of an application user — the
// authUsuariosClient adapter maps auth.UsuarioResumen into this shape so the
// app layer never depends on the auth module's domain types directly.
type AppUsuario struct {
	ID      uuid.UUID
	Nombre  string
	Email   string
	Estatus string
}

// VendedorSlot is one resolved VENDEDOR_LISTA_ID_n slot: the Microsip
// LISTA_ATRIB_ID plus its VALOR_DESPLEGADO (display name).
type VendedorSlot struct {
	ListaID int
	Nombre  string
}

// VendedorAsignacion is one row of the vendedores administration screen: an
// application usuario alongside its (possibly partial) mapping to the three
// Microsip credit-vendor slots.
type VendedorAsignacion struct {
	UsuarioID uuid.UUID
	Nombre    string
	Email     string
	V1        *VendedorSlot
	V2        *VendedorSlot
	V3        *VendedorSlot
	Estado    string
}

// NewVendedorAsignacion builds a VendedorAsignacion, deriving Estado from how
// many of the three slots are populated.
func NewVendedorAsignacion(usuarioID uuid.UUID, nombre, email string, v1, v2, v3 *VendedorSlot) VendedorAsignacion {
	n := 0
	for _, v := range []*VendedorSlot{v1, v2, v3} {
		if v != nil {
			n++
		}
	}
	return VendedorAsignacion{
		UsuarioID: usuarioID,
		Nombre:    nombre,
		Email:     email,
		V1:        v1,
		V2:        v2,
		V3:        v3,
		Estado:    estadoAsignacion(n),
	}
}

// estadoAsignacion renders how many of the three vendedor slots are filled
// as a short status label for the administration UI.
func estadoAsignacion(n int) string {
	switch n {
	case 3:
		return "3/3"
	case 2:
		return "2/3"
	case 1:
		return "1/3"
	default:
		return "sin asignar"
	}
}

// CatalogoRef is a resolved reference into a Microsip catalog table (zona,
// caja, cajero, vendedor, or cobrador): the row's id plus its NOMBRE.
type CatalogoRef struct {
	ID     int
	Nombre string
}

// ZonaCajaAsignacion is one row of the zonas/cajas administration screen: a
// client zone (ZONAS_CLIENTES) alongside its (possibly partial) mapping to
// caja/cajero/vendedor/cobrador. A nil ref means that slot is unmapped —
// either the zone has no MSP_CFG_ZONA_CAJA row at all, or the row has
// SinMapeoZonaCaja (-1) in that column.
type ZonaCajaAsignacion struct {
	ZonaClienteID int
	ZonaNombre    string
	Caja          *CatalogoRef
	Cajero        *CatalogoRef
	Vendedor      *CatalogoRef
	Cobrador      *CatalogoRef
}

// OpcionesZonasCajas lists every catalog needed to populate the zonas/cajas
// administration screen's dropdowns.
type OpcionesZonasCajas struct {
	Zonas      []CatalogoRef
	Cajas      []CatalogoRef
	Cajeros    []CatalogoRef
	Vendedores []CatalogoRef
	Cobradores []CatalogoRef
}

// IdentidadMicrosip is one Microsip "persona" grouped by VALOR_DESPLEGADO
// across the three credit-vendor attributes (19985/19986/19987) — the
// per-attribute LISTA_ATRIB_ID values an admin picks from when assigning a
// VendedorMapping. MatchCount is how many of the three attributes have a row
// for this name (3 = fully resolvable identity; less means the Microsip
// catalog is missing an entry for one or more attributes).
type IdentidadMicrosip struct {
	Nombre     string
	V1ListaID  *int
	V2ListaID  *int
	V3ListaID  *int
	MatchCount int
}
