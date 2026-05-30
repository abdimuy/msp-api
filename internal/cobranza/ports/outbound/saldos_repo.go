package outbound

import (
	"context"

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

	// EnRutaPorZona returns ventas abiertas and recently paid (within the
	// last ventanaDias days) for the given zona.
	EnRutaPorZona(ctx context.Context, zonaID, ventanaDias int) ([]domain.Saldo, error)

	// AbiertasPorCliente returns all open saldos (saldo > 0, not cancelled)
	// for the given cliente.
	AbiertasPorCliente(ctx context.Context, clienteID int) ([]domain.Saldo, error)

	// ResumenZonas aggregates open saldos by zona, returning one ResumenZona
	// per zona that has at least one open cargo.
	ResumenZonas(ctx context.Context) ([]domain.ResumenZona, error)
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
