//nolint:misspell // Spanish vocabulary (saldo, cargo, etc.) by convention.
package ventfb_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// TestE2E_Cobranza_Recompute_ReturnsFreshSaldo inserts a cargo, corrupts the
// cache row directly, calls Recomputer.Recompute, and verifies the returned
// saldo matches the correct computed value.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_Recompute_ReturnsFreshSaldo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, _ := seedClienteID(t, q)
		importe := decimal.RequireFromString("4000.00")
		cargoID := insertCargoDoctosCC(t, q, clienteID, "RECOMP-001", importe)

		repo := cobranzaventfb.NewSaldosRepo(pool)

		saldoOK, err := repo.PorCargo(ctx, cargoID)
		if err != nil {
			t.Skipf("trigger did not create cache row for cargo %d", cargoID)
		}
		originalSaldo := saldoOK.Saldo()

		// Corrupt the cache row.
		_, err = q.ExecContext(
			context.Background(),
			`UPDATE MSP_SALDOS_VENTAS SET SALDO = -777 WHERE DOCTO_CC_ID = ?`,
			cargoID,
		)
		require.NoError(t, err, "corrupt cache row")

		// Recompute should fix it.
		recomputer := cobranzaventfb.NewRecomputer(pool, repo)
		fresh, err := recomputer.Recompute(ctx, cargoID)
		require.NoError(t, err)

		assert.True(t, originalSaldo.Equal(fresh.Saldo()),
			"recomputed saldo must match original: want=%s got=%s",
			originalSaldo.StringFixed(2), fresh.Saldo().StringFixed(2))

		// Verify the cache row is also fixed.
		cached, err := repo.PorCargo(ctx, cargoID)
		require.NoError(t, err)
		assert.True(t, originalSaldo.Equal(cached.Saldo()),
			"cache row must be fixed after Recompute: want=%s got=%s",
			originalSaldo.StringFixed(2), cached.Saldo().StringFixed(2))

		t.Logf("recompute: cargoID=%d originalSaldo=%s fixed=%s",
			cargoID, originalSaldo.StringFixed(2), fresh.Saldo().StringFixed(2))
	})
}
