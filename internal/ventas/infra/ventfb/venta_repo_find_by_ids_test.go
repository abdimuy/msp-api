//nolint:misspell // Spanish vocabulary (productos, etc.) by convention.
package ventfb_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// TestVentaRepo_FindByIDs_Empty asserts an empty ids slice short-circuits to
// an empty result without issuing a query (no FB round-trip to observe here,
// but the contract is "no error, no rows").
func TestVentaRepo_FindByIDs_Empty(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		got, err := repo.FindByIDs(ctx, nil)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

// TestVentaRepo_FindByIDs_ScrambledSubsetWithMissingID seeds three ventas
// (one rich with combo+producto+vendedor+imagen, two plain), then requests a
// scrambled-order subset plus one id that was never persisted. Asserts:
//   - exactly the existing ventas come back (the missing id is silently
//     dropped, never an error);
//   - every returned venta is fully hydrated (children present), matching
//     what FindByID would return for the same id — this guards the batched
//     IN-query + assembleListItems path against silently losing children.
//
// Never persists outside the rollback-only transaction (fbtestutil.
// WithTestTransaction) per project convention — the shared dev Firebird
// database must never accumulate state from this test.
func TestVentaRepo_FindByIDs_ScrambledSubsetWithMissingID(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)

		ventaA := buildRichVenta(t, root, root, richVentaOptions{withCombo: true})
		require.NoError(t, repo.Save(ctx, ventaA))
		imgA := buildImagen(t, root)
		require.NoError(t, repo.InsertImagen(ctx, ventaA.ID(), imgA))

		ventaB := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, ventaB))

		ventaC := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, ventaC))

		missingID := uuid.New()

		// Scrambled order: C, missing, A, B — deliberately not insertion order
		// and not sorted, to prove the repo does not attempt to preserve or
		// impose any particular order (that's the app layer's job).
		requested := []uuid.UUID{ventaC.ID(), missingID, ventaA.ID(), ventaB.ID()}

		got, err := repo.FindByIDs(ctx, requested)
		require.NoError(t, err)
		require.Len(t, got, 3, "missing id must be silently dropped, not errored")

		byID := make(map[uuid.UUID]*domain.Venta, len(got))
		for _, v := range got {
			byID[v.ID()] = v
		}
		require.Contains(t, byID, ventaA.ID())
		require.Contains(t, byID, ventaB.ID())
		require.Contains(t, byID, ventaC.ID())
		require.NotContains(t, byID, missingID)

		// Cross-check full hydration against the per-id FindByID path for the
		// rich venta (the one with combo+producto+vendedor+imagen).
		wantA, err := repo.FindByID(ctx, ventaA.ID())
		require.NoError(t, err)
		gotA := byID[ventaA.ID()]
		assert.Equal(t, wantA.CombosCount(), gotA.CombosCount())
		assert.Equal(t, wantA.ProductosCount(), gotA.ProductosCount())
		assert.Equal(t, wantA.VendedoresCount(), gotA.VendedoresCount())
		assert.Equal(t, wantA.ImagenesCount(), gotA.ImagenesCount())

		wantB, err := repo.FindByID(ctx, ventaB.ID())
		require.NoError(t, err)
		gotB := byID[ventaB.ID()]
		assert.Equal(t, wantB.ProductosCount(), gotB.ProductosCount())
		assert.Equal(t, wantB.VendedoresCount(), gotB.VendedoresCount())
	})
}
