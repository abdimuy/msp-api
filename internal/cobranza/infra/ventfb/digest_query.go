//nolint:misspell // Spanish domain vocabulary by project convention.
package ventfb

import (
	"context"
	"database/sql"

	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertions: both concrete repos satisfy the reconcile interfaces.
var (
	_ outbound.PagosReconcileRepo  = (*PagosRepo)(nil)
	_ outbound.SaldosReconcileRepo = (*SaldosRepo)(nil)
)

// snapshotTx begins a Firebird snapshot-isolation tx. The firebirdsql driver
// maps sql.LevelRepeatableRead to isc_tpb_concurrency (Firebird snapshot, no
// blocking of writers). The driver does not expose a read-only TPB option via
// database/sql, so the tx is opened with isc_tpb_write even though Digest /
// ListIDs only read. This is safe: we never write within these queries.
// The returned tx must be rolled back or committed by the caller.
func snapshotTx(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	return db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
}

// ─── PagosRepo — Digest + ListIDs ─────────────────────────────────────────────

// Digest streams active pago PKs and UPDATED_AT under a snapshot transaction
// and computes (count, xor, sum, max_updated_at) in Go. The firebirdsql driver
// does not provide a native XOR aggregate so we stream the rows — at 50k rows
// that is roughly 800 kB over loopback, well within acceptable bounds.
func (r *PagosRepo) Digest(ctx context.Context, zonaID int) (outbound.DigestResult, error) {
	tx, err := snapshotTx(ctx, r.pool.DB)
	if err != nil {
		return outbound.DigestResult{}, firebird.MapError(err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
SELECT IMPTE_DOCTO_CC_ID, UPDATED_AT
FROM MSP_PAGOS_VENTAS
WHERE ZONA_CLIENTE_ID = ?
  AND CANCELADO = 'N'`,
		zonaID)
	if err != nil {
		return outbound.DigestResult{}, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	result, err := computeDigest(ctx, rows)
	if err != nil {
		return outbound.DigestResult{}, err
	}
	return result, nil
}

// ListIDs returns active pago IDs for zonaID with IMPTE_DOCTO_CC_ID > after,
// ordered ascending. It fetches limit+1 rows to detect has_more without an
// extra count query.
func (r *PagosRepo) ListIDs(ctx context.Context, zonaID, after, limit int) ([]int, bool, error) {
	tx, err := snapshotTx(ctx, r.pool.DB)
	if err != nil {
		return nil, false, firebird.MapError(err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
SELECT FIRST ? IMPTE_DOCTO_CC_ID
FROM MSP_PAGOS_VENTAS
WHERE ZONA_CLIENTE_ID = ?
  AND CANCELADO = 'N'
  AND IMPTE_DOCTO_CC_ID > ?
ORDER BY IMPTE_DOCTO_CC_ID ASC`,
		limit+1, zonaID, after)
	if err != nil {
		return nil, false, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	return scanIDsWithHasMore(rows, limit)
}

// ─── SaldosRepo — Digest + ListIDs ────────────────────────────────────────────

// Digest streams active saldo PKs and UPDATED_AT under a snapshot transaction
// and computes the digest in Go. Same approach as PagosRepo.Digest.
func (r *SaldosRepo) Digest(ctx context.Context, zonaID int) (outbound.DigestResult, error) {
	tx, err := snapshotTx(ctx, r.pool.DB)
	if err != nil {
		return outbound.DigestResult{}, firebird.MapError(err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
SELECT DOCTO_CC_ID, UPDATED_AT
FROM MSP_SALDOS_VENTAS
WHERE ZONA_CLIENTE_ID = ?
  AND CARGO_CANCELADO = 'N'`,
		zonaID)
	if err != nil {
		return outbound.DigestResult{}, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	result, err := computeDigest(ctx, rows)
	if err != nil {
		return outbound.DigestResult{}, err
	}
	return result, nil
}

// ListIDs returns active saldo IDs for zonaID with DOCTO_CC_ID > after,
// ordered ascending.
func (r *SaldosRepo) ListIDs(ctx context.Context, zonaID, after, limit int) ([]int, bool, error) {
	tx, err := snapshotTx(ctx, r.pool.DB)
	if err != nil {
		return nil, false, firebird.MapError(err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
SELECT FIRST ? DOCTO_CC_ID
FROM MSP_SALDOS_VENTAS
WHERE ZONA_CLIENTE_ID = ?
  AND CARGO_CANCELADO = 'N'
  AND DOCTO_CC_ID > ?
ORDER BY DOCTO_CC_ID ASC`,
		limit+1, zonaID, after)
	if err != nil {
		return nil, false, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	return scanIDsWithHasMore(rows, limit)
}

// ─── computation helpers ──────────────────────────────────────────────────────

// computeDigest iterates rows of (INTEGER pk, TIMESTAMP updated_at) and
// computes count, XOR, sum, and max updated_at in Go. The XOR and sum use
// int64 arithmetic; wrap-around for sum is intentional and documented in
// DigestResult.IDsSum.
func computeDigest(_ context.Context, rows *sql.Rows) (outbound.DigestResult, error) {
	var (
		count     int
		xorAcc    int64
		sumAcc    int64
		maxRaw    any
		havingMax bool
	)

	for rows.Next() {
		var (
			pkRaw        int
			updatedAtRaw any
		)
		if err := rows.Scan(&pkRaw, &updatedAtRaw); err != nil {
			return outbound.DigestResult{}, firebird.MapError(err)
		}

		pk := int64(pkRaw)
		count++
		xorAcc ^= pk
		sumAcc += pk

		// Track max UPDATED_AT. The first row becomes the initial maximum;
		// subsequent rows replace it when they are strictly later.
		newMax, err := updateMax(maxRaw, updatedAtRaw, havingMax)
		if err != nil {
			return outbound.DigestResult{}, err
		}
		maxRaw = newMax
		havingMax = true
	}
	if err := rows.Err(); err != nil {
		return outbound.DigestResult{}, firebird.MapError(err)
	}

	res := outbound.DigestResult{
		CountActivos: count,
		IDsXor:       xorAcc,
		IDsSum:       sumAcc,
	}
	if havingMax {
		t, err := firebird.ScanUTCTime(maxRaw)
		if err != nil {
			return outbound.DigestResult{}, err
		}
		res.MaxUpdatedAt = t.UTC()
	}
	return res, nil
}

// updateMax returns the raw timestamp that represents the later of prev and
// candidate. When havingMax is false, candidate is returned unconditionally.
func updateMax(prev, candidate any, havingMax bool) (any, error) {
	if !havingMax {
		return candidate, nil
	}
	cur, err := firebird.ScanUTCTime(candidate)
	if err != nil {
		return nil, err
	}
	prevT, err := firebird.ScanUTCTime(prev)
	if err != nil {
		return nil, err
	}
	if cur.After(prevT) {
		return candidate, nil
	}
	return prev, nil
}

// scanIDsWithHasMore scans a single-column INTEGER result set and returns
// (ids, hasMore). It reads limit+1 rows: if exactly limit+1 rows came back,
// hasMore=true and the slice is trimmed to limit.
func scanIDsWithHasMore(rows *sql.Rows, limit int) ([]int, bool, error) {
	ids := make([]int, 0, limit+1)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, false, firebird.MapError(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, false, firebird.MapError(err)
	}
	if len(ids) > limit {
		return ids[:limit], true, nil
	}
	return ids, false, nil
}
