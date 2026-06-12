//nolint:misspell // ventas vocabulary is Spanish per project convention.
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// MicrosipVentaInput carries every resolved identifier the adapter needs to
// materialize a venta into Microsip's DOCTOS_PV family. All IDs are looked up
// by the application command before calling the adapter.
type MicrosipVentaInput struct {
	// Venta is the source aggregate (cliente_id, productos, montos, tipo, plan).
	Venta *domain.Venta

	// CajaID is the caja for this zona de cliente.
	CajaID int
	// CajeroID is the cajero for this zona de cliente.
	CajeroID int
	// VendedorID is the Microsip ruta vendedor for the zona (-1 when none).
	// Kept for reference/diagnostics; LIBRES_CARGOS_CC.VENDEDOR_1/2/3 now use
	// the per-vendedor LISTA_ATRIB_ID mapping in VendedorListaIDs.
	VendedorID int
	// VendedorListaIDs holds the resolved LISTA_ATRIB_ID written to
	// LIBRES_CARGOS_CC.VENDEDOR_1 / VENDEDOR_2 / VENDEDOR_3 respectively. Each
	// position is the seller in that slot mapped through atributos
	// 19985/19986/19987 (see MSP_CFG_VENDEDOR_MICROSIP); a slot with no seller
	// or no mapping is the sentinel -1.
	VendedorListaIDs [3]int
	// SucursalID is the Microsip sucursal for the sale (usually 225490 Matriz).
	SucursalID int

	// FormaCobroID selects contado (67) or crédito (71) in DOCTOS_PV_COBROS.
	FormaCobroID int
	// FormaDePagoID is the LISTAS_ATRIBUTOS id for LIBRES_CARGOS_CC.FORMA_DE_PAGO.
	// Nil for CONTADO ventas.
	FormaDePagoID *int
	// CreditoEnMesesID is the LISTAS_ATRIBUTOS id for CREDITO_EN_MESES.
	// Nil for CONTADO ventas.
	CreditoEnMesesID *int
	// NumeroDeVendedoresID is the LISTAS_ATRIBUTOS id for NUMERO_DE_VENDEDORES.
	NumeroDeVendedoresID int
}

// MicrosipVentaResult is returned by MicrosipVentaWriter.Aplicar after the
// cascade has completed successfully within the caller's transaction.
type MicrosipVentaResult struct {
	// DoctoPVID is the DOCTOS_PV.DOCTO_PV_ID assigned by the trigger.
	DoctoPVID int
	// Folio is the final folio string (SERIE + LPAD(CONSECUTIVO,8,'0')).
	Folio string
}

// MicrosipVentaWriter materializes an approved MSP venta into Microsip's
// DOCTOS_PV ledger. The implementation must execute all INSERTs and UPDATEs
// (phases 1-7 of the write recipe) within the caller's ambient transaction so
// the Microsip cascade and the MSP header update are atomic.
type MicrosipVentaWriter interface {
	// Aplicar writes the DOCTOS_PV header, detalle lines, cobro, campos libres,
	// flips APLICADO N→S (firing the cascade), updates the folio counter, and —
	// for CREDITO ventas — inserts LIBRES_CARGOS_CC and optionally the enganche
	// document. It must join the caller's transaction via GetQuerier(ctx, pool).
	Aplicar(ctx context.Context, in MicrosipVentaInput) (MicrosipVentaResult, error)
}
