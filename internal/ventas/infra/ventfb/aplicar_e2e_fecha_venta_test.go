//nolint:misspell // Spanish vocabulary (ventas, fechaVenta, enganche, etc.) by convention.
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
	"github.com/abdimuy/msp-api/internal/ventas/infra/microsip"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── builders with custom FechaVenta ─────────────────────────────────────────

// buildAplicarContadoConFechaVenta builds a CONTADO venta in APROBADA state
// with the given fechaVenta. All other fixture values mirror buildAplicarContado.
func buildAplicarContadoConFechaVenta(t *testing.T, userID uuid.UUID, fechaVenta time.Time) *domain.Venta {
	t.Helper()
	nombre, err := domain.NewNombreCliente("González García María")
	require.NoError(t, err)
	cid := testClienteID
	zona := testZonaID
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle:         "Calle Juárez",
		Colonia:       "Centro",
		Poblacion:     "Tehuacán",
		Ciudad:        "Puebla",
		ZonaClienteID: &zona,
	})
	require.NoError(t, err)
	gps, err := domain.NewGPSCoords(18.46, -97.39)
	require.NoError(t, err)
	precioProducto, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("4320.00"),
		decimal.RequireFromString("3900.00"),
		decimal.RequireFromString("3500.00"),
	)
	require.NoError(t, err)
	almOrigen := testAlmacenID
	almDestino := 11059
	v, err := domain.CrearVenta(domain.CrearVentaParams{
		ID:        uuid.New(),
		ClienteID: &cid,
		Cliente: domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{
			Nombre: nombre,
		}),
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: fechaVenta,
		TipoVenta:  domain.TipoVentaContado,
		Productos: []domain.CrearVentaProductoInput{{
			ID:             uuid.New(),
			ArticuloID:     testArticuloID,
			Articulo:       "Cantinera Caoba",
			Cantidad:       decimal.RequireFromString("1.0000"),
			Precios:        precioProducto,
			AlmacenOrigen:  &almOrigen,
			AlmacenDestino: &almDestino,
		}},
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID:        uuid.New(),
			UsuarioID: userID,
			Email:     "ruta25@muebleriamsp.mx",
			Nombre:    "Vendedor Ruta 25",
		}},
		CreatedBy: userID,
		Now:       fechaVenta,
	})
	require.NoError(t, err)
	attachAplicarEvidencia(t, v, userID)
	require.NoError(t, v.EnviarARevision(userID, fechaVenta))
	require.NoError(t, v.Aprobar(userID, fechaVenta))
	return v
}

// buildAplicarCreditoConFechaVenta builds a CREDITO venta in APROBADA state
// with the given fechaVenta and enganche > 0. All other fixture values mirror
// buildAplicarCredito.
func buildAplicarCreditoConFechaVenta(t *testing.T, userID uuid.UUID, fechaVenta time.Time) *domain.Venta {
	t.Helper()
	nombre, err := domain.NewNombreCliente("Hernández López José")
	require.NoError(t, err)
	aval := domain.HydrateNombreCliente("Ramírez Torres Ana")
	cid := testClienteID
	zona := testZonaID
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle:         "Av. Independencia",
		Colonia:       "San Juan",
		Poblacion:     "Tehuacán",
		Ciudad:        "Puebla",
		ZonaClienteID: &zona,
	})
	require.NoError(t, err)
	gps, err := domain.NewGPSCoords(18.46, -97.38)
	require.NoError(t, err)
	precioProducto, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("9100.00"),
		decimal.RequireFromString("8200.00"),
		decimal.RequireFromString("7000.00"),
	)
	require.NoError(t, err)
	plan, err := domain.NewPlanCredito(
		12,
		decimal.RequireFromString("500.00"),
		decimal.RequireFromString("250.00"),
		domain.FrecPagoSemanal,
	)
	require.NoError(t, err)
	dc, err := domain.NewDiaCobranzaSemana(domain.DiaSemanaLunes)
	require.NoError(t, err)
	nota := "venta credito fecha real e2e"
	almOrigen := testAlmacenID
	almDestino := 11059
	v, err := domain.CrearVenta(domain.CrearVentaParams{
		ID:        uuid.New(),
		ClienteID: &cid,
		Cliente: domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{
			Nombre: nombre,
			Aval:   &aval,
		}),
		Direccion:   dir,
		GPS:         gps,
		FechaVenta:  fechaVenta,
		TipoVenta:   domain.TipoVentaCredito,
		PlanCredito: &plan,
		DiaCobranza: &dc,
		Nota:        &nota,
		Productos: []domain.CrearVentaProductoInput{{
			ID:             uuid.New(),
			ArticuloID:     testArticuloID,
			Articulo:       "Cantinera Caoba",
			Cantidad:       decimal.RequireFromString("1.0000"),
			Precios:        precioProducto,
			AlmacenOrigen:  &almOrigen,
			AlmacenDestino: &almDestino,
		}},
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID:        uuid.New(),
			UsuarioID: userID,
			Email:     "ruta25@muebleriamsp.mx",
			Nombre:    "Vendedor Ruta 25",
		}},
		CreatedBy: userID,
		Now:       fechaVenta,
	})
	require.NoError(t, err)
	attachAplicarEvidencia(t, v, userID)
	require.NoError(t, v.EnviarARevision(userID, fechaVenta))
	require.NoError(t, v.Aprobar(userID, fechaVenta))
	return v
}

// ─── fecha_venta assertions ───────────────────────────────────────────────────

// assertFechaMatchesFechaVenta reads a TIMESTAMP column from DOCTOS_PV (or
// DOCTOS_CC) and asserts its calendar date (in BusinessTZ) matches fechaVenta.
//
// We compare the year/month/day in BusinessTZ rather than the full UTC instant
// to avoid off-by-TZ issues: the writer converts UTC → wall-clock via
// ToWallClock, and the scanner converts wall-clock → UTC via ScanUTCTime; the
// round-trip is lossless but comparing calendar dates in BusinessTZ is robust
// against sub-second driver encoding differences.
func assertFechaMatchesFechaVenta(t *testing.T, label string, raw any, fechaVenta time.Time) {
	t.Helper()
	got, err := firebird.ScanUTCTime(raw)
	require.NoErrorf(t, err, "%s: scan timestamp", label)

	tz := firebird.BusinessTZ()
	wY, wM, wD := fechaVenta.In(tz).Date()
	gY, gM, gD := got.In(tz).Date()

	assert.Equal(t, wY, gY, "%s: year must match FechaVenta()", label)
	assert.Equal(t, wM, gM, "%s: month must match FechaVenta()", label)
	assert.Equal(t, wD, gD, "%s: day must match FechaVenta()", label)
}

// ─── E2E tests ────────────────────────────────────────────────────────────────

// TestE2E_AplicarVenta_FechaVenta_Contado verifies that DOCTOS_PV.FECHA reflects
// the venta's real sale date (FechaVenta()) and NOT the application clock at the
// time of applying.
//
// A CONTADO venta is built with FechaVenta fixed to 2025-03-15 (clearly in the
// past relative to the current date). After Aplicar, DOCTOS_PV.FECHA must match
// that date — if the writer had used time.Now() the assertion would fail because
// today is in 2026.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_AplicarVenta_FechaVenta_Contado(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)

	// Fixed past date clearly different from today (2026).
	fechaVenta := time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		q := firebird.GetQuerier(ctx, pool.DB)

		v := buildAplicarContadoConFechaVenta(t, userID, fechaVenta)

		fpID := formaDePagoSemanalID
		cmID := creditoEnMeses12ID
		in := outbound.MicrosipVentaInput{
			Venta:                v,
			CajaID:               testCajaID,
			CajeroID:             testCajeroID,
			VendedorID:           testVendedorID,
			SucursalID:           testSucursalID,
			FormaCobroID:         formaCobroContadoID,
			FormaDePagoID:        &fpID,
			CreditoEnMesesID:     &cmID,
			NumeroDeVendedoresID: numVendedores1ID,
		}

		res, err := writer.Aplicar(ctx, in)
		require.NoError(t, err)
		assert.Positive(t, res.DoctoPVID)

		var fechaRaw any
		err = q.QueryRowContext(ctx,
			`SELECT FECHA FROM DOCTOS_PV WHERE DOCTO_PV_ID = ?`, res.DoctoPVID,
		).Scan(&fechaRaw)
		require.NoError(t, err, "must read DOCTOS_PV.FECHA")

		assertFechaMatchesFechaVenta(t, "DOCTOS_PV.FECHA", fechaRaw, fechaVenta)

		t.Logf("fecha_venta_contado: DoctoPVID=%d FechaVenta=%s",
			res.DoctoPVID, fechaVenta.Format(time.DateOnly))
	})
}

// TestE2E_AplicarVenta_FechaVenta_Credito_Enganche verifies that both
// DOCTOS_PV.FECHA and the enganche DOCTOS_CC.FECHA reflect FechaVenta(), not
// the application clock, for a CREDITO venta with enganche > 0.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_AplicarVenta_FechaVenta_Credito_Enganche(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)

	// Fixed past date clearly different from today (2026).
	fechaVenta := time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		q := firebird.GetQuerier(ctx, pool.DB)

		v := buildAplicarCreditoConFechaVenta(t, userID, fechaVenta)

		fpID := formaDePagoSemanalID
		cmID := creditoEnMeses12ID
		in := outbound.MicrosipVentaInput{
			Venta:                v,
			CajaID:               testCajaID,
			CajeroID:             testCajeroID,
			VendedorID:           testVendedorID,
			SucursalID:           testSucursalID,
			FormaCobroID:         formaCobroCreditoID,
			FormaDePagoID:        &fpID,
			CreditoEnMesesID:     &cmID,
			NumeroDeVendedoresID: numVendedores1ID,
		}

		res, err := writer.Aplicar(ctx, in)
		require.NoError(t, err)
		assert.Positive(t, res.DoctoPVID)

		// ── DOCTOS_PV.FECHA ──────────────────────────────────────────────────
		var fechaPVRaw any
		err = q.QueryRowContext(ctx,
			`SELECT FECHA FROM DOCTOS_PV WHERE DOCTO_PV_ID = ?`, res.DoctoPVID,
		).Scan(&fechaPVRaw)
		require.NoError(t, err, "must read DOCTOS_PV.FECHA")
		assertFechaMatchesFechaVenta(t, "DOCTOS_PV.FECHA", fechaPVRaw, fechaVenta)

		// ── Enganche DOCTOS_CC.FECHA ─────────────────────────────────────────
		// The cargo CC is generated by the cascade (APLICADO='N'→'S') and linked
		// to the DOCTO_PV via DOCTOS_ENTRE_SIS.
		var cargoCCID int
		err = q.QueryRowContext(ctx, `
			SELECT D.DOCTO_CC_ID
			FROM DOCTOS_ENTRE_SIS E
			JOIN DOCTOS_CC D ON D.DOCTO_CC_ID = E.DOCTO_DEST_ID
			WHERE E.CLAVE_SIS_FTE = 'PV' AND E.CLAVE_SIS_DEST = 'CC' AND E.DOCTO_FTE_ID = ?`,
			res.DoctoPVID,
		).Scan(&cargoCCID)
		require.NoError(t, err, "cargo CC doc must exist via DOCTOS_ENTRE_SIS")

		// The enganche DOCTOS_CC is linked to the cargo via
		// IMPORTES_DOCTOS_CC.DOCTO_CC_ACR_ID (TIPO_IMPTE='R').
		var engancheDoctoID int
		err = q.QueryRowContext(ctx, `
			SELECT DOCTO_CC_ID FROM IMPORTES_DOCTOS_CC
			WHERE DOCTO_CC_ACR_ID = ? AND TIPO_IMPTE = 'R'`,
			cargoCCID,
		).Scan(&engancheDoctoID)
		require.NoError(t, err, "enganche DOCTOS_CC must exist")

		var fechaCCRaw any
		err = q.QueryRowContext(ctx,
			`SELECT FECHA FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, engancheDoctoID,
		).Scan(&fechaCCRaw)
		require.NoError(t, err, "must read enganche DOCTOS_CC.FECHA")
		assertFechaMatchesFechaVenta(t, "enganche DOCTOS_CC.FECHA", fechaCCRaw, fechaVenta)

		t.Logf("fecha_venta_credito: DoctoPVID=%d cargoCCID=%d engancheID=%d FechaVenta=%s",
			res.DoctoPVID, cargoCCID, engancheDoctoID, fechaVenta.Format(time.DateOnly))
	})
}
