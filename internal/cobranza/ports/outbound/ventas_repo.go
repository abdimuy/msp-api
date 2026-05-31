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
	// desde controla el filtro de saldo en el sync inicial (cursor zero):
	//   - desde zero:   solo cargos activos + tombstones (legacy).
	//   - desde set:    activos + tombstones + saldados con FECHA_ULT_PAGO >= desde.
	// En sync incremental (cursor set) el filtro de saldo se quita: cualquier
	// row con UPDATED_AT > cursor entra, incluyendo ventas que acaban de
	// saldarse y tombstones — el cliente decide qué hacer con cada caso.
	SyncPorZona(ctx context.Context, zonaID int, cursor time.Time, afterID, limit int, desde time.Time) (SyncPage[domain.Venta], error)
}
