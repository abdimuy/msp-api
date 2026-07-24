// Integration tests for outbound.CategoriasClienteReader (reads the Microsip
// venta history: DOCTOS_PV → DOCTOS_PV_DET → ARTICULOS). Read-only; the query
// runs inside a rolled-back transaction so the shared dev DB is untouched.
//
// Prerequisites:
//   - FB_DATABASE env var pointing at the dev Microsip Firebird DB.
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/reactivacion/infra/reactivacionfb/...
//
//nolint:misspell // Spanish vocabulary (categorias, cliente) by convention.
package reactivacionfb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionfb"
)

//nolint:paralleltest // serial: shares rollback-only tx.
func TestCategoriasRepo_UnknownClienteReturnsEmpty(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		// A synthetic id that never has ventas → empty, non-nil, no error.
		cats, err := repo.CategoriasCompradas(ctx, 999999999)
		require.NoError(t, err)
		assert.NotNil(t, cats)
		assert.Empty(t, cats)
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestCategoriasRepo_RealClienteReturnsDistinctLineas(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		// Cliente 24037 (Minerva López) is a known real cliente with credit
		// venta history in the dev DB (used across other integration fixtures).
		cats, err := repo.CategoriasCompradas(ctx, 24037)
		require.NoError(t, err)
		require.NotNil(t, cats)
		if len(cats) == 0 {
			t.Skip("cliente 24037 has no venta history in this dev DB snapshot")
		}

		// Result must be DISTINCT positive line ids.
		seen := make(map[int]bool, len(cats))
		for _, id := range cats {
			assert.Positive(t, id, "LINEA_ARTICULO_ID must be positive")
			assert.False(t, seen[id], "line %d duplicated — query must return DISTINCT", id)
			seen[id] = true
		}
		t.Logf("cliente 24037 compró en %d categorías: %v", len(cats), cats)
	})
}
