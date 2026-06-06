//nolint:misspell // Spanish vocabulary (situacion, sincronizacion, borrador) by convention.
package ventfb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// TestVentaRepo_List_FilterBySituacion_Revisada creates one borrador and two
// revisadas ventas and asserts that situacion=revisada returns only the two
// revisadas.
func TestVentaRepo_List_FilterBySituacion_Revisada(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		now := testNow()

		borrador := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, borrador))

		revisada1 := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, revisada1))
		require.NoError(t, revisada1.EnviarARevision(root, now))
		require.NoError(t, repo.Update(ctx, revisada1))

		revisada2 := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, revisada2))
		require.NoError(t, revisada2.EnviarARevision(root, now))
		require.NoError(t, repo.Update(ctx, revisada2))

		page, err := repo.List(ctx,
			outbound.ListParams{PageSize: 50},
			outbound.ListVentasFilters{Situacion: "revisada"},
		)
		require.NoError(t, err)
		assert.True(t, containsVentaID(page.Items, revisada1.ID()))
		assert.True(t, containsVentaID(page.Items, revisada2.ID()))
		assert.False(t, containsVentaID(page.Items, borrador.ID()))
	})
}

// TestVentaRepo_List_FilterBySincronizacion_Pendiente creates two ventas whose
// default sincronizacion is "pendiente" and asserts that sincronizacion=pendiente
// returns both of them. This exercises the new SINCRONIZACION filter without
// needing a MarcarAplicada helper (which requires a real Microsip docto).
func TestVentaRepo_List_FilterBySincronizacion_Pendiente(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)

		pendiente1 := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, pendiente1))

		pendiente2 := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, pendiente2))

		page, err := repo.List(ctx,
			outbound.ListParams{PageSize: 50},
			outbound.ListVentasFilters{Sincronizacion: "pendiente"},
		)
		require.NoError(t, err)
		assert.True(t, containsVentaID(page.Items, pendiente1.ID()))
		assert.True(t, containsVentaID(page.Items, pendiente2.ID()))
		// All returned rows must be pendiente.
		for _, v := range page.Items {
			assert.Equal(t, "pendiente", string(v.Sincronizacion()),
				"expected only pendiente ventas in page")
		}
	})
}

// TestVentaRepo_List_FilterBySituacion_Cancelada verifies that requesting
// situacion=cancelada (without incluir_canceladas=true) still returns the
// canceled venta. This pins the coherence fix in appendCanceladasFilter that
// skips the CANCELED_AT IS NULL guard when Situacion == "cancelada".
func TestVentaRepo_List_FilterBySituacion_Cancelada(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		now := testNow()

		activa := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, activa))

		cancelada := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, cancelada))
		require.NoError(t, cancelada.Cancelar("invalida", root, now))
		require.NoError(t, repo.Update(ctx, cancelada))

		// situacion=cancelada without incluir_canceladas — must still return
		// the cancelada row (coherence between Situacion filter and CANCELED_AT guard).
		page, err := repo.List(ctx,
			outbound.ListParams{PageSize: 50},
			outbound.ListVentasFilters{Situacion: "cancelada"},
		)
		require.NoError(t, err)
		assert.True(t, containsVentaID(page.Items, cancelada.ID()))
		assert.False(t, containsVentaID(page.Items, activa.ID()))
	})
}
