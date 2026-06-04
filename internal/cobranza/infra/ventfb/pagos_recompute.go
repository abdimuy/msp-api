package ventfb

import (
	"context"

	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertion: PagosRecomputer satisfies the outbound port.
var _ outbound.PagosRecomputer = (*PagosRecomputer)(nil)

// PagosRecomputer implements outbound.PagosRecomputer by invoking the Firebird
// stored procedure MSP_RECOMPUTE_PAGO.
type PagosRecomputer struct {
	pool *firebird.Pool
}

// NewPagosRecomputer builds a PagosRecomputer wired to the given pool.
func NewPagosRecomputer(pool *firebird.Pool) *PagosRecomputer {
	return &PagosRecomputer{pool: pool}
}

// Recompute calls EXECUTE PROCEDURE MSP_RECOMPUTE_PAGO(:impteID).
func (r *PagosRecomputer) Recompute(ctx context.Context, impteID int) error {
	return firebird.RunInTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		if _, err := q.ExecContext(ctx, "EXECUTE PROCEDURE MSP_RECOMPUTE_PAGO(?)", impteID); err != nil {
			return firebird.MapError(err)
		}
		return nil
	})
}
