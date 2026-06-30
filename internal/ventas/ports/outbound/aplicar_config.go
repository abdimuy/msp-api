//nolint:misspell // ventas vocabulary is Spanish per project convention.
package outbound

import (
	"context"

	"github.com/google/uuid"
)

// CajaCajero is the caja + cajero + vendedor + cobrador assigned to a cliente
// zona. The vendedor is the Microsip ruta vendedor (display/quick-reference
// only — the authoritative seller attribution lives in MSP_VENTAS_VENDEDORES).
// VendedorID is -1 when the zona has no matching ruta vendedor. CobradorID is
// the cobrador most frequently assigned to clients in the zona, used by the
// auto-create cliente flow in AplicarVenta; -1 when the zona has no clients
// (e.g. MAYOREO).
type CajaCajero struct {
	CajaID     int
	CajeroID   int
	VendedorID int // -1 cuando no hay (zonas tipo MAYOREO).
	CobradorID int // -1 cuando no hay (zonas tipo MAYOREO).
}

// AplicarDefaults is the singleton MSP_CFG_APLICAR configuration row.
type AplicarDefaults struct {
	SucursalID          int
	FormaCobroContadoID int
	FormaCobroCreditoID int
	// CajaContadoID is the fixed mostrador caja used for contado ventas (no zona).
	// -1 means the column is NULL in MSP_CFG_APLICAR (not yet configured).
	CajaContadoID int
	// CajeroContadoID is the fixed mostrador cajero used for contado ventas (no zona).
	// -1 means the column is NULL in MSP_CFG_APLICAR (not yet configured).
	CajeroContadoID int
}

// AplicarConfig resolves the editable Microsip mapping needed to materialize a
// venta into DOCTOS_PV (the MSP_CFG_* tables). Every lookup miss surfaces a
// specific domain validation error so the operator learns exactly which
// mapping is missing.
type AplicarConfig interface {
	// CajaCajero resolves the caja and cajero for a cliente zona. Returns
	// domain.ErrZonaSinCaja when the zona has no mapping.
	CajaCajero(ctx context.Context, zonaClienteID int) (CajaCajero, error)

	// FormaDePagoID maps a credit frequency (domain FrecPago string) to its
	// Microsip list id. Returns domain.ErrFrecuenciaSinFormaPago when unmapped.
	FormaDePagoID(ctx context.Context, frecuencia string) (int, error)

	// CreditoEnMesesID maps a credit term in months to its Microsip list id.
	// Returns domain.ErrPlazoSinCreditoMeses when unmapped.
	CreditoEnMesesID(ctx context.Context, plazoMeses int) (int, error)

	// NumeroDeVendedoresID maps a seller count to its Microsip list id.
	// Returns domain.ErrNumVendedoresSinMapeo when unmapped.
	NumeroDeVendedoresID(ctx context.Context, n int) (int, error)

	// VendedorListaIDs resolves the three Microsip LISTA_ATRIB_ID values
	// (atributos 19985 / 19986 / 19987) configured for a vendedor usuario in
	// MSP_CFG_VENDEDOR_MICROSIP. Returns the sentinel [3]int{-1, -1, -1} when
	// the usuario has no mapping row; individual unset ids are also -1. A miss
	// is not an error — the seller slot simply stays unmapped.
	VendedorListaIDs(ctx context.Context, usuarioID uuid.UUID) ([3]int, error)

	// Defaults returns the singleton MSP_CFG_APLICAR row. Returns
	// domain.ErrConfigAplicarFaltante when the row is absent.
	Defaults(ctx context.Context) (AplicarDefaults, error)
}
