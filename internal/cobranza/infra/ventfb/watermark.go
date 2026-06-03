package ventfb

import (
	"context"
	"math"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// SentinelNoActiveTx is the watermark value returned when MON$TRANSACTIONS
// reports no active transactions — every committed row passes "TX_ID < watermark".
// Matches Firebird's 0x7FFFFFFFFFFFFFFF (max BIGINT positive).
const SentinelNoActiveTx int64 = math.MaxInt64

// MinActiveTransactionID returns the smallest MON$TRANSACTION_ID of any
// currently-active (state=1) transaction on the database, or SentinelNoActiveTx
// if none are active.
//
// Callers use this as a watermark to exclude changelog rows written by
// in-flight transactions whose commit visibility is not yet ordered after
// our cursor. By construction, any committed transaction has a TX_ID smaller
// than every currently-active TX_ID — so "TX_ID < watermark" yields only
// definitely-committed rows.
//
// MON$STATE codes (Firebird): 0=idle, 1=active, 2=limbo, 3=committed,
// 4=rolled back. Only state=1 is in-flight from our point of view; states
// 2/3/4 are terminal.
//
// Note on isolation: this function uses pool.DB directly (bypassing any active
// transaction in the context). This is intentional: MON$TRANSACTIONS reflects
// live server state, not a versioned snapshot. If the caller is inside a
// snapshot transaction and we used GetQuerier, Firebird would see that
// transaction's own snapshot of MON$TRANSACTIONS — which includes the caller's
// own tx as "active" and may miss other recently-started transactions on other
// connections. Using pool.DB ensures a fresh auto-commit read that sees the
// true current state of all active transactions on the server.
//
// Cross-connection invariant: if connection A holds open transaction T_A, then
// MinActiveTransactionID called on any connection B returns a value <= T_A.
// This is the watermark guarantee the listener relies on.
//
// Note: this query targets MON$TRANSACTIONS which is a monitoring table.
// On Firebird 2.5+ it requires no special privilege when the caller is SYSDBA
// or has SELECT on the table. The dev DB satisfies this.
func MinActiveTransactionID(ctx context.Context, pool *firebird.Pool) (int64, error) {
	// Consulta directa en pool.DB (auto-commit) para ver el estado real del servidor,
	// sin heredar el nivel de aislamiento de ninguna transacción activa en el contexto.
	var minTx *int64
	err := pool.DB.QueryRowContext(
		ctx, `
SELECT MIN(MON$TRANSACTION_ID) FROM MON$TRANSACTIONS WHERE MON$STATE = 1`,
	).Scan(&minTx)
	if err != nil {
		return 0, firebird.MapError(err)
	}
	if minTx == nil {
		// No hay transacciones activas: todo está committed.
		// El sentinel garantiza que TX_ID < watermark sea verdad para cualquier fila.
		return SentinelNoActiveTx, nil
	}
	return *minTx, nil
}
