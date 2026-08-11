//nolint:misspell // Spanish domain vocabulary (venta, producto, articulo) by project convention.
package ventfb_test

// Integration tests for VentasRepo.ProductosByPVIDs (productos_repo.go).
//
// Read-only: discovers a live DOCTO_PV_ID with at least one DOCTOS_PV_DET
// row whose ROL is 'N' or 'J', then exercises ProductosByPVIDs against it.
// No fixtures are inserted — the whole test runs inside a rollback-only
// transaction (fbtestutil.WithTestTransaction) purely for isolation; it never
// writes.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// findPVWithProductoLines returns a DOCTO_PV_ID that has at least one
// DOCTOS_PV_DET row with ROL IN ('N', 'J'). Skips the test when none exists.
func findPVWithProductoLines(t *testing.T, q firebird.Querier) int {
	t.Helper()
	var pvID int
	err := q.QueryRowContext(context.Background(),
		`SELECT FIRST 1 DOCTO_PV_ID FROM DOCTOS_PV_DET
		 WHERE ROL IN ('N', 'J')
		 GROUP BY DOCTO_PV_ID
		 HAVING COUNT(*) >= 1
		 ORDER BY DOCTO_PV_ID`).Scan(&pvID)
	if err != nil {
		t.Skipf("no DOCTOS_PV_DET row with ROL IN ('N','J') available: %v", err)
	}
	return pvID
}

// TestE2E_VentasRepo_ProductosByPVIDs_HappyPath discovers a live venta with
// product lines and verifies ProductosByPVIDs returns them correctly shaped,
// ordered by POSICION, and filtered to ROL IN ('N', 'J').
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VentasRepo_ProductosByPVIDs_HappyPath(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		pvID := findPVWithProductoLines(t, q)

		var expectedCount int
		err := q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM DOCTOS_PV_DET WHERE DOCTO_PV_ID = ? AND ROL IN ('N', 'J')`,
			pvID).Scan(&expectedCount)
		require.NoError(t, err, "cross-check ROL count query")
		require.GreaterOrEqual(t, expectedCount, 1)

		repo := cobranzaventfb.NewVentasRepo(pool)
		result, err := repo.ProductosByPVIDs(ctx, []int{pvID})
		require.NoError(t, err)

		lines, ok := result[pvID]
		require.True(t, ok, "map must have an entry for the discovered DOCTO_PV_ID")
		require.GreaterOrEqual(t, len(lines), 1)

		// ROL filter cross-check: DOCTOS_PV_DET's ROL IN ('N','J') count must
		// equal the number of returned lines (ROL='C' kit components excluded).
		assert.Len(t, lines, expectedCount,
			"returned line count must match the ROL IN ('N','J') count — ROL='C' rows excluded")

		lastPosicion := -1
		for _, l := range lines {
			assert.Equal(t, pvID, l.DoctoPVID(), "every line must belong to the requested DOCTO_PV_ID")
			assert.NotEmpty(t, l.Folio(), "FOLIO must not be empty")
			assert.Positive(t, l.ArticuloID(), "ARTICULO_ID must be positive")
			assert.GreaterOrEqual(t, l.Posicion(), 0, "POSICION must be non-negative")
			assert.GreaterOrEqual(t, l.PrecioUnitario().Sign(), 0, "PRECIO_UNITARIO_IMPTO must be >= 0")
			assert.GreaterOrEqual(t, l.PrecioTotalNeto().Sign(), 0, "PRECIO_TOTAL_NETO must be >= 0")
			assert.GreaterOrEqual(t, l.Posicion(), lastPosicion, "lines must be ordered by POSICION ascending")
			lastPosicion = l.Posicion()
		}

		t.Logf("ProductosByPVIDs pvID=%d lines=%d expectedByRolFilter=%d", pvID, len(lines), expectedCount)
	})
}

// TestE2E_VentasRepo_ProductosByPVIDs_EmptyInput verifies that an empty slice
// of IDs returns a non-nil, empty map with no error (short-circuit path, no
// query executed).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VentasRepo_ProductosByPVIDs_EmptyInput(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := cobranzaventfb.NewVentasRepo(pool)
		result, err := repo.ProductosByPVIDs(ctx, []int{})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Empty(t, result)
	})
}

// TestE2E_VentasRepo_ProductosByPVIDs_NonExistentID verifies that a
// non-existent DOCTO_PV_ID produces no entry in the returned map (absent, not
// an empty slice) and no error.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VentasRepo_ProductosByPVIDs_NonExistentID(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := cobranzaventfb.NewVentasRepo(pool)
		result, err := repo.ProductosByPVIDs(ctx, []int{-1})
		require.NoError(t, err)
		require.NotNil(t, result)
		_, ok := result[-1]
		assert.False(t, ok, "a non-existent DOCTO_PV_ID must not appear in the result map")
	})
}
