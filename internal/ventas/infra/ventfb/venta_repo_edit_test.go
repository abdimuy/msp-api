//nolint:misspell // Spanish vocabulary (productos, vendedores) by convention.
package ventfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── UpdateHeader ──────────────────────────────────────────────────────────

func TestVentaRepo_UpdateHeader_PersistsFields(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		newFecha := v.FechaVenta().Add(48 * time.Hour)
		newMontos, _ := domain.NewMontoSnapshot(
			decimal.RequireFromString("3500.00"),
			decimal.RequireFromString("3000.00"),
			decimal.RequireFromString("2500.00"),
		)
		newDir, err := domain.NewDireccion(domain.NewDireccionParams{
			Calle: "Otra Calle", Colonia: "Roma", Poblacion: "CDMX", Ciudad: "CDMX",
		})
		require.NoError(t, err)
		newGPS, err := domain.NewGPSCoords(20.0, -100.0)
		require.NoError(t, err)
		nota := "corrección posterior"
		require.NoError(t, v.ActualizarHeader(domain.ActualizarHeaderParams{
			Direccion: newDir, GPS: newGPS, FechaVenta: newFecha, Montos: newMontos,
			Nota: &nota, By: root, Now: testNow().Add(time.Hour),
		}))
		require.NoError(t, repo.UpdateHeader(ctx, v))

		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		assert.Equal(t, "Otra Calle", got.Direccion().Calle())
		assert.InDelta(t, 20.0, got.GPS().Latitud(), 0.0001)
		// With the BusinessTZ wall-clock contract the instant round-trips
		// exactly (down to seconds — Firebird TIMESTAMP precision is 100µs).
		assert.WithinDuration(t, newFecha, got.FechaVenta(), time.Second,
			"FechaVenta round-trip; want=%s got=%s", newFecha, got.FechaVenta())
		assert.True(t, got.Montos().Anual().Equal(decimal.RequireFromString("3500.00")))
		require.NotNil(t, got.Nota())
		assert.Equal(t, "corrección posterior", *got.Nota())
		assert.Equal(t, domain.SituacionBorrador, got.Situacion())
	})
}

func TestVentaRepo_UpdateHeader_NotFound(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{
			id: uuid.New(), createdBy: root, vendedor: root,
		})
		// Do NOT Save. Just try to update.
		err := repo.UpdateHeader(ctx, v)
		require.ErrorIs(t, err, domain.ErrVentaNotFound)
	})
}

// ─── UpdateCliente ─────────────────────────────────────────────────────────

func TestVentaRepo_UpdateCliente_PersistsSnapshotAndID(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		// Pick a real CLIENTES row to link.
		realID := pickExistingClienteID(ctx, t, pool)

		newNom, err := domain.NewNombreCliente("Cliente Corregido")
		require.NoError(t, err)
		newSnap, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: newNom})
		require.NoError(t, err)
		require.NoError(t, v.ActualizarCliente(domain.ActualizarClienteParams{
			ClienteID: &realID, Cliente: newSnap, By: root, Now: testNow().Add(time.Hour),
		}))
		require.NoError(t, repo.UpdateCliente(ctx, v))

		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		assert.Equal(t, "Cliente Corregido", got.Cliente().Nombre().Value())
		require.NotNil(t, got.ClienteID())
		assert.Equal(t, realID, *got.ClienteID())
	})
}

func TestVentaRepo_UpdateCliente_NullsOutID(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		// First seed with a non-nil cliente_id, then null it out.
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		realID := pickExistingClienteID(ctx, t, pool)
		// Re-hydrate with cliente_id set via Actualizar.
		require.NoError(t, repo.Save(ctx, v))
		snap, _ := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: v.Cliente().Nombre()})
		require.NoError(t, v.ActualizarCliente(domain.ActualizarClienteParams{
			ClienteID: &realID, Cliente: snap, By: root, Now: testNow().Add(time.Hour),
		}))
		require.NoError(t, repo.UpdateCliente(ctx, v))

		// Now clear it (cliente_id = nil).
		require.NoError(t, v.ActualizarCliente(domain.ActualizarClienteParams{
			ClienteID: nil, Cliente: snap, By: root, Now: testNow().Add(2 * time.Hour),
		}))
		require.NoError(t, repo.UpdateCliente(ctx, v))

		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		assert.Nil(t, got.ClienteID())
	})
}

// ─── ReplaceProductos ──────────────────────────────────────────────────────

func TestVentaRepo_ReplaceProductos_SwapsCollection(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		productoPrecios, _ := domain.NewMontoSnapshot(
			decimal.RequireFromString("60.00"),
			decimal.RequireFromString("55.00"),
			decimal.RequireFromString("50.00"),
		)
		one, two := 1, 2
		require.NoError(t, v.ReemplazarProductos(domain.ReemplazarProductosParams{
			Productos: []domain.CrearVentaProductoInput{
				{
					ID: uuid.New(), ArticuloID: 1, Articulo: "P1",
					Cantidad: decimal.RequireFromString("2.0000"), Precios: productoPrecios,
					AlmacenOrigen: &one, AlmacenDestino: &two,
				},
				{
					ID: uuid.New(), ArticuloID: 2, Articulo: "P2",
					Cantidad: decimal.RequireFromString("3.0000"), Precios: productoPrecios,
					AlmacenOrigen: &one, AlmacenDestino: &two,
				},
			},
			By: root, Now: testNow().Add(time.Hour),
		}))
		require.NoError(t, repo.ReplaceProductos(ctx, v))

		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		assert.Equal(t, 2, got.ProductosCount())
		q := firebird.GetQuerier(ctx, pool.DB)
		assertCount(ctx, t, q, "MSP_VENTAS_PRODUCTOS", v.ID().String(), "VENTA_ID", 2)
	})
}

func TestVentaRepo_ReplaceProductos_RecordsAlmacenesPerLine(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		precios, _ := domain.NewMontoSnapshot(
			decimal.RequireFromString("60.00"),
			decimal.RequireFromString("55.00"),
			decimal.RequireFromString("50.00"),
		)
		// Two productos from different origin warehouses.
		o1, o2, d := 10, 20, 99
		require.NoError(t, v.ReemplazarProductos(domain.ReemplazarProductosParams{
			Productos: []domain.CrearVentaProductoInput{
				{
					ID: uuid.New(), ArticuloID: 1, Articulo: "Mesa", Cantidad: decimal.RequireFromString("1"),
					Precios: precios, AlmacenOrigen: &o1, AlmacenDestino: &d,
				},
				{
					ID: uuid.New(), ArticuloID: 2, Articulo: "Silla", Cantidad: decimal.RequireFromString("1"),
					Precios: precios, AlmacenOrigen: &o2, AlmacenDestino: &d,
				},
			},
			By: root, Now: testNow().Add(time.Hour),
		}))
		require.NoError(t, repo.ReplaceProductos(ctx, v))

		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		origins := map[int]struct{}{}
		for p := range got.Productos() {
			require.NotNil(t, p.AlmacenOrigen())
			origins[*p.AlmacenOrigen()] = struct{}{}
		}
		assert.Contains(t, origins, 10)
		assert.Contains(t, origins, 20)
	})
}

// ─── ReplaceCombos ─────────────────────────────────────────────────────────

func TestVentaRepo_ReplaceCombos_SwapsCollection(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		montos, _ := domain.NewMontoSnapshot(
			decimal.RequireFromString("500.00"),
			decimal.RequireFromString("450.00"),
			decimal.RequireFromString("400.00"),
		)
		require.NoError(t, v.ReemplazarCombos(domain.ReemplazarCombosParams{
			Combos: []domain.CrearVentaComboInput{{
				ID: uuid.New(), Nombre: "Bundle Recámara", Precios: montos,
				Cantidad: decimal.RequireFromString("3.0000"), AlmacenOrigen: 5, AlmacenDestino: 6,
			}},
			By: root, Now: testNow().Add(time.Hour),
		}))
		require.NoError(t, repo.ReplaceCombos(ctx, v))

		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		assert.Equal(t, 1, got.CombosCount())
		for c := range got.Combos() {
			assert.Equal(t, "Bundle Recámara", c.Nombre())
			assert.True(t, c.Cantidad().Equal(decimal.RequireFromString("3.0000")))
			assert.Equal(t, 5, c.AlmacenOrigen())
			assert.Equal(t, 6, c.AlmacenDestino())
		}
	})
}

func TestVentaRepo_ReplaceCombos_RejectsOrphanedProductos(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		// buildRichVenta with combo puts a producto inside that combo; dropping
		// the combo without first reemplazando productos must be rejected by
		// the aggregate (productos.combo_id no longer resolves).
		v := buildRichVenta(t, root, root, richVentaOptions{withCombo: true})
		require.NoError(t, repo.Save(ctx, v))
		err := v.ReemplazarCombos(domain.ReemplazarCombosParams{
			Combos: nil, By: root, Now: testNow().Add(time.Hour),
		})
		require.ErrorIs(t, err, domain.ErrProductoComboReferenciaInvalida)
	})
}

// ─── ReplaceVendedores ─────────────────────────────────────────────────────

func TestVentaRepo_ReplaceVendedores_SwapsCollection(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		other := seedUsuarioRow(ctx, t, pool)
		require.NoError(t, v.ReemplazarVendedores(domain.ReemplazarVendedoresParams{
			Vendedores: []domain.CrearVentaVendedorInput{
				{ID: uuid.New(), UsuarioID: root, Email: "a-" + uuid.NewString() + "@x.com", Nombre: "Vend A"},
				{ID: uuid.New(), UsuarioID: other, Email: "b-" + uuid.NewString() + "@x.com", Nombre: "Vend B"},
			},
			By: root, Now: testNow().Add(time.Hour),
		}))
		require.NoError(t, repo.ReplaceVendedores(ctx, v))

		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		assert.Equal(t, 2, got.VendedoresCount())
		q := firebird.GetQuerier(ctx, pool.DB)
		assertCount(ctx, t, q, "MSP_VENTAS_VENDEDORES", v.ID().String(), "VENTA_ID", 2)
	})
}

// ─── List filter: ClienteID ────────────────────────────────────────────────

func TestVentaRepo_List_FilterByClienteID(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		realID := pickExistingClienteID(ctx, t, pool)

		// Seed two ventas: one with cliente_id, one without.
		v1 := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v1))
		snap, _ := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: v1.Cliente().Nombre()})
		require.NoError(t, v1.ActualizarCliente(domain.ActualizarClienteParams{
			ClienteID: &realID, Cliente: snap, By: root, Now: testNow().Add(time.Hour),
		}))
		require.NoError(t, repo.UpdateCliente(ctx, v1))

		v2 := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v2))

		// List with the cliente_id filter — only v1 should appear (among any
		// pre-existing rows that happen to share the same cliente_id).
		page, err := repo.List(ctx,
			outbound.ListParams{PageSize: 50},
			outbound.ListVentasFilters{ClienteID: &realID},
		)
		require.NoError(t, err)
		require.NotEmpty(t, page.Items)
		// v1 must be in the page; v2 must NOT.
		assert.True(t, containsVentaID(page.Items, v1.ID()), "v1 should appear in cliente_id-filtered list")
		assert.False(t, containsVentaID(page.Items, v2.ID()), "v2 (no cliente_id) must NOT appear")
		// Every returned row carries the expected cliente_id.
		for _, it := range page.Items {
			require.NotNil(t, it.ClienteID(), "filtered rows must have non-nil cliente_id")
			assert.Equal(t, realID, *it.ClienteID())
		}
	})
}
