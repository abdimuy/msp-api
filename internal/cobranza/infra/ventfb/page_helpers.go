package ventfb

import (
	"context"
	"database/sql"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// syncClockSkewSeconds is the margin subtracted from CURRENT_TIMESTAMP when
// computing the upper bound of a sync query. It covers only clock skew between
// the Firebird server and the device; correctness for in-flight transactions is
// now provided by the TX_ID < watermark predicate (commit 7), not by a wide
// time window. 1 s is sufficient for typical server/device clock drift.
//
// Previous value (syncLagSeconds = 5) also had to cover in-flight tx by relying
// on timing heuristics — that approach caused permanent lost-updates when a
// Microsip GUI transaction took longer than 5 s to commit.
const syncClockSkewSeconds = 1

// listIDPage runs a cursor-paginated SELECT that returns one INTEGER column
// (a PK) per row. queryTmpl must select that column with `FIRST ?` plus a
// `WHERE pk > ?` clause; the helper passes (limit, cursorAfter) as params.
//
// nextCursor is 0 when fewer than limit rows remain (caller knows the table
// is exhausted); otherwise it is the last seen PK.
func listIDPage(ctx context.Context, pool *firebird.Pool, queryTmpl string, cursorAfter, limit int) ([]int, int, error) {
	q := firebird.GetQuerier(ctx, pool.DB)
	rows, err := q.QueryContext(ctx, queryTmpl, limit, cursorAfter)
	if err != nil {
		return nil, 0, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var ids []int
	for rows.Next() {
		var id int
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, 0, firebird.MapError(scanErr)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, firebird.MapError(err)
	}

	if len(ids) < limit {
		return ids, 0, nil
	}
	return ids, ids[len(ids)-1], nil
}

// syncPageSpec is the table-shaped input for querySyncPage. The same SELECT /
// WHERE / ORDER BY layout works for any sync-eligible MSP_* table; only the
// SELECT-list, table name and PK column change.
type syncPageSpec struct {
	columns    string    // comma-separated SELECT list
	table      string    // table name (no FROM clause)
	pkColumn   string    // primary key column for stable ordering and tie-break
	zonaID     int       // zone filter
	cursor     time.Time // > cursor when non-zero; ignored when zero
	upperBound time.Time // UPDATED_AT <= upperBound (clock skew margin)
	// watermark filters out rows written by in-flight transactions.
	// Only rows with TX_ID < watermark are returned. Pass SentinelNoActiveTx
	// (math.MaxInt64) to disable watermark filtering (rows with any TX_ID
	// pass the predicate). Tables that do not have a TX_ID column must use
	// SentinelNoActiveTx; the predicate is then always true and does not
	// reference the column.
	//
	// IMPORTANT: use strict less-than (TX_ID < watermark), NOT <=. The
	// watermark is the smallest active TX_ID; a row written by a still-active
	// tx has TX_ID == watermark, so <= would include uncommitted data.
	// Off-by-one here = money lost (mutation test: flip < to <= → test fails).
	watermark int64
	afterID   int // pagination tie-breaker for rows sharing the same UPDATED_AT
	limit     int // FIRST :limit
}

// querySyncPage builds and executes the canonical sync page query. Initial
// sync (cursor zero) skips the lower-bound predicate and tie-break clause;
// otherwise the WHERE clause uses (UPDATED_AT > cursor) with a (=, pk >)
// tie-break to make pagination deterministic when multiple rows share the
// same UPDATED_AT.
//
// When spec.watermark < SentinelNoActiveTx the query adds `AND TX_ID < ?`
// to exclude rows written by in-flight transactions (gap 2 fix). The table
// must have a TX_ID column for this to work; pass SentinelNoActiveTx to
// bypass the predicate for tables without TX_ID.
func querySyncPage(ctx context.Context, q firebird.Querier, spec syncPageSpec) (*sql.Rows, error) {
	upper := firebird.ToWallClock(spec.upperBound)
	if spec.cursor.IsZero() {
		// Initial sync — no lower bound on UPDATED_AT.
		query := `
SELECT FIRST ? ` + spec.columns + `
FROM ` + spec.table + `
WHERE ZONA_CLIENTE_ID = ?
  AND UPDATED_AT <= ?
  AND TX_ID < ?
  AND ` + spec.pkColumn + ` > ?
ORDER BY UPDATED_AT, ` + spec.pkColumn
		rows, err := q.QueryContext(ctx, query, spec.limit, spec.zonaID, upper, spec.watermark, spec.afterID)
		if err != nil {
			return nil, firebird.MapError(err)
		}
		return rows, nil
	}
	cur := firebird.ToWallClock(spec.cursor)
	// UPDATED_AT >= cursor (no estricto): habilita el tie-break por pk para
	// el caso comun en backfills donde miles de rows comparten el mismo
	// UPDATED_AT. Sin esto, una primera pagina llena con max=T1 hace que la
	// segunda llamada con cursor=T1 no encuentre ninguno (UPDATED_AT > T1
	// excluiria todos los rows con UPDATED_AT == T1 que no entraron).
	query := `
SELECT FIRST ? ` + spec.columns + `
FROM ` + spec.table + `
WHERE ZONA_CLIENTE_ID = ?
  AND UPDATED_AT >= ?
  AND UPDATED_AT <= ?
  AND TX_ID < ?
  AND (UPDATED_AT > ? OR (UPDATED_AT = ? AND ` + spec.pkColumn + ` > ?))
ORDER BY UPDATED_AT, ` + spec.pkColumn
	rows, err := q.QueryContext(ctx, query, spec.limit, spec.zonaID, cur, upper, spec.watermark, cur, cur, spec.afterID)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	return rows, nil
}

// itemWithUpdatedAt is the subset of a domain entity that buildSyncPage needs:
// just the UPDATED_AT timestamp. Saldo and Pago both satisfy this.
type itemWithUpdatedAt interface {
	UpdatedAt() time.Time
}

// runSyncPage executes a sync page query and packages the result. It is
// generic over the item type so saldos and pagos can share the orchestration
// (compute watermark/server_now, scan rows, set MaxUpdatedAt / HasMore).
// pageQuery is the table-specific query builder; it receives upper and
// watermark from serverNowAndWatermark so each table can decide how to use
// them. rowScanner is the table-specific scanner.
//
// Both queries follow the same shape:
//   - WHERE zona = ? AND UPDATED_AT > cursor AND UPDATED_AT <= upper
//     AND TX_ID < watermark
//     AND (UPDATED_AT > cursor OR (UPDATED_AT = cursor AND pk > afterID))
//   - ORDER BY UPDATED_AT, pk
//   - FIRST limit
//
// Only the SELECT list and table change, so the type parameter is enough to
// keep the abstraction clean.
func runSyncPage[T itemWithUpdatedAt](
	ctx context.Context,
	pool *firebird.Pool,
	cursor time.Time,
	limit int,
	pageQuery func(ctx context.Context, q firebird.Querier, upper time.Time, watermark int64) (*sql.Rows, error),
	rowScanner func(rows *sql.Rows) ([]T, error),
) (outbound.SyncPage[T], error) {
	q := firebird.GetQuerier(ctx, pool.DB)

	sn, err := serverNowAndWatermark(ctx, q, pool)
	if err != nil {
		return outbound.SyncPage[T]{}, err
	}

	rows, err := pageQuery(ctx, q, sn.upperBound, sn.watermark)
	if err != nil {
		return outbound.SyncPage[T]{}, err
	}
	defer func() { _ = rows.Close() }()

	items, err := rowScanner(rows)
	if err != nil {
		return outbound.SyncPage[T]{}, err
	}
	if err := rows.Err(); err != nil {
		return outbound.SyncPage[T]{}, firebird.MapError(err)
	}

	page := outbound.SyncPage[T]{
		Items:        items,
		MaxUpdatedAt: cursor.UTC(),
		ServerNow:    sn.serverNow,
		HasMore:      len(items) == limit,
	}
	if len(items) > 0 {
		page.MaxUpdatedAt = items[len(items)-1].UpdatedAt()
	}
	return page, nil
}

// syncNow holds the computed values from a serverNowAndWatermark call.
type syncNow struct {
	// upperBound is server_now - syncClockSkewSeconds; used as the UPDATED_AT
	// upper bound in sync queries to absorb device/server clock skew.
	upperBound time.Time
	// serverNow is the raw CURRENT_TIMESTAMP from Firebird; returned to the
	// client in the SyncPage response so the device can align its clock.
	serverNow time.Time
	// watermark is the MinActiveTransactionID; only rows with TX_ID < watermark
	// are included in the sync response (structurally excludes in-flight tx).
	watermark int64
}

// serverNowAndWatermark queries the Firebird server for CURRENT_TIMESTAMP and
// MinActiveTransactionID, returning the pair needed to build a safe sync page:
//   - upperBound = server_now − syncClockSkewSeconds (absorbs clock skew)
//   - watermark = smallest active TX_ID (excludes in-flight long transactions)
//
// Replaces the old serverNowAndLag (syncLagSeconds = 5 s). The 5-second window
// previously had to absorb both clock skew AND in-flight long transactions from
// the Microsip GUI (gap 2). With the watermark predicate, only clock skew
// remains — 1 s is sufficient.
func serverNowAndWatermark(ctx context.Context, q firebird.Querier, pool *firebird.Pool) (syncNow, error) {
	var raw any
	if err := q.QueryRowContext(ctx, `SELECT CURRENT_TIMESTAMP FROM RDB$DATABASE`).Scan(&raw); err != nil {
		return syncNow{}, firebird.MapError(err)
	}
	now, err := firebird.ScanUTCTime(raw)
	if err != nil {
		return syncNow{}, err
	}
	wm, err := MinActiveTransactionID(ctx, pool)
	if err != nil {
		return syncNow{}, err
	}
	return syncNow{
		upperBound: now.Add(-syncClockSkewSeconds * time.Second),
		serverNow:  now,
		watermark:  wm,
	}, nil
}
