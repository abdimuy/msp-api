package ventfb

import (
	"context"

	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertion: SaldosLister satisfies the outbound port.
var _ outbound.SaldosLister = (*SaldosLister)(nil)

// SaldosLister implements outbound.SaldosLister using a cursor-paginated SELECT
// over MSP_SALDOS_VENTAS ordered by DOCTO_CC_ID ascending.
type SaldosLister struct {
	pool *firebird.Pool
}

// NewSaldosLister builds a SaldosLister wired to the given pool.
func NewSaldosLister(pool *firebird.Pool) *SaldosLister {
	return &SaldosLister{pool: pool}
}

// Page returns up to limit cargo IDs from MSP_SALDOS_VENTAS starting AFTER
// cursorAfter. Pass 0 to start from the beginning.
// nextCursor is 0 when fewer than limit rows remain (end of table).
func (l *SaldosLister) Page(ctx context.Context, cursorAfter, limit int) ([]int, int, error) {
	q := firebird.GetQuerier(ctx, l.pool.DB)
	rows, err := q.QueryContext(
		ctx, `
SELECT FIRST ? DOCTO_CC_ID
FROM MSP_SALDOS_VENTAS
WHERE DOCTO_CC_ID > ?
ORDER BY DOCTO_CC_ID`,
		limit, cursorAfter,
	)
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
		// Fewer than limit rows means we reached the end.
		return ids, 0, nil
	}

	// The next call should start after the last seen ID.
	return ids, ids[len(ids)-1], nil
}
