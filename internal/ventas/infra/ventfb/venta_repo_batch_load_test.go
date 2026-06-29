//nolint:misspell // Spanish vocabulary (combos, productos, vendedores, imagenes) by convention.
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
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// TestVentaRepo_List_BatchLoad_MatchesFindByID inserts two ventas with
// intentionally mixed child collections and then verifies that the children
// returned by List (batch IN-queries) are identical — same order, same IDs,
// same combo_id links — to those returned by FindByID (per-venta queries).
// This guards against regressions where the batch path groups or orders rows
// differently from the per-venta loaders.
func TestVentaRepo_List_BatchLoad_MatchesFindByID(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)

		// Venta A — has a combo + 1 producto linked to that combo + 1 imagen.
		ventaA := buildRichVenta(t, root, root, richVentaOptions{
			withCombo: true,
		})
		require.NoError(t, repo.Save(ctx, ventaA))

		// Attach an imagen to ventaA so the imagenes batch path is exercised.
		imgA := buildImagen(t, root)
		require.NoError(t, repo.InsertImagen(ctx, ventaA.ID(), imgA))

		// Venta B — plain venta: no combos, 1 producto, 1 vendedor, no imagenes.
		ventaB := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, ventaB))

		// ── Load both ventas via the per-venta path (FindByID) ─────────────
		perIDVentaA, err := repo.FindByID(ctx, ventaA.ID())
		require.NoError(t, err)
		perIDVentaB, err := repo.FindByID(ctx, ventaB.ID())
		require.NoError(t, err)

		// ── Load them via the List batch path ──────────────────────────────
		page, err := repo.List(ctx,
			outbound.ListParams{PageSize: 50},
			outbound.ListVentasFilters{},
		)
		require.NoError(t, err)

		listVentaA := findInPage(page.Items, ventaA.ID())
		listVentaB := findInPage(page.Items, ventaB.ID())
		require.NotNil(t, listVentaA, "ventaA must appear in List results")
		require.NotNil(t, listVentaB, "ventaB must appear in List results")

		// ── Assert children are identical between the two load paths ───────
		assertBatchChildrenMatchPerID(t, "ventaA", perIDVentaA, listVentaA)
		assertBatchChildrenMatchPerID(t, "ventaB", perIDVentaB, listVentaB)
	})
}

// findInPage returns the first venta in items whose ID matches id, or nil.
func findInPage(items []*domain.Venta, id uuid.UUID) *domain.Venta {
	for _, v := range items {
		if v.ID() == id {
			return v
		}
	}
	return nil
}

// assertBatchChildrenMatchPerID compares the combos, productos (including
// combo_id links), vendedores, and imagenes between a FindByID result (want)
// and a List result (got) for the same venta. Order must match.
func assertBatchChildrenMatchPerID(t *testing.T, label string, want, got *domain.Venta) {
	t.Helper()

	// ── Counts ──────────────────────────────────────────────────────────────
	assert.Equal(t, want.CombosCount(), got.CombosCount(),
		"%s: combos count mismatch", label)
	assert.Equal(t, want.ProductosCount(), got.ProductosCount(),
		"%s: productos count mismatch", label)
	assert.Equal(t, want.VendedoresCount(), got.VendedoresCount(),
		"%s: vendedores count mismatch", label)
	assert.Equal(t, want.ImagenesCount(), got.ImagenesCount(),
		"%s: imagenes count mismatch", label)

	// ── Combo IDs in order ───────────────────────────────────────────────────
	wantCombos := want.CombosForRepo()
	gotCombos := got.CombosForRepo()
	for i := range wantCombos {
		if i >= len(gotCombos) {
			break
		}
		assert.Equal(t, wantCombos[i].ID(), gotCombos[i].ID(),
			"%s: combo[%d] ID mismatch", label, i)
	}

	// ── Producto IDs in order, plus combo_id link ────────────────────────────
	wantProds := want.ProductosForRepo()
	gotProds := got.ProductosForRepo()
	for i := range wantProds {
		if i >= len(gotProds) {
			break
		}
		assert.Equal(t, wantProds[i].ID(), gotProds[i].ID(),
			"%s: producto[%d] ID mismatch", label, i)
		assert.Equal(t, wantProds[i].ComboID(), gotProds[i].ComboID(),
			"%s: producto[%d] ComboID mismatch", label, i)
	}

	// ── Vendedor IDs in order ────────────────────────────────────────────────
	wantVends := want.VendedoresForRepo()
	gotVends := got.VendedoresForRepo()
	for i := range wantVends {
		if i >= len(gotVends) {
			break
		}
		assert.Equal(t, wantVends[i].ID(), gotVends[i].ID(),
			"%s: vendedor[%d] ID mismatch", label, i)
	}

	// ── Imagen IDs in order ──────────────────────────────────────────────────
	wantImgs := want.ImagenesForRepo()
	gotImgs := got.ImagenesForRepo()
	for i := range wantImgs {
		if i >= len(gotImgs) {
			break
		}
		assert.Equal(t, wantImgs[i].ID(), gotImgs[i].ID(),
			"%s: imagen[%d] ID mismatch", label, i)
	}
}
