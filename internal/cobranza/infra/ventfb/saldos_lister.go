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
func (l *SaldosLister) Page(ctx context.Context, cursorAfter, limit int) ([]int, int, error) {
	return listIDPage(ctx, l.pool, `
SELECT FIRST ? DOCTO_CC_ID
FROM MSP_SALDOS_VENTAS
WHERE DOCTO_CC_ID > ?
ORDER BY DOCTO_CC_ID`, cursorAfter, limit)
}
