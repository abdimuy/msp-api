package outbound

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// SyncPage is the result of a cursor-based sync page. It carries the items
// plus the metadata the client needs to advance its cursor on the next call
// without trusting its local clock.
type SyncPage[T any] struct {
	// Items returned by this page. Empty when there are no rows since cursor.
	Items []T
	// MaxUpdatedAt is the highest UPDATED_AT in Items. When Items is empty,
	// equals the cursor the caller supplied (so the client never goes
	// backwards). Always UTC.
	MaxUpdatedAt time.Time
	// ServerNow is the server wall-clock at query time, for clock-skew
	// detection by clients. Always UTC.
	ServerNow time.Time
	// HasMore signals that more rows past Items exist for the same cursor —
	// the client should call again with the same cursor and pass AfterID =
	// MaxItemID to paginate.
	HasMore bool
}

// PagosRepo reads from the materialized cache MSP_PAGOS_VENTAS.
type PagosRepo interface {
	// PorVenta returns every pago acreditado al cargo DOCTO_CC_ACR_ID =
	// doctoCCID, ordered by FECHA ascending. Returns an empty slice when
	// none exist (not an error — the cargo may simply have no pagos yet).
	PorVenta(ctx context.Context, doctoCCID int) ([]domain.Pago, error)

	// PorCliente returns every pago made by the given cliente, ordered by
	// FECHA descending (most recent first).
	PorCliente(ctx context.Context, clienteID int) ([]domain.Pago, error)

	// EnRutaPorZona returns pagos hechos en la zona desde `desde`. When desde
	// is the zero time, returns all pagos for the zone. Ordered by FECHA
	// descending. This is the human-readable filter (by pago timestamp).
	EnRutaPorZona(ctx context.Context, zonaID int, desde time.Time) ([]domain.Pago, error)

	// SyncPorZona returns a page of pagos whose UPDATED_AT > cursor AND
	// UPDATED_AT <= server_now - 5 seconds (lag to avoid losing in-flight
	// transactions). Items are ordered by (UPDATED_AT, IMPTE_DOCTO_CC_ID)
	// ascending; afterID is used for sub-cursor pagination when has_more=true.
	// Pass cursor=time.Time{} for a full initial sync.
	//
	// desde controla el filtro de saldo en el sync inicial (cursor zero):
	//   - desde zero:   solo pagos de cargos con saldo activo (legacy).
	//   - desde set:    pagos de cargos activos + pagos cuyo p.FECHA >= desde
	//                   (incluye pagos finales que saldaron una venta).
	// En sync incremental (cursor set) el filtro de saldo se quita; el filtro
	// de concepto (87327, 27969) se mantiene siempre.
	SyncPorZona(ctx context.Context, zonaID int, cursor time.Time, afterID, limit int, desde time.Time) (SyncPage[domain.Pago], error)
}

// PagosRecomputer wraps the MSP_RECOMPUTE_PAGO stored procedure. Used only
// by the reconcile path to refresh a pago row when drift is suspected.
type PagosRecomputer interface {
	// Recompute calls EXECUTE PROCEDURE MSP_RECOMPUTE_PAGO(impteID).
	Recompute(ctx context.Context, impteID int) error
}

// PagosLister iterates over MSP_PAGOS_VENTAS in pages for the reconciler.
type PagosLister interface {
	// Page returns up to limit IMPTE_DOCTO_CC_IDs starting AFTER cursorAfter
	// (pass 0 to start from the beginning). IDs are sorted ascending so the
	// reconciler can resume from the last seen ID. nextCursor is 0 when fewer
	// than limit rows remain.
	Page(ctx context.Context, cursorAfter, limit int) (ids []int, nextCursor int, err error)
}

// PagosTombstoneCleaner physically deletes pago rows marked as cancelled
// (CANCELADO='S') whose UPDATED_AT is older than the cutoff. Mirrors
// SaldosTombstoneCleaner. Used by the reconciler to keep
// MSP_PAGOS_VENTAS bounded — any mobile client that hasn't synced for
// >cutoff has already lost its session and will resync from scratch.
type PagosTombstoneCleaner interface {
	// DeleteTombstonesOlderThan deletes tombstones whose UPDATED_AT < cutoff
	// and returns how many rows were removed.
	DeleteTombstonesOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}
