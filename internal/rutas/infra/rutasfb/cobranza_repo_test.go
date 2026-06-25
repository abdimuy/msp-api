//nolint:misspell // rutas vocabulary is Spanish per project convention.
//nolint:paralleltest // serial: shares rollback-only tx.
package rutasfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
)

func TestCobranzaRepo_VentasPorZona(t *testing.T) { //nolint:paralleltest // serial: shares rollback-only tx.
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := rutasfb.NewCobranzaRepo(pool)
		// Use a known zona from MSP_CFG_ZONA_CAJA seed (12271).
		// Window: last 30 days to capture any real data.
		desde := time.Now().UTC().AddDate(0, 0, -30)
		hasta := time.Now().UTC()

		ventas, err := repo.VentasPorZona(ctx, 12271, desde, hasta)
		require.NoError(t, err)
		assert.NotNil(t, ventas)
		t.Logf("VentasPorZona(12271) returned %d ventas", len(ventas))
		for _, v := range ventas {
			assert.NotZero(t, v.VentaID)
			assert.NotZero(t, v.ClienteID)
			// Parcialidad may be 0 for unregistered credits — just check scan.
			assert.False(t, v.Saldo.IsNegative(), "saldo must not be negative")
			// Cash sales (de contado) must never appear in the cobranza set;
			// they are not credit collection and would inflate % ponderado.
			assert.False(t, v.Frecuencia.EsContado(),
				"venta %d de contado no debe aparecer en cobranza", v.VentaID)
		}
	})
}
