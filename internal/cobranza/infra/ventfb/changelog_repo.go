package ventfb

import (
	"context"
	"database/sql"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertions: both concrete repos must satisfy their ports.
var (
	_ outbound.PagosChangelogRepo  = (*PagosChangelogRepo)(nil)
	_ outbound.SaldosChangelogRepo = (*SaldosChangelogRepo)(nil)
)

// ─── shared scan helper ───────────────────────────────────────────────────────

// scanChangelogRows scans an open *sql.Rows from either MSP_PAGOS_CHANGELOG or
// MSP_SALDOS_CHANGELOG. Column order must be SEQ_ID, PK, TX_ID, COMMIT_AT.
func scanChangelogRows(rows *sql.Rows) ([]outbound.ChangelogEntry, error) {
	defer func() { _ = rows.Close() }()

	var result []outbound.ChangelogEntry
	for rows.Next() {
		var (
			e         outbound.ChangelogEntry
			commitRaw any
		)
		if scanErr := rows.Scan(&e.SeqID, &e.PK, &e.TxID, &commitRaw); scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		t, parseErr := firebird.ScanUTCTime(commitRaw)
		if parseErr != nil {
			return nil, parseErr
		}
		e.CommitAt = t
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return result, nil
}

// ─── PagosChangelogRepo ───────────────────────────────────────────────────────

// PagosChangelogRepo implements outbound.PagosChangelogRepo backed by
// MSP_PAGOS_CHANGELOG.
type PagosChangelogRepo struct {
	pool *firebird.Pool
}

// NewPagosChangelogRepo builds a PagosChangelogRepo wired to pool.
func NewPagosChangelogRepo(pool *firebird.Pool) *PagosChangelogRepo {
	return &PagosChangelogRepo{pool: pool}
}

// Since returns up to limit rows de MSP_PAGOS_CHANGELOG donde SEQ_ID > sinceSeq
// y TX_ID < watermark, ordenados por SEQ_ID ascendente.
func (r *PagosChangelogRepo) Since(ctx context.Context, sinceSeq, watermark int64, limit int) ([]outbound.ChangelogEntry, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, `
SELECT FIRST ? SEQ_ID, IMPTE_DOCTO_CC_ID, TX_ID, COMMIT_AT
  FROM MSP_PAGOS_CHANGELOG
 WHERE SEQ_ID > ? AND TX_ID < ?
 ORDER BY SEQ_ID ASC`,
		limit, sinceSeq, watermark)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	return scanChangelogRows(rows)
}

// DeleteOlderThan elimina hasta maxDelete filas de MSP_PAGOS_CHANGELOG cuyo
// COMMIT_AT < cutoff. Usa un sub-SELECT con FIRST para limitar el scan y
// evitar escalación de locks en Firebird 2.x / 3.x.
func (r *PagosChangelogRepo) DeleteOlderThan(ctx context.Context, cutoff time.Time, maxDelete int) (int, error) {
	// Firebird ROWS/FIRST en DELETE no es portable en 2.x. El patrón
	// DELETE ... WHERE pk IN (SELECT FIRST ? pk ...) sí lo es.
	q := firebird.GetQuerier(ctx, r.pool.DB)
	result, err := q.ExecContext(ctx, `
DELETE FROM MSP_PAGOS_CHANGELOG
 WHERE SEQ_ID IN (
   SELECT FIRST ? SEQ_ID
     FROM MSP_PAGOS_CHANGELOG
    WHERE COMMIT_AT < ?
    ORDER BY SEQ_ID ASC
 )`,
		maxDelete, firebird.ToWallClock(cutoff))
	if err != nil {
		return 0, firebird.MapError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, firebird.MapError(err)
	}
	return int(n), nil
}

// MaxSeqID retorna el mayor SEQ_ID visible bajo el watermark en
// MSP_PAGOS_CHANGELOG, o 0 cuando la tabla está vacía o todas las filas
// están por encima del watermark.
func (r *PagosChangelogRepo) MaxSeqID(ctx context.Context, watermark int64) (int64, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var maxSeq *int64
	err := q.QueryRowContext(ctx, `
SELECT MAX(SEQ_ID) FROM MSP_PAGOS_CHANGELOG WHERE TX_ID < ?`,
		watermark).Scan(&maxSeq)
	if err != nil {
		return 0, firebird.MapError(err)
	}
	if maxSeq == nil {
		return 0, nil
	}
	return *maxSeq, nil
}

// ─── SaldosChangelogRepo ──────────────────────────────────────────────────────

// SaldosChangelogRepo implements outbound.SaldosChangelogRepo backed by
// MSP_SALDOS_CHANGELOG.
type SaldosChangelogRepo struct {
	pool *firebird.Pool
}

// NewSaldosChangelogRepo builds a SaldosChangelogRepo wired to pool.
func NewSaldosChangelogRepo(pool *firebird.Pool) *SaldosChangelogRepo {
	return &SaldosChangelogRepo{pool: pool}
}

// Since returns up to limit rows de MSP_SALDOS_CHANGELOG donde SEQ_ID > sinceSeq
// y TX_ID < watermark, ordenados por SEQ_ID ascendente.
func (r *SaldosChangelogRepo) Since(ctx context.Context, sinceSeq, watermark int64, limit int) ([]outbound.ChangelogEntry, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, `
SELECT FIRST ? SEQ_ID, DOCTO_CC_ID, TX_ID, COMMIT_AT
  FROM MSP_SALDOS_CHANGELOG
 WHERE SEQ_ID > ? AND TX_ID < ?
 ORDER BY SEQ_ID ASC`,
		limit, sinceSeq, watermark)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	return scanChangelogRows(rows)
}

// DeleteOlderThan elimina hasta maxDelete filas de MSP_SALDOS_CHANGELOG cuyo
// COMMIT_AT < cutoff.
func (r *SaldosChangelogRepo) DeleteOlderThan(ctx context.Context, cutoff time.Time, maxDelete int) (int, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	result, err := q.ExecContext(ctx, `
DELETE FROM MSP_SALDOS_CHANGELOG
 WHERE SEQ_ID IN (
   SELECT FIRST ? SEQ_ID
     FROM MSP_SALDOS_CHANGELOG
    WHERE COMMIT_AT < ?
    ORDER BY SEQ_ID ASC
 )`,
		maxDelete, firebird.ToWallClock(cutoff))
	if err != nil {
		return 0, firebird.MapError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, firebird.MapError(err)
	}
	return int(n), nil
}

// MaxSeqID retorna el mayor SEQ_ID visible bajo el watermark en
// MSP_SALDOS_CHANGELOG, o 0 cuando la tabla está vacía o todas las filas
// están por encima del watermark.
func (r *SaldosChangelogRepo) MaxSeqID(ctx context.Context, watermark int64) (int64, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var maxSeq *int64
	err := q.QueryRowContext(ctx, `
SELECT MAX(SEQ_ID) FROM MSP_SALDOS_CHANGELOG WHERE TX_ID < ?`,
		watermark).Scan(&maxSeq)
	if err != nil {
		return 0, firebird.MapError(err)
	}
	if maxSeq == nil {
		return 0, nil
	}
	return *maxSeq, nil
}
