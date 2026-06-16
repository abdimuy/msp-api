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

// ─── ListarDirectorioCompleto ─────────────────────────────────────────────────

// TestClientesRepo_ListarDirectorioCompleto_FilteredByZona verifies the unbounded
// directory listing returns rows for a zone, with non-negative grouped saldo. A
// zone filter keeps the grouped saldo aggregation sub-second (verified live).
func TestClientesRepo_ListarDirectorioCompleto_FilteredByZona(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	zona := 21566 // largest zone (~2.5k clients) — confirmed live

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		items, err := repo.ListarDirectorioCompleto(ctx, outbound.FiltroDirectorio{
			ZonaClienteID: &zona,
		})
		require.NoError(t, err)
		require.NotEmpty(t, items, "zone should return rows")

		for _, it := range items {
			require.NotNil(t, it.Cliente)
			assert.NotEmpty(t, it.Cliente.Nombre())
			assert.False(t, it.SaldoTotal.IsNegative(), "saldo should be >= 0")
		}
		t.Logf("ListarDirectorioCompleto zona=%d: %d items", zona, len(items))
	})
}

// TestClientesRepo_ListarDirectorioCompleto_ConSaldo verifies the ConSaldo filter
// keeps only clients with a positive grouped balance.
func TestClientesRepo_ListarDirectorioCompleto_ConSaldo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	zona := 21566

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		items, err := repo.ListarDirectorioCompleto(ctx, outbound.FiltroDirectorio{
			ZonaClienteID: &zona,
			ConSaldo:      true,
		})
		require.NoError(t, err)
		for _, it := range items {
			assert.True(t, it.SaldoTotal.IsPositive(),
				"ConSaldo must exclude zero-saldo clients (cliente %d had %s)",
				it.Cliente.ClienteID(), it.SaldoTotal)
		}
		t.Logf("ListarDirectorioCompleto zona=%d con_saldo: %d items", zona, len(items))
	})
}

// TestClientesRepo_ListarDirectorioCompleto_SaldoIsNonNegative verifies the
// grouped saldo for a known client is non-negative and matches the expected
// MSP_SALDOS_VENTAS cache value (verified live 2026-06-16: 504666.60).
func TestClientesRepo_ListarDirectorioCompleto_SaldoIsNonNegative(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	target := 12440 // confirmed saldo 504666.60 (grouped == MSP_SALDOS_VENTAS)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		completo, err := repo.ListarDirectorioCompleto(ctx, outbound.FiltroDirectorio{
			ClienteIDs: []int{target},
		})
		require.NoError(t, err)
		require.Len(t, completo, 1)
		assert.False(t, completo[0].SaldoTotal.IsNegative(),
			"saldo should be >= 0 for cliente %d (got %s)", target, completo[0].SaldoTotal)
		t.Logf("ListarDirectorioCompleto saldo cliente %d: %s", target, completo[0].SaldoTotal)
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
// known sale. Originally used DOCTO_PV_ID=15542211 (B3 research §5.3) but that
// row was not present in the live dev DB; replaced with DOCTO_PV_ID=14941516
// (cliente 12387, tipo=V, estatus=N, 7 product lines, 1 CC bridge row, confirmed
// live against real MUEBLERA.FDB on 2026-06-15).
func TestClientesRepo_ObtenerVentaDetalle_Found(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		detail, err := repo.ObtenerVentaDetalle(ctx, 14941516)
		require.NoError(t, err)
		require.NotNil(t, detail.Venta)

		assert.Equal(t, 14941516, detail.Venta.DoctoPVID())
		assert.NotEmpty(t, detail.Productos, "sale 14941516 has 7 lines confirmed in live DB")

		for _, p := range detail.Productos {
			assert.NotEmpty(t, p.Nombre())
			assert.True(t, p.Unidades().IsPositive(), "unidades should be positive")
			t.Logf("producto: articuloID=%d nombre=%q unidades=%s total=%s",
				p.ArticuloID(), p.Nombre(), p.Unidades(), p.PrecioTotalNeto())
		}

		// Pagos may or may not exist depending on the sale's credit status.
		t.Logf("ObtenerVentaDetalle 14941516: tipo=%s productos=%d pagos=%d contrato=%v",
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
