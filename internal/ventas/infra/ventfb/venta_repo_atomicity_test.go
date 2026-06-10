//nolint:misspell // Spanish vocabulary (productos, vendedores, combos) by convention.
package ventfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// These tests verify the error path of the wholesale-replace operations.
// True transactional atomicity (DELETE+INSERT rollback on partial failure)
// is provided by the platform's TxManager — which the service layer wraps
// around every Replace* call via runInTx and which has its own dedicated
// tests in internal/platform/firebird. What we verify here is that the
// REPO surfaces a typed apperror on PK/UQ violations, so the outer tx
// machinery sees a real error and rolls back as designed.

func mkProducto(id uuid.UUID, almOrigen, almDestino *int) domain.CrearVentaProductoInput {
	montos, _ := domain.NewMontoSnapshot(decimal.NewFromInt(1), decimal.NewFromInt(1), decimal.NewFromInt(1))
	return domain.CrearVentaProductoInput{
		ID: id, ArticuloID: 1, Articulo: "X",
		Cantidad: decimal.RequireFromString("1.0000"), Precios: montos,
		AlmacenOrigen: almOrigen, AlmacenDestino: almDestino,
	}
}

func TestVentaRepo_ReplaceProductos_DuplicatePK_SurfacesConflict(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		one, two := 1, 2
		dup := uuid.New()
		// Feed two productos sharing the same primary key. The aggregate's
		// ReemplazarProductos accepts the list (it does not de-dupe by ID);
		// the repo's INSERT loop hits a PK violation on the second row.
		err := v.ReemplazarProductos(domain.ReemplazarProductosParams{
			Productos: []domain.CrearVentaProductoInput{
				mkProducto(dup, &one, &two),
				mkProducto(dup, &one, &two),
			},
			By: root, Now: testNow().Add(time.Hour),
		})
		require.NoError(t, err)

		err = repo.ReplaceProductos(ctx, v)
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected typed apperror, got %T: %v", err, err)
		assert.Equal(t, "firebird_unique_violation", appErr.Code)
	})
}

func TestVentaRepo_ReplaceCombos_DuplicatePK_SurfacesConflict(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		montos, _ := domain.NewMontoSnapshot(decimal.NewFromInt(1), decimal.NewFromInt(1), decimal.NewFromInt(1))
		dup := uuid.New()
		err := v.ReemplazarCombos(domain.ReemplazarCombosParams{
			Combos: []domain.CrearVentaComboInput{
				{ID: dup, Nombre: "A", Precios: montos, Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2},
				{ID: dup, Nombre: "B", Precios: montos, Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2},
			},
			By: root, Now: testNow().Add(time.Hour),
		})
		require.NoError(t, err)

		err = repo.ReplaceCombos(ctx, v)
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "firebird_unique_violation", appErr.Code)
	})
}

func TestVentaRepo_ReplaceVendedores_DuplicateUsuarioID_SurfacesConflict(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		// Same usuario assigned twice → UQ_VTA_VEND_VENTA_USR.
		err := v.ReemplazarVendedores(domain.ReemplazarVendedoresParams{
			Vendedores: []domain.CrearVentaVendedorInput{
				{ID: uuid.New(), UsuarioID: root, Email: "a-" + uuid.NewString() + "@x.com", Nombre: "A"},
				{ID: uuid.New(), UsuarioID: root, Email: "b-" + uuid.NewString() + "@x.com", Nombre: "B"},
			},
			By: root, Now: testNow().Add(time.Hour),
		})
		require.NoError(t, err)

		err = repo.ReplaceVendedores(ctx, v)
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		// insertVendedores maps unique conflicts to errVendedorDuplicado
		// (a domain-meaningful apperror). Otherwise it would be the raw
		// firebird_unique_violation code.
		assert.True(t,
			appErr.Code == "venta_vendedor_duplicado" || appErr.Code == "firebird_unique_violation",
			"unexpected error code: %s", appErr.Code,
		)
	})
}
