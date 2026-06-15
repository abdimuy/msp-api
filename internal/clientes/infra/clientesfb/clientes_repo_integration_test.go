// Integration tests for ClientesRepo against the real Microsip Firebird database.
//
// These tests are SKIPPED when FB_DATABASE is not set (the normal case for
// non-Firebird devs and CI without a DB container). They serve as the live
// verification scaffold for the Fase-1 checkpoint.
//
// All tests run inside fbtestutil.WithTestTransaction which always rolls back —
// no writes are made to the shared dev DB by this read-only repository.
//
//nolint:paralleltest // serial: tests share the rollback-only tx context.
//nolint:misspell    // Spanish domain vocabulary by project convention.
package clientesfb_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

// requireFBEnv skips the test if FB_DATABASE is not set.
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set — point it at the dev Microsip DB to run Firebird integration tests")
	}
}

// ─── ObtenerCliente ───────────────────────────────────────────────────────────

// TestClientesRepo_ObtenerCliente_Found verifies that a known cliente row
// can be hydrated from the real Microsip CLIENTES table.
//
// Uses cliente 12387 (VICTORINO ENRIQUEZ — highest-frequency buyer, confirmed
// in B4 research) as a stable fixture that is very unlikely to be deleted.
func TestClientesRepo_ObtenerCliente_Found(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		c, err := repo.ObtenerCliente(ctx, 12387)
		require.NoError(t, err)
		require.NotNil(t, c)

		assert.Equal(t, 12387, c.ClienteID())
		assert.NotEmpty(t, c.Nombre(), "NOMBRE should never be empty")
		assert.NotEmpty(t, c.Estatus(), "ESTATUS should never be empty")

		t.Logf("ObtenerCliente 12387: nombre=%q estatus=%q zona=%d",
			c.Nombre(), c.Estatus(), c.ZonaClienteID())
	})
}

// TestClientesRepo_ObtenerCliente_NotFound verifies ErrClienteNotFound sentinel.
func TestClientesRepo_ObtenerCliente_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.ObtenerCliente(ctx, -999999)
		assert.ErrorIs(t, err, domain.ErrClienteNotFound)
	})
}

// ─── ListarDirectorio ─────────────────────────────────────────────────────────

// TestClientesRepo_ListarDirectorio_FirstPage verifies a first-page directory
// listing returns items and a non-empty cursor for the next page.
func TestClientesRepo_ListarDirectorio_FirstPage(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		page, err := repo.ListarDirectorio(
			ctx,
			outbound.ListParams{PageSize: 10},
			outbound.FiltroDirectorio{},
		)
		require.NoError(t, err)
		assert.Len(t, page.Items, 10)
		// With ~45k clients, there should always be a next page.
		assert.NotEmpty(t, page.NextCursor)
		// Each item should have a non-nil Cliente.
		for _, item := range page.Items {
			require.NotNil(t, item.Cliente)
			assert.NotEmpty(t, item.Cliente.Nombre())
			assert.False(t, item.SaldoTotal.IsNegative(), "saldo should be >= 0")
		}
		t.Logf("ListarDirectorio first page: %d items, nextCursor=%q",
			len(page.Items), page.NextCursor)
	})
}

// TestClientesRepo_ListarDirectorio_NextPage verifies cursor pagination works.
func TestClientesRepo_ListarDirectorio_NextPage(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		page1, err := repo.ListarDirectorio(
			ctx,
			outbound.ListParams{PageSize: 5},
			outbound.FiltroDirectorio{},
		)
		require.NoError(t, err)
		require.NotEmpty(t, page1.NextCursor)

		page2, err := repo.ListarDirectorio(
			ctx,
			outbound.ListParams{PageSize: 5, Cursor: page1.NextCursor},
			outbound.FiltroDirectorio{},
		)
		require.NoError(t, err)
		assert.NotEmpty(t, page2.Items)

		// Ensure pages don't overlap.
		ids1 := make(map[int]struct{}, len(page1.Items))
		for _, item := range page1.Items {
			ids1[item.Cliente.ClienteID()] = struct{}{}
		}
		for _, item := range page2.Items {
			_, dup := ids1[item.Cliente.ClienteID()]
			assert.False(t, dup, "page 2 item duplicated from page 1: %d", item.Cliente.ClienteID())
		}
	})
}

// TestClientesRepo_ListarDirectorio_FilterClienteIDs verifies IDs filter.
func TestClientesRepo_ListarDirectorio_FilterClienteIDs(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	targetIDs := []int{12387, 12440} // known stable high-frequency buyers

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		page, err := repo.ListarDirectorio(
			ctx,
			outbound.ListParams{PageSize: 10},
			outbound.FiltroDirectorio{ClienteIDs: targetIDs},
		)
		require.NoError(t, err)
		require.Len(t, page.Items, 2)

		gotIDs := make(map[int]struct{})
		for _, item := range page.Items {
			gotIDs[item.Cliente.ClienteID()] = struct{}{}
		}
		for _, id := range targetIDs {
			_, ok := gotIDs[id]
			assert.True(t, ok, "expected cliente %d in filtered result", id)
		}
	})
}

// ─── ObtenerResumenFicha ──────────────────────────────────────────────────────

// TestClientesRepo_ObtenerResumenFicha verifies the financial summary aggregation.
func TestClientesRepo_ObtenerResumenFicha(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		// Use a known credit client from B2 research sample.
		resumen, err := repo.ObtenerResumenFicha(ctx, 2782515)
		require.NoError(t, err)

		// Totals should be positive for a known active credit client.
		assert.True(t, resumen.TotalComprado.IsPositive(),
			"TotalComprado should be positive for active credit client")
		assert.False(t, resumen.SaldoTotal.IsNegative(),
			"SaldoTotal should be >= 0")
		assert.GreaterOrEqual(t, resumen.NumVentas, 0)
		assert.GreaterOrEqual(t, resumen.NumPagos, 0)
		assert.False(t, resumen.PctLiquidado.IsNegative())

		t.Logf("ResumenFicha 2782515: comprado=%s abonado=%s saldo=%s ventas=%d pagos=%d",
			resumen.TotalComprado, resumen.TotalAbonado, resumen.SaldoTotal,
			resumen.NumVentas, resumen.NumPagos)
	})
}

// ─── ListarVentas ─────────────────────────────────────────────────────────────

// TestClientesRepo_ListarVentas verifies ventas pagination for a known client.
func TestClientesRepo_ListarVentas(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		// VICTORINO ENRIQUEZ: 2381 ventas — guaranteed to have multiple pages.
		page, err := repo.ListarVentas(ctx, 12387, outbound.ListParams{PageSize: 10})
		require.NoError(t, err)
		require.Len(t, page.Items, 10)
		assert.NotEmpty(t, page.NextCursor)

		for _, v := range page.Items {
			require.NotNil(t, v)
			assert.Equal(t, 12387, v.ClienteID())
			assert.True(t, v.Tipo().IsValid(), "tipo should be valid")
			assert.False(t, v.SaldoVenta().IsNegative())
			t.Logf("venta %d: fecha=%s tipo=%s total=%s saldo=%s",
				v.DoctoPVID(), v.Fecha().Format("2006-01-02"),
				v.Tipo(), v.Total(), v.SaldoVenta())
		}
	})
}

// ─── ObtenerVentaDetalle ──────────────────────────────────────────────────────

// TestClientesRepo_ObtenerVentaDetalle_Found verifies the detail bundle for a
// known sale (validated in B3 research §5.3: DOCTO_PV_ID=15542211).
func TestClientesRepo_ObtenerVentaDetalle_Found(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		detail, err := repo.ObtenerVentaDetalle(ctx, 15542211)
		require.NoError(t, err)
		require.NotNil(t, detail.Venta)

		assert.Equal(t, 15542211, detail.Venta.DoctoPVID())
		assert.NotEmpty(t, detail.Productos, "sale 15542211 has 4 lines per B3 research")

		for _, p := range detail.Productos {
			assert.NotEmpty(t, p.Nombre())
			assert.True(t, p.Unidades().IsPositive(), "unidades should be positive")
			t.Logf("producto: articuloID=%d nombre=%q unidades=%s total=%s",
				p.ArticuloID(), p.Nombre(), p.Unidades(), p.PrecioTotalNeto())
		}

		// Pagos may or may not exist depending on the sale's credit status.
		t.Logf("ObtenerVentaDetalle 15542211: tipo=%s productos=%d pagos=%d contrato=%v",
			detail.Venta.Tipo(), len(detail.Productos), len(detail.Pagos), detail.Contrato != nil)
	})
}

// TestClientesRepo_ObtenerVentaDetalle_NotFound verifies ErrVentaNotFound.
func TestClientesRepo_ObtenerVentaDetalle_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.ObtenerVentaDetalle(ctx, -999999)
		assert.ErrorIs(t, err, domain.ErrVentaNotFound)
	})
}

// ─── BuscarClienteIDsBasico ───────────────────────────────────────────────────

// TestClientesRepo_BuscarBasico verifies the SQL LIKE fallback search.
func TestClientesRepo_BuscarBasico(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		// "GARCIA" is a common surname in the DB; expect multiple results.
		ids, err := repo.BuscarClienteIDsBasico(ctx, "GARCIA", 5)
		require.NoError(t, err)
		assert.NotEmpty(t, ids)
		assert.LessOrEqual(t, len(ids), 5)
		t.Logf("BuscarBasico 'GARCIA' (limit 5): %v", ids)
	})
}

// TestClientesRepo_BuscarBasico_NoMatch verifies empty result for no match.
func TestClientesRepo_BuscarBasico_NoMatch(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		ids, err := repo.BuscarClienteIDsBasico(ctx, "ZZZZZZZZZZZZZZZZZZZ", 10)
		require.NoError(t, err)
		assert.Empty(t, ids)
	})
}

// ─── LeerDocumentosBusqueda ───────────────────────────────────────────────────

// TestClientesRepo_LeerDocumentos verifies that the search doc bulk load
// returns rows with non-empty ClienteID and Texto.
func TestClientesRepo_LeerDocumentos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		docs, err := repo.LeerDocumentosBusqueda(ctx)
		require.NoError(t, err)
		// Expect at least the ~10k active clientes.
		assert.GreaterOrEqual(t, len(docs), 1000,
			"expected at least 1000 search docs for active clients")

		// Spot-check a few docs for sanity.
		for i, doc := range docs[:min(10, len(docs))] {
			assert.Positive(t, doc.ClienteID, "doc %d: ClienteID should be positive", i)
			assert.NotEmpty(t, doc.Texto, "doc %d: Texto should not be empty", i)
		}
		t.Logf("LeerDocumentosBusqueda: %d docs returned", len(docs))
	})
}
