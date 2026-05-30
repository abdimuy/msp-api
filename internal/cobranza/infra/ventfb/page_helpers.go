package ventfb

import (
	"context"
	"database/sql"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// syncLagSeconds is the lag window subtracted from CURRENT_TIMESTAMP when
// computing the upper bound of a sync query. Tx committed within this window
// are excluded so the next sync round catches them — avoids losing rows whose
// COMMIT lands between the query's snapshot and the response.
const syncLagSeconds = 5

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
	upperBound time.Time // UPDATED_AT <= upperBound (lag window)
	afterID    int       // pagination tie-breaker for rows sharing the same UPDATED_AT
	limit      int       // FIRST :limit
}

// querySyncPage builds and executes the canonical sync page query. Initial
// sync (cursor zero) skips the lower-bound predicate and tie-break clause;
// otherwise the WHERE clause uses (UPDATED_AT > cursor) with a (=, pk >)
// tie-break to make pagination deterministic when multiple rows share the
// same UPDATED_AT.
func querySyncPage(ctx context.Context, q firebird.Querier, spec syncPageSpec) (*sql.Rows, error) {
	upper := firebird.ToWallClock(spec.upperBound)
	if spec.cursor.IsZero() {
		// Initial sync — no lower bound on UPDATED_AT.
		query := `
SELECT FIRST ? ` + spec.columns + `
FROM ` + spec.table + `
WHERE ZONA_CLIENTE_ID = ?
  AND UPDATED_AT <= ?
  AND ` + spec.pkColumn + ` > ?
ORDER BY UPDATED_AT, ` + spec.pkColumn
		rows, err := q.QueryContext(ctx, query, spec.limit, spec.zonaID, upper, spec.afterID)
		if err != nil {
			return nil, firebird.MapError(err)
		}
		return rows, nil
	}
	cur := firebird.ToWallClock(spec.cursor)
	query := `
SELECT FIRST ? ` + spec.columns + `
FROM ` + spec.table + `
WHERE ZONA_CLIENTE_ID = ?
  AND UPDATED_AT > ?
  AND UPDATED_AT <= ?
  AND (UPDATED_AT > ? OR (UPDATED_AT = ? AND ` + spec.pkColumn + ` > ?))
ORDER BY UPDATED_AT, ` + spec.pkColumn
	rows, err := q.QueryContext(ctx, query, spec.limit, spec.zonaID, cur, upper, cur, cur, spec.afterID)
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
// (compute lag/server_now, scan rows, set MaxUpdatedAt / HasMore). pageQuery
// is the table-specific query builder; rowScanner is the table-specific
// scanner.
//
// Both queries follow the same shape:
//   - WHERE zona = ? AND UPDATED_AT > cursor AND UPDATED_AT <= upper
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
	pageQuery func(ctx context.Context, q firebird.Querier, upper time.Time) (*sql.Rows, error),
	rowScanner func(rows *sql.Rows) ([]T, error),
) (outbound.SyncPage[T], error) {
	q := firebird.GetQuerier(ctx, pool.DB)

	upperBound, serverNow, err := serverNowAndLag(ctx, q)
	if err != nil {
		return outbound.SyncPage[T]{}, err
	}

	rows, err := pageQuery(ctx, q, upperBound)
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
		ServerNow:    serverNow,
		HasMore:      len(items) == limit,
	}
	if len(items) > 0 {
		page.MaxUpdatedAt = items[len(items)-1].UpdatedAt()
	}
	return page, nil
}

// serverNowAndLag returns the timestamp the sync query should use as upper
// bound (server_now - lag) and the raw server_now (for the response).
func serverNowAndLag(ctx context.Context, q firebird.Querier) (time.Time, time.Time, error) {
	var raw any
	if err := q.QueryRowContext(ctx, `SELECT CURRENT_TIMESTAMP FROM RDB$DATABASE`).Scan(&raw); err != nil {
		return time.Time{}, time.Time{}, firebird.MapError(err)
	}
	now, err := firebird.ScanUTCTime(raw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return now.Add(-syncLagSeconds * time.Second), now, nil
}
