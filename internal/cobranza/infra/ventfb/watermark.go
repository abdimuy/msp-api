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
// currently-active (state=1) transaction on the database OTHER THAN this
// query's own implicit transaction, or SentinelNoActiveTx if none are
// active.
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
// Self-exclusion via CURRENT_TRANSACTION: the SELECT itself runs inside a
// transaction whose own row in MON$TRANSACTIONS reports state=1 while the
// query executes. Without "MON$TRANSACTION_ID <> CURRENT_TRANSACTION", a
// fresh probe (no other writers active) would return its own TX_ID and the
// listener's watermark would never advance — the probe loop would deadlock
// itself.
//
// Wrapped in RunInReadTx so the probe tx commits cleanly instead of leaking
// as an idle uncommitted tx (nakagami/firebirdsql v0.9.18 driver does not
// commit implicit txs from bare QueryContext on *sql.DB; see ADR-0007 and
// the platform/firebird package comment).
//
// Cross-connection invariant: if connection A holds open transaction T_A
// (other than ours), MinActiveTransactionID called on any connection B
// returns a value <= T_A. This is the watermark guarantee the listener
// relies on.
//
// Note: this query targets MON$TRANSACTIONS, a monitoring table. On Firebird
// 2.5+ it requires no special privilege when the caller is SYSDBA or has
// SELECT on the table. The dev DB satisfies this.
func MinActiveTransactionID(ctx context.Context, pool *firebird.Pool) (int64, error) {
	var minTx *int64
	err := firebird.RunInReadTx(ctx, pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, pool.DB)
		return q.QueryRowContext(ctx, `
SELECT MIN(MON$TRANSACTION_ID)
FROM MON$TRANSACTIONS
WHERE MON$STATE = 1
  AND MON$TRANSACTION_ID <> CURRENT_TRANSACTION`,
		).Scan(&minTx)
	})
	if err != nil {
		return 0, firebird.MapError(err)
	}
	if minTx == nil {
		// No hay transacciones activas (excepto la nuestra): todo está committed.
		// El sentinel garantiza que TX_ID < watermark sea verdad para cualquier fila.
		return SentinelNoActiveTx, nil
	}
	return *minTx, nil
}
