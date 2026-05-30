package outbound

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// SaldosRepo reads from the materialized cache MSP_SALDOS_VENTAS.
// All reads are expected to be sub-10ms thanks to PK or covering indexes.
type SaldosRepo interface {
	// PorVenta returns the saldo for the given PV document ID. Returns
	// ErrSaldoNoEncontrado when no cache row exists (the trigger hasn't fired
	// yet or the venta was never applied in Microsip).
	PorVenta(ctx context.Context, doctoPVID int) (*domain.Saldo, error)

	// PorCargo returns the saldo for the given cargo (DOCTOS_CC) ID. Returns
	// ErrSaldoNoEncontrado when no cache row exists.
	PorCargo(ctx context.Context, doctoCCID int) (*domain.Saldo, error)

	// EnRutaPorZona returns ventas abiertas (saldo > 0) for the given zona,
	// plus ventas saldadas whose FECHA_ULT_PAGO >= desde (when desde is
	// non-zero). Pass time.Time{} (the zero value) to suppress the
	// recently-paid branch entirely. desde is truncated to DATE precision by
	// the underlying column type.
	EnRutaPorZona(ctx context.Context, zonaID int, desde time.Time) ([]domain.Saldo, error)

	// AbiertasPorCliente returns all open saldos (saldo > 0, not cancelled)
	// for the given cliente.
	AbiertasPorCliente(ctx context.Context, clienteID int) ([]domain.Saldo, error)

	// ResumenZonas aggregates open saldos by zona, returning one ResumenZona
	// per zona that has at least one open cargo.
	ResumenZonas(ctx context.Context) ([]domain.ResumenZona, error)

	// SyncPorZona returns a page of saldos whose UPDATED_AT > cursor AND
	// UPDATED_AT <= server_now - 5 seconds (lag window). Tombstones
	// (CARGO_CANCELADO='S') ARE included so the client can propagate
	// cancellations. Items ordered by (UPDATED_AT, DOCTO_CC_ID) ascending;
	// afterID is used for sub-cursor pagination when has_more=true.
	// Pass cursor=time.Time{} for a full initial sync.
	SyncPorZona(ctx context.Context, zonaID int, cursor time.Time, afterID, limit int) (SyncPage[domain.Saldo], error)
}

// SaldosTombstoneCleaner physically deletes saldo rows marked as cancelled
// (CARGO_CANCELADO='S') whose UPDATED_AT is older than the cutoff. Used by
// the reconciler to keep the cache bounded — any mobile client that hasn't
// synced for >cutoff has already lost its session and will resync from
// scratch anyway.
type SaldosTombstoneCleaner interface {
	// DeleteTombstonesOlderThan deletes tombstones whose UPDATED_AT < cutoff
	// and returns how many rows were removed.
	DeleteTombstonesOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}

// SaldosRecomputer wraps the MSP_RECOMPUTE_SALDO_VENTA stored procedure.
// Used ONLY by the reconcile path to refresh a cargo when drift is suspected.
// The Microsip triggers handle recomputation atomically for ordinary writes —
// this is the "force resync" escape hatch.
type SaldosRecomputer interface {
	// Recompute calls EXECUTE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA(cargoCCID),
	// then re-reads the row from MSP_SALDOS_VENTAS and returns the refreshed
	// snapshot so callers can compare it against the pre-call state.
	Recompute(ctx context.Context, cargoCCID int) (*domain.Saldo, error)
}

// SaldosLister iterates over MSP_SALDOS_VENTAS in pages. Used only by the
// Reconciler to walk all rows for drift detection. It is a separate port
// because not all consumers need full-table iteration.
type SaldosLister interface {
	// Page returns up to limit cargo IDs (from MSP_SALDOS_VENTAS) starting
	// AFTER cursorAfter (pass 0 to start from the beginning). IDs are sorted
	// ascending so the reconciler can resume from the last seen ID.
	// nextCursor is 0 when fewer than limit rows remain (end of table).
	Page(ctx context.Context, cursorAfter, limit int) (ids []int, nextCursor int, err error)
}
