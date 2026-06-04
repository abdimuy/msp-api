//nolint:misspell // Spanish domain vocabulary by project convention.
package ventfb

import (
	"context"
	"database/sql"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertions: both concrete repos satisfy the reconcile interfaces.
var (
	_ outbound.PagosReconcileRepo  = (*PagosRepo)(nil)
	_ outbound.SaldosReconcileRepo = (*SaldosRepo)(nil)
)

// ─── PagosRepo — Digest + ListIDs ─────────────────────────────────────────────

// Digest streams pago PKs and UPDATED_AT under a snapshot transaction and
// computes (count, xor, sum, max_updated_at) in Go. The query mirrors the
// /sync filter: CONCEPTO_CC_ID IN (87327, 27969) and either s.SALDO > 0
// (desde zero) or s.SALDO > 0 OR p.FECHA >= desde (desde set). This ensures
// the digest is always comparable to what /sync would deliver.
//
// The firebirdsql driver does not provide a native XOR aggregate so we stream
// the rows — at 50k rows that is roughly 800 kB over loopback, well within
// acceptable bounds.
//
// firebird.RunInSnapshotTx wraps the read in a REPEATABLE READ
// (isc_tpb_concurrency) transaction explicitly committed at the end. This
// guarantees a single point-in-time view across the streamed rows and avoids
// the implicit-transaction leak in the nakagami/firebirdsql driver — the tx
// transitions cleanly to MON\$STATE=3 (committed) instead of lingering as
// idle/uncommitted.
func (r *PagosRepo) Digest(ctx context.Context, zonaID int, desde time.Time) (outbound.DigestResult, error) {
	var result outbound.DigestResult
	err := firebird.RunInSnapshotTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		saldoFilter, extraArgs := pagoDigestSaldoFilter(desde)
		args := append([]any{zonaID}, extraArgs...)
		//nolint:gosec // SQL built from package-level consts; saldoFilter is a fixed literal, not user input.
		query := `
SELECT p.IMPTE_DOCTO_CC_ID, p.UPDATED_AT
` + pagoFromClause + `
WHERE p.ZONA_CLIENTE_ID = ?
  AND p.CANCELADO = 'N'
  AND ` + pagoConceptoFilter + `
  AND ` + saldoFilter

		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()

		d, derr := computeDigest(ctx, rows)
		if derr != nil {
			return derr
		}
		result = d
		return nil
	})
	return result, err
}

// ListIDs returns pago IDs for zonaID with IMPTE_DOCTO_CC_ID > after, ordered
// ascending. The filter is identical to Digest so the pageable ID set always
// matches the digest count. Fetches limit+1 rows to detect has_more without
// an extra count query.
func (r *PagosRepo) ListIDs(ctx context.Context, zonaID, after, limit int, desde time.Time) ([]int, bool, error) {
	var ids []int
	var hasMore bool
	err := firebird.RunInSnapshotTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		saldoFilter, extraArgs := pagoDigestSaldoFilter(desde)
		args := append([]any{limit + 1, zonaID, after}, extraArgs...)
		//nolint:gosec // SQL built from package-level consts; saldoFilter is a fixed literal, not user input.
		query := `
SELECT FIRST ? p.IMPTE_DOCTO_CC_ID
` + pagoFromClause + `
WHERE p.ZONA_CLIENTE_ID = ?
  AND p.CANCELADO = 'N'
  AND p.IMPTE_DOCTO_CC_ID > ?
  AND ` + pagoConceptoFilter + `
  AND ` + saldoFilter + `
ORDER BY p.IMPTE_DOCTO_CC_ID ASC`

		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()

		scanned, more, serr := scanIDsWithHasMore(rows, limit)
		if serr != nil {
			return serr
		}
		ids = scanned
		hasMore = more
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return ids, hasMore, nil
}

// pagoDigestSaldoFilter returns the saldo WHERE fragment and any extra bind
// args for the digest/ids queries. When desde is zero it mirrors the legacy
// sync filter (s.SALDO > 0 only). When desde is set it also includes pagos
// whose p.FECHA >= desde — exactly the condition used by queryPagoSyncPage.
func pagoDigestSaldoFilter(desde time.Time) (string, []any) {
	if desde.IsZero() {
		return `s.SALDO > 0`, nil
	}
	return `(s.SALDO > 0 OR p.FECHA >= ?)`, []any{firebird.ToWallClock(desde)}
}

// ─── SaldosRepo — Digest + ListIDs ────────────────────────────────────────────

// Digest streams saldo PKs and UPDATED_AT under a snapshot transaction and
// computes the digest in Go. The filter mirrors /sync: when desde is zero only
// SALDO > 0 rows are included; when desde is set, rows with SALDO <= 0 AND
// FECHA_ULT_PAGO >= desde are also included. Same approach as PagosRepo.Digest.
func (r *SaldosRepo) Digest(ctx context.Context, zonaID int, desde time.Time) (outbound.DigestResult, error) {
	var result outbound.DigestResult
	err := firebird.RunInSnapshotTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		saldoFilter, extraArgs := saldoDigestSaldoFilter(desde)
		args := append([]any{zonaID}, extraArgs...)
		//nolint:gosec // SQL built from a fixed two-branch literal; no user input in saldoFilter.
		query := `
SELECT DOCTO_CC_ID, UPDATED_AT
FROM MSP_SALDOS_VENTAS
WHERE ZONA_CLIENTE_ID = ?
  AND CARGO_CANCELADO = 'N'
  AND ` + saldoFilter

		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()

		d, derr := computeDigest(ctx, rows)
		if derr != nil {
			return derr
		}
		result = d
		return nil
	})
	return result, err
}

// ListIDs returns saldo IDs for zonaID with DOCTO_CC_ID > after, ordered
// ascending. The filter is identical to Digest so the pageable ID set always
// matches the digest count.
func (r *SaldosRepo) ListIDs(ctx context.Context, zonaID, after, limit int, desde time.Time) ([]int, bool, error) {
	var ids []int
	var hasMore bool
	err := firebird.RunInSnapshotTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		saldoFilter, extraArgs := saldoDigestSaldoFilter(desde)
		args := append([]any{limit + 1, zonaID, after}, extraArgs...)
		//nolint:gosec // SQL built from a fixed two-branch literal; no user input in saldoFilter.
		query := `
SELECT FIRST ? DOCTO_CC_ID
FROM MSP_SALDOS_VENTAS
WHERE ZONA_CLIENTE_ID = ?
  AND CARGO_CANCELADO = 'N'
  AND DOCTO_CC_ID > ?
  AND ` + saldoFilter + `
ORDER BY DOCTO_CC_ID ASC`

		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()

		scanned, more, serr := scanIDsWithHasMore(rows, limit)
		if serr != nil {
			return serr
		}
		ids = scanned
		hasMore = more
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return ids, hasMore, nil
}

// saldoDigestSaldoFilter returns the saldo WHERE fragment and any extra bind
// args for the saldos digest/ids queries. When desde is zero it mirrors the
// legacy sync filter (SALDO > 0 only). When desde is set it also includes
// recently-paid saldos (SALDO <= 0 AND FECHA_ULT_PAGO >= desde).
func saldoDigestSaldoFilter(desde time.Time) (string, []any) {
	if desde.IsZero() {
		return `SALDO > 0`, nil
	}
	return `(SALDO > 0 OR (SALDO <= 0 AND FECHA_ULT_PAGO >= ?))`, []any{firebird.ToWallClock(desde)}
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
