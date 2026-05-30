package ventfb

import (
	"context"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertion: Recomputer satisfies the outbound port.
var _ outbound.SaldosRecomputer = (*Recomputer)(nil)

// Recomputer implements outbound.SaldosRecomputer by invoking the Firebird
// stored procedure MSP_RECOMPUTE_SALDO_VENTA and then re-reading the refreshed
// cache row via the SaldosRepo.
type Recomputer struct {
	pool *firebird.Pool
	repo outbound.SaldosRepo
}

// NewRecomputer builds a Recomputer wired to the given pool and repo.
// The repo is used for the re-read step after the stored procedure runs.
func NewRecomputer(pool *firebird.Pool, repo outbound.SaldosRepo) *Recomputer {
	return &Recomputer{pool: pool, repo: repo}
}

// Recompute calls EXECUTE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA(:cargoCCID)
// then re-reads the refreshed row from MSP_SALDOS_VENTAS.
func (r *Recomputer) Recompute(ctx context.Context, cargoCCID int) (*domain.Saldo, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	_, err := q.ExecContext(ctx, "EXECUTE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA(?)", cargoCCID)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	return r.repo.PorCargo(ctx, cargoCCID)
}
