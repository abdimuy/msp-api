//nolint:misspell // Spanish domain vocabulary (cliente, venta, cobranza, zona) per project convention.
package outbound

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// VentasRepo reads the enriched venta projection for incremental sync. Each
// row JOINs MSP_SALDOS_VENTAS with CLIENTES, DIRS_CLIENTES, ZONAS_CLIENTES,
// COBRADORES, LIBRES_CARGOS_CC and DOCTOS_PV so the mobile app gets every
// field needed to render a route in a single round-trip.
//
// The cursor is MSP_SALDOS_VENTAS.UPDATED_AT — same column that drives
// SaldosRepo.SyncPorZona. Tombstones (CARGO_CANCELADO='S') are included so the
// client can propagate cancellations.
type VentasRepo interface {
	// SyncPorZona returns a page of ventas whose underlying saldo row was
	// updated after cursor AND at most server_now - 5 seconds (lag window).
	// Items ordered by (UPDATED_AT, DOCTO_CC_ID) ascending; afterID is used
	// for sub-cursor pagination when has_more=true. Pass cursor=time.Time{}
	// for a full initial sync.
	//
	// desde acota la ventana en TODAS las páginas (no solo la primera):
	//   - desde zero:   solo cargos activos + tombstones.
	//   - desde set:    activos + saldados con FECHA_ULT_PAGO >= desde +
	//                   tombstones cancelados dentro de la ventana.
	//
	// El HTTP nunca pasa zero: app.ResolveSyncDesde le da un default de
	// servidor. Zero queda para llamadas internas y tests.
	SyncPorZona(ctx context.Context, zonaID int, cursor time.Time, afterID, limit int, desde time.Time) (SyncPage[domain.Venta], error)

	// ByIDs returns the enriched Venta rows for the given primary keys
	// (DOCTO_CC_IDs) constrained to ZONA_CLIENTE_ID = zonaID. Uses the same
	// selectVentaCols + ventaFromClause JOIN as SyncPorZona so the response
	// shape is identical to GET /sync/ventas/zona/{id}. Rows whose PK is in
	// ids but whose zona does not match are silently excluded (authorization
	// filter, not 404). No watermark filtering — callers obtain these PKs
	// from the SSE listener which only publishes committed rows.
	//
	// desde aplica el MISMO filtro de ventana que SyncPorZona y que
	// SaldosReconcileRepo.ListIDs — ver la nota equivalente en PagosRepo.ByIDs.
	//
	// ids may contain duplicates; the result deduplicates by PK.
	ByIDs(ctx context.Context, zonaID int, ids []int, desde time.Time) ([]domain.Venta, error)

	// ProductosByPVIDs returns the DOCTOS_PV_DET line items for every DOCTOS_PV
	// document in pvIDs, keyed by DOCTO_PV_ID. Batch query (anti N+1). Documents
	// with no lines are absent from the map. ids may contain duplicates.
	ProductosByPVIDs(ctx context.Context, pvIDs []int) (map[int][]domain.ProductoVenta, error)
}
