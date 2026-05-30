package ventfb

import (
	"context"

	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertion: PagosLister satisfies the outbound port.
var _ outbound.PagosLister = (*PagosLister)(nil)

// PagosLister implements outbound.PagosLister using a cursor-paginated SELECT
// over MSP_PAGOS_VENTAS ordered by IMPTE_DOCTO_CC_ID ascending.
type PagosLister struct {
	pool *firebird.Pool
}

// NewPagosLister builds a PagosLister wired to the given pool.
func NewPagosLister(pool *firebird.Pool) *PagosLister {
	return &PagosLister{pool: pool}
}

// Page returns up to limit IMPTE_DOCTO_CC_IDs from MSP_PAGOS_VENTAS starting
// AFTER cursorAfter. Pass 0 to start from the beginning.
func (l *PagosLister) Page(ctx context.Context, cursorAfter, limit int) ([]int, int, error) {
	return listIDPage(ctx, l.pool, `
SELECT FIRST ? IMPTE_DOCTO_CC_ID
FROM MSP_PAGOS_VENTAS
WHERE IMPTE_DOCTO_CC_ID > ?
ORDER BY IMPTE_DOCTO_CC_ID`, cursorAfter, limit)
}
