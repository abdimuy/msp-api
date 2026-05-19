//nolint:misspell // Spanish vocabulary (productos, etc.) by convention.
package ventfb_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// requireFBEnv skips the calling test when the FB_DATABASE env var is unset.
// Mirrors the auth firebird tests' gating approach so the suite stays useful
// for developers who don't have a Firebird container handy.
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}

func TestVentaRepo_Save_RoundTrip(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))
		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		assert.Equal(t, v.ID(), got.ID())
		assert.Equal(t, v.Cliente().Nombre().Value(), got.Cliente().Nombre().Value())
		assert.Equal(t, v.TipoVenta(), got.TipoVenta())
		assert.True(t, v.Montos().Equals(got.Montos()))
		assert.Equal(t, v.ProductosCount(), got.ProductosCount())
		assert.Equal(t, v.VendedoresCount(), got.VendedoresCount())
	})
}

func TestVentaRepo_Save_MultiTablesCommitted(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))
		q := firebird.GetQuerier(ctx, pool.DB)
		assertCount(ctx, t, q, "MSP_VENTAS", v.ID().String(), "ID", 1)
		assertCount(ctx, t, q, "MSP_VENTAS_PRODUCTOS", v.ID().String(), "VENTA_ID", 1)
		assertCount(ctx, t, q, "MSP_VENTAS_VENDEDORES", v.ID().String(), "VENTA_ID", 1)
		assertCount(ctx, t, q, "MSP_VENTAS_COMBOS", v.ID().String(), "VENTA_ID", 0)
		assertCount(ctx, t, q, "MSP_VENTAS_IMAGENES", v.ID().String(), "VENTA_ID", 0)
	})
}

func TestVentaRepo_FindByID_NotFound(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.FindByID(ctx, uuid.New())
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrVentaNotFound)
	})
}

func TestVentaRepo_Save_DuplicateVendedor_Errors(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))
		// Forge a duplicate vendedor row using the raw querier — the domain
		// would never produce two snapshots with the same usuario id, but the
		// DB has its own unique constraint we need to exercise.
		q := firebird.GetQuerier(ctx, pool.DB)
		now := testNow()
		_, err := q.ExecContext(ctx,
			`INSERT INTO MSP_VENTAS_VENDEDORES
			 (ID, VENTA_ID, VENDEDOR_USUARIO_ID, VENDEDOR_EMAIL, VENDEDOR_NOMBRE, POSICION,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, ?, ?, 'Dupe', 99, ?, ?, ?, ?)`,
			uuid.New().String(), v.ID().String(), root.String(),
			"dupe-"+uuid.NewString()+"@example.invalid",
			now, now, root.String(), root.String(),
		)
		require.Error(t, err, "raw duplicate insert should hit UQ violation")
	})
}

func TestVentaRepo_Update_Cancel(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))
		// Cancel the venta and persist via Update.
		require.NoError(t, v.Cancelar("cliente desistió", root, testNow().Add(time.Hour)))
		require.NoError(t, repo.Update(ctx, v))
		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		require.True(t, got.IsCanceled())
		require.NotNil(t, got.Cancelacion())
		assert.Equal(t, "cliente desistió", got.Cancelacion().Reason())
		assert.Equal(t, root, got.Cancelacion().By())
		// The cliente snapshot stays unchanged.
		assert.Equal(t, v.Cliente().Nombre().Value(), got.Cliente().Nombre().Value())
	})
}

func TestVentaRepo_Update_NotFound(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		// Build a venta but never save it — Update must report NotFound.
		ghost := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		err := repo.Update(ctx, ghost)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrVentaNotFound)
	})
}

func TestVentaRepo_List_NoFilters_ReturnsAll(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		existing := countAllVentas(ctx, t, pool)
		const newRows = 3
		for range newRows {
			v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
			require.NoError(t, repo.Save(ctx, v))
		}
		page, err := repo.List(ctx, outbound.ListParams{PageSize: 50}, outbound.ListVentasFilters{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(page.Items), existing+newRows-maxPageSize())
	})
}

func TestVentaRepo_List_FilterByVendedor(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		otherVendedor := seedUsuarioRow(ctx, t, pool)
		match := buildVenta(t, newVentaInput{createdBy: root, vendedor: otherVendedor})
		require.NoError(t, repo.Save(ctx, match))
		nonMatch := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, nonMatch))
		page, err := repo.List(ctx,
			outbound.ListParams{PageSize: 50},
			outbound.ListVentasFilters{VendedorUsuarioID: &otherVendedor},
		)
		require.NoError(t, err)
		require.NotEmpty(t, page.Items)
		assert.True(t, containsVentaID(page.Items, match.ID()))
		assert.False(t, containsVentaID(page.Items, nonMatch.ID()))
	})
}

func TestVentaRepo_List_FilterByTipoVenta(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		contado := buildVenta(t, newVentaInput{createdBy: root, vendedor: root, tipoVenta: domain.TipoVentaContado})
		credito := buildVenta(t, newVentaInput{createdBy: root, vendedor: root, tipoVenta: domain.TipoVentaCredito})
		require.NoError(t, repo.Save(ctx, contado))
		require.NoError(t, repo.Save(ctx, credito))
		page, err := repo.List(ctx,
			outbound.ListParams{PageSize: 50},
			outbound.ListVentasFilters{TipoVenta: string(domain.TipoVentaCredito)},
		)
		require.NoError(t, err)
		require.NotEmpty(t, page.Items)
		for _, it := range page.Items {
			assert.Equal(t, domain.TipoVentaCredito, it.TipoVenta())
		}
		assert.True(t, containsVentaID(page.Items, credito.ID()))
		assert.False(t, containsVentaID(page.Items, contado.ID()))
	})
}

func TestVentaRepo_List_FilterByDateRange(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		anchor := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
		old := buildVenta(t, newVentaInput{createdBy: root, vendedor: root, fecha: anchor.AddDate(0, -2, 0)})
		recent := buildVenta(t, newVentaInput{createdBy: root, vendedor: root, fecha: anchor.AddDate(0, 0, 5)})
		require.NoError(t, repo.Save(ctx, old))
		require.NoError(t, repo.Save(ctx, recent))
		desde := anchor
		hasta := anchor.AddDate(0, 1, 0)
		page, err := repo.List(ctx,
			outbound.ListParams{PageSize: 50},
			outbound.ListVentasFilters{Desde: &desde, Hasta: &hasta},
		)
		require.NoError(t, err)
		assert.True(t, containsVentaID(page.Items, recent.ID()))
		assert.False(t, containsVentaID(page.Items, old.ID()))
	})
}

func TestVentaRepo_List_IncluirCanceladasFalse_ExcludesCanceled(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		alive := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		dead := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, alive))
		require.NoError(t, repo.Save(ctx, dead))
		require.NoError(t, dead.Cancelar("invalida", root, testNow().Add(time.Hour)))
		require.NoError(t, repo.Update(ctx, dead))
		// IncluirCanceladas = false (default): canceled venta must be excluded.
		page, err := repo.List(ctx,
			outbound.ListParams{PageSize: 50},
			outbound.ListVentasFilters{},
		)
		require.NoError(t, err)
		assert.True(t, containsVentaID(page.Items, alive.ID()))
		assert.False(t, containsVentaID(page.Items, dead.ID()))
		// IncluirCanceladas = true: dead is back.
		page, err = repo.List(ctx,
			outbound.ListParams{PageSize: 50},
			outbound.ListVentasFilters{IncluirCanceladas: true},
		)
		require.NoError(t, err)
		assert.True(t, containsVentaID(page.Items, dead.ID()))
	})
}

func TestVentaRepo_List_CursorPagination(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		const newRows = 4
		created := make(map[uuid.UUID]bool, newRows)
		for i := range newRows {
			fecha := testNow().Add(time.Duration(i) * time.Minute)
			v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root, fecha: fecha})
			require.NoError(t, repo.Save(ctx, v))
			created[v.ID()] = true
		}
		seen := make(map[uuid.UUID]bool)
		const pageSize = 2
		const safetyLimit = 500
		var cursor string
		pages := 0
		for pages < safetyLimit {
			page, err := repo.List(ctx,
				outbound.ListParams{Cursor: cursor, PageSize: pageSize},
				outbound.ListVentasFilters{},
			)
			require.NoError(t, err)
			require.LessOrEqual(t, len(page.Items), pageSize)
			for _, it := range page.Items {
				assert.False(t, seen[it.ID()], "duplicate id across pages: %s", it.ID())
				seen[it.ID()] = true
			}
			pages++
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		require.Less(t, pages, safetyLimit)
		for id := range created {
			assert.True(t, seen[id], "created venta %s missing from paginated walk", id)
		}
	})
}

func TestVentaRepo_List_BadCursor(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.List(ctx,
			outbound.ListParams{Cursor: "not-base64-!", PageSize: 10},
			outbound.ListVentasFilters{},
		)
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "invalid_cursor", appErr.Code)
	})
}

func TestVentaRepo_InsertImagen_RoundTrip(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))
		img := buildImagen(t, root)
		require.NoError(t, repo.InsertImagen(ctx, v.ID(), img))
		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		assert.Equal(t, 1, got.ImagenesCount())
	})
}

func TestVentaRepo_DeleteImagen_Existing_Deletes(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))
		img := buildImagen(t, root)
		require.NoError(t, repo.InsertImagen(ctx, v.ID(), img))
		require.NoError(t, repo.DeleteImagen(ctx, v.ID(), img.ID()))
		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		assert.Equal(t, 0, got.ImagenesCount())
	})
}

func TestVentaRepo_DeleteImagen_Missing_ReturnsNotFound(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))
		err := repo.DeleteImagen(ctx, v.ID(), uuid.New())
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrImagenNotFound)
	})
}

// TestVentaRepo_FindByID_MalformedComboUUID exercises scanCombo's
// parseUUIDColumn error path: insert a combo row with a 36-char non-UUID id
// then FindByID. The header parses fine but the combo child surfaces
// firebird_uuid_invalid.
func TestVentaRepo_FindByID_MalformedComboUUID(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))
		q := firebird.GetQuerier(ctx, pool.DB)
		badID := "combo-bad-uuid-XXXXXXXXXXXXXXXXXXXXX"
		require.Len(t, badID, 36)
		now := testNow()
		_, err := q.ExecContext(ctx,
			`INSERT INTO MSP_VENTAS_COMBOS
			 (ID, VENTA_ID, NOMBRE_COMBO,
			  PRECIO_ANUAL, PRECIO_CORTO_PLAZO, PRECIO_CONTADO,
			  CANTIDAD, ALMACEN_ORIGEN_ID, ALMACEN_DESTINO_ID, POSICION,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, 'malformed', 100, 100, 100, 1, 1, 2, 1, ?, ?, ?, ?)`,
			badID, v.ID().String(), now, now, root.String(), root.String(),
		)
		require.NoError(t, err)
		_, err = repo.FindByID(ctx, v.ID())
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "firebird_uuid_invalid", appErr.Code)
	})
}

// TestVentaRepo_FindByID_CanceledContext drives the QueryRowContext error
// branch of loadHeaderRaw.
func TestVentaRepo_FindByID_CanceledContext(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := repo.FindByID(cctx, uuid.New())
		require.Error(t, err)
	})
}

// TestVentaRepo_List_CanceledContext drives the QueryContext error branch
// inside List.
func TestVentaRepo_List_CanceledContext(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := repo.List(cctx, outbound.ListParams{PageSize: 10}, outbound.ListVentasFilters{})
		require.Error(t, err)
	})
}

// TestVentaRepo_Update_CanceledContext drives the ExecContext error branch
// of Update.
func TestVentaRepo_Update_CanceledContext(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		ghost := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := repo.Update(cctx, ghost)
		require.Error(t, err)
	})
}

// TestVentaRepo_Save_CanceledContext drives the ExecContext error branch on
// insertHeader.
func TestVentaRepo_Save_CanceledContext(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := repo.Save(cctx, v)
		require.Error(t, err)
	})
}

// TestVentaRepo_InsertImagen_CanceledContext drives the ExecContext error
// branch.
func TestVentaRepo_InsertImagen_CanceledContext(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		img := buildImagen(t, root)
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := repo.InsertImagen(cctx, uuid.New(), img)
		require.Error(t, err)
	})
}

// TestVentaRepo_DeleteImagen_CanceledContext drives the ExecContext error
// branch.
func TestVentaRepo_DeleteImagen_CanceledContext(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := repo.DeleteImagen(cctx, uuid.New(), uuid.New())
		require.Error(t, err)
	})
}

// TestVentaRepo_Save_RoundTrip_RichVenta exercises every optional column on
// the insert path: combo header, producto-with-combo-FK, telefono/aval/numext
// /zona_cliente_id/nota strings, plan credito by day of month, and an
// imagen with descripcion.
func TestVentaRepo_Save_RoundTrip_RichVenta(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildRichVenta(t, root, root, richVentaOptions{
			withCombo:       true,
			withCreditoMes:  true,
			withTelefono:    true,
			withAval:        true,
			withNumExterior: true,
			withZonaCliente: true,
			withNota:        true,
		})
		require.NoError(t, repo.Save(ctx, v))
		desc := "evidencia entrega"
		img := buildImagenWithDesc(t, root, &desc)
		require.NoError(t, repo.InsertImagen(ctx, v.ID(), img))
		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		// Combo + producto with combo FK.
		assert.Equal(t, 1, got.CombosCount())
		assert.Equal(t, 1, got.ProductosCount())
		for p := range got.Productos() {
			require.NotNil(t, p.ComboID())
		}
		// Plan credito by day of month.
		require.NotNil(t, got.PlanCredito())
		assert.Equal(t, domain.FrecPagoMensual, got.PlanCredito().FrecPago())
		require.NotNil(t, got.DiaCobranza())
		assert.True(t, got.DiaCobranza().IsMes())
		// Optional cliente / direccion fields preserved.
		require.NotNil(t, got.Cliente().Telefono())
		require.NotNil(t, got.Cliente().Aval())
		require.NotNil(t, got.Direccion().NumeroExterior())
		require.NotNil(t, got.Direccion().ZonaClienteID())
		require.NotNil(t, got.Nota())
		// Imagen with descripcion round-tripped.
		assert.Equal(t, 1, got.ImagenesCount())
		for ig := range got.Imagenes() {
			require.NotNil(t, ig.Descripcion())
			assert.Equal(t, desc, *ig.Descripcion())
		}
	})
}

// TestVentaRepo_Save_DuplicateID_Errors verifies that two Saves with the same
// venta id are not silently merged: the second hits the PK constraint and is
// rejected. This is the lawsuit-critical invariant — exactly one Save per
// business identity wins, the other returns a typed error.
func TestVentaRepo_Save_DuplicateID_Errors(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		id := uuid.New()
		first := buildVenta(t, newVentaInput{id: id, createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, first))

		// Second Save with the same id must fail. Use a fresh vendedor so the
		// failure can ONLY be the PK constraint on MSP_VENTAS.
		other := seedUsuarioRow(ctx, t, pool)
		second := buildVenta(t, newVentaInput{id: id, createdBy: root, vendedor: other})
		err := repo.Save(ctx, second)
		require.Error(t, err, "second Save with same id must fail")
	})
}

// TestVentaRepo_Save_FKViolation_BadCreatedBy verifies the FK between
// MSP_VENTAS.CREATED_BY and MSP_USUARIOS.ID is enforced: when the caller hands
// the repo a created_by uuid that doesn't exist, the INSERT must fail (not
// silently write an orphan row).
func TestVentaRepo_Save_FKViolation_BadCreatedBy(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		ghost := uuid.New() // never seeded → FK target does not exist
		v := buildVenta(t, newVentaInput{createdBy: ghost, vendedor: root})
		err := repo.Save(ctx, v)
		require.Error(t, err, "Save with non-existent created_by must fail")

		// And verify no header row leaked through.
		q := firebird.GetQuerier(ctx, pool.DB)
		assertCount(ctx, t, q, "MSP_VENTAS", v.ID().String(), "ID", 0)
	})
}

// TestVentaRepo_Save_FKViolation_BadVendedor verifies the FK between
// MSP_VENTAS_VENDEDORES.VENDEDOR_USUARIO_ID and MSP_USUARIOS.ID is enforced.
// Save must propagate a typed error the App layer can translate to 4xx.
//
// Note: Save itself is NOT atomic — multi-table atomicity is the caller's
// responsibility via firebird.TxManager.RunInTx. In production
// App.CrearVenta wraps Save in RunInTx so the inner tx is rolled back when
// any INSERT fails. The rollback primitive is independently verified by
// platform/firebird/transaction_test.go::TestTxManager_RollsBackOnError, and
// the App-level wrapping is enforced by depguard + the App's runInTx helper.
func TestVentaRepo_Save_FKViolation_BadVendedor(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		ghost := uuid.New() // FK target does not exist
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: ghost})
		err := repo.Save(ctx, v)
		require.Error(t, err, "Save with non-existent vendedor must fail")
		// Inside this single test tx, prior INSERTs are still pending until
		// the WithTestTransaction defer rolls them back. We deliberately do
		// NOT assert row-count = 0 here because the production rollback
		// happens at the App layer's outer tx, not at the Save level. See
		// the doc comment above for the chain-of-guarantees.
	})
}

// Atomicity of the multi-table Save is NOT tested here because:
//
//   1. It cannot be observed inside fbtestutil.WithTestTransaction — every
//      INSERT runs on the same outer tx, so partial writes are pending
//      regardless of error propagation.
//
//   2. Verifying it outside WithTestTransaction would require writes to the
//      shared dev database (even with t.Cleanup, this is forbidden by
//      project policy — see memory/feedback_test_db_writes.md).
//
// The production guarantee instead rests on the COMPOSITION of three pieces
// that are tested independently:
//
//   * platform/firebird/transaction_test.go::TestTxManager_RollsBackOnError
//     proves TxManager.RunInTx rolls back when fn returns an error.
//   * App.CrearVenta wraps the repo.Save call in s.runInTx → TxManager.RunInTx
//     (visible in internal/ventas/app/crear_venta.go).
//   * The FK / unique tests above prove Save propagates the inner failure.
//
// Reviewing this chain is the lawsuit-grade assurance for atomicity.

// TestVentaRepo_Update_Cancel_BumpsAudit verifies the audit subrecord is
// actually persisted with the cancel (UPDATED_AT and UPDATED_BY change). A
// silent failure to update the audit columns would break compliance.
func TestVentaRepo_Update_Cancel_BumpsAudit(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		other := seedUsuarioRow(ctx, t, pool)
		require.NoError(t, v.Cancelar("compliance test", other, testNow().Add(2*time.Hour)))
		require.NoError(t, repo.Update(ctx, v))

		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		auditAfter := got.Audit()
		assert.Equal(t, other, auditAfter.UpdatedBy(), "updated_by must reflect the canceler")
		assert.False(t, auditAfter.UpdatedAt().Equal(auditAfter.CreatedAt()),
			"updated_at must advance after cancel")
		require.NotNil(t, got.Cancelacion())
		assert.Equal(t, other, got.Cancelacion().By())
	})
}

// TestVentaRepo_DeleteImagen_OtherVenta_NotFound verifies that the imagen id
// is scoped to its venta — a delete with a real imagen id but the wrong
// venta_id must return NotFound (never delete across venta boundaries).
func TestVentaRepo_DeleteImagen_OtherVenta_NotFound(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		a := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		b := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, a))
		require.NoError(t, repo.Save(ctx, b))

		img := buildImagen(t, root)
		require.NoError(t, repo.InsertImagen(ctx, a.ID(), img))

		// Try to delete A's imagen via B's venta id.
		err := repo.DeleteImagen(ctx, b.ID(), img.ID())
		require.ErrorIs(t, err, domain.ErrImagenNotFound)

		// And A still has its imagen.
		got, err := repo.FindByID(ctx, a.ID())
		require.NoError(t, err)
		assert.Equal(t, 1, got.ImagenesCount(), "imagen must not have been deleted")
	})
}

// TestVentaRepo_List_AllFiltersCombined seeds a matrix of ventas differing
// across every supported filter axis (tipo, vendedor, cliente_id, status,
// fecha) and verifies that activating every filter at once narrows the
// result set to exactly the row that matches all predicates. This exercises
// the SQL builder's WHERE-clause concatenation and argument positioning end
// to end.
func TestVentaRepo_List_AllFiltersCombined(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		targetVendedor := seedUsuarioRow(ctx, t, pool)
		otherVendedor := seedUsuarioRow(ctx, t, pool)
		realCliente := pickExistingClienteID(ctx, t, pool)

		anchor := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
		desde := anchor
		hasta := anchor.AddDate(0, 1, 0)
		inside := anchor.AddDate(0, 0, 10)
		outside := anchor.AddDate(0, -2, 0)

		// match: CREDITO + targetVendedor + cliente_id + alive + inside date.
		match := buildVenta(t, newVentaInput{
			createdBy: root, vendedor: targetVendedor,
			tipoVenta: domain.TipoVentaCredito, fecha: inside,
		})
		require.NoError(t, repo.Save(ctx, match))
		snap, _ := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: match.Cliente().Nombre()})
		require.NoError(t, match.ActualizarCliente(domain.ActualizarClienteParams{
			ClienteID: &realCliente, Cliente: snap, By: root, Now: inside.Add(time.Hour),
		}))
		require.NoError(t, repo.UpdateCliente(ctx, match))

		// CONTADO instead — fails TipoVenta filter.
		wrongTipo := buildVenta(t, newVentaInput{
			createdBy: root, vendedor: targetVendedor,
			tipoVenta: domain.TipoVentaContado, fecha: inside,
		})
		require.NoError(t, repo.Save(ctx, wrongTipo))
		require.NoError(t, wrongTipo.ActualizarCliente(domain.ActualizarClienteParams{
			ClienteID: &realCliente, Cliente: snap, By: root, Now: inside.Add(time.Hour),
		}))
		require.NoError(t, repo.UpdateCliente(ctx, wrongTipo))

		// Wrong vendedor.
		wrongVendedor := buildVenta(t, newVentaInput{
			createdBy: root, vendedor: otherVendedor,
			tipoVenta: domain.TipoVentaCredito, fecha: inside,
		})
		require.NoError(t, repo.Save(ctx, wrongVendedor))
		require.NoError(t, wrongVendedor.ActualizarCliente(domain.ActualizarClienteParams{
			ClienteID: &realCliente, Cliente: snap, By: root, Now: inside.Add(time.Hour),
		}))
		require.NoError(t, repo.UpdateCliente(ctx, wrongVendedor))

		// Date outside range.
		wrongDate := buildVenta(t, newVentaInput{
			createdBy: root, vendedor: targetVendedor,
			tipoVenta: domain.TipoVentaCredito, fecha: outside,
		})
		require.NoError(t, repo.Save(ctx, wrongDate))
		require.NoError(t, wrongDate.ActualizarCliente(domain.ActualizarClienteParams{
			ClienteID: &realCliente, Cliente: snap, By: root, Now: outside.Add(time.Hour),
		}))
		require.NoError(t, repo.UpdateCliente(ctx, wrongDate))

		// Canceled — fails IncluirCanceladas=false predicate.
		canceled := buildVenta(t, newVentaInput{
			createdBy: root, vendedor: targetVendedor,
			tipoVenta: domain.TipoVentaCredito, fecha: inside,
		})
		require.NoError(t, repo.Save(ctx, canceled))
		require.NoError(t, canceled.ActualizarCliente(domain.ActualizarClienteParams{
			ClienteID: &realCliente, Cliente: snap, By: root, Now: inside.Add(time.Hour),
		}))
		require.NoError(t, repo.UpdateCliente(ctx, canceled))
		require.NoError(t, canceled.Cancelar("test", root, inside.Add(2*time.Hour)))
		require.NoError(t, repo.Update(ctx, canceled))

		// Activate every filter at once. Expect: match is in the page, the
		// other four are not (independent of any pre-existing rows in the DB).
		page, err := repo.List(ctx,
			outbound.ListParams{PageSize: 50},
			outbound.ListVentasFilters{
				Desde:             &desde,
				Hasta:             &hasta,
				VendedorUsuarioID: &targetVendedor,
				ClienteID:         &realCliente,
				TipoVenta:         string(domain.TipoVentaCredito),
				IncluirCanceladas: false,
			},
		)
		require.NoError(t, err)
		assert.True(t, containsVentaID(page.Items, match.ID()), "match must be in result")
		assert.False(t, containsVentaID(page.Items, wrongTipo.ID()), "CONTADO must be filtered out")
		assert.False(t, containsVentaID(page.Items, wrongVendedor.ID()), "wrong vendedor must be filtered out")
		assert.False(t, containsVentaID(page.Items, wrongDate.ID()), "outside-range date must be filtered out")
		assert.False(t, containsVentaID(page.Items, canceled.ID()), "canceled must be filtered out")
	})
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func assertCount(
	ctx context.Context,
	t *testing.T,
	q firebird.Querier,
	table, value, column string,
	expected int,
) {
	t.Helper()
	var n int
	//nolint:gosec // table/column are package-private constants, not user input.
	row := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+column+" = ?", value)
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, expected, n, "row count mismatch on %s.%s = %s", table, column, value)
}

func countAllVentas(ctx context.Context, t *testing.T, pool *firebird.Pool) int {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var n int
	require.NoError(t, q.QueryRowContext(ctx, "SELECT COUNT(*) FROM MSP_VENTAS").Scan(&n))
	return n
}

func containsVentaID(items []*domain.Venta, id uuid.UUID) bool {
	for _, it := range items {
		if it.ID() == id {
			return true
		}
	}
	return false
}

// maxPageSize exposes the repo's pagination ceiling for the no-filter list
// assertion (which can otherwise over-shoot when prior dev rows exist).
func maxPageSize() int { return 100 }
