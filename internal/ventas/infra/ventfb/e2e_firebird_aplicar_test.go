//nolint:misspell // Spanish vocabulary (ventas, clientes, etc.) by convention.
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
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── catalog constants (verified against MUEBLERA.FDB in the spike) ──────────

// Real IDs from MUEBLERA.FDB used only in this test.
const (
	testClienteID       = 11486 // exists in CLIENTES
	testArticuloID      = 378   // ARTICULOS.ES_ALMACENABLE='S' — TASA 0% (no IVA)
	testArticuloID16Pct = 510   // BATERIA FORMACION CINSA — IMPUESTO 339 (16% IVA), almacenable
	testAlmacenID       = 11058 // ruta almacen used by RUTA25 caja
	testZonaID          = 21563 // R/25 → CAJA_ID=22198, CAJERO_ID=22392
	testCajaID          = 22198 // RUTA25
	testCajeroID        = 22392 // CAJERO RUTA25
	testVendedorID      = 88266 // vendedor mapped in MSP_CFG_ZONA_CAJA for zona 21563
	testSucursalID      = 225490

	formaCobroContadoID  = 67 // Efectivo
	formaCobroCreditoID  = 71 // Crédito
	formaDePagoSemanalID = 33824
	creditoEnMeses12ID   = 33828
	numVendedores1ID     = 47558
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildAplicarContado builds a CONTADO venta in situación APROBADA using the
// default TASA-0% test article. See buildAplicarContadoConArticulo for the
// parameterized variant used by the IVA cases.
func buildAplicarContado(t *testing.T, userID uuid.UUID) *domain.Venta {
	t.Helper()
	return buildAplicarContadoConArticulo(t, userID, testArticuloID, "Cantinera Caoba")
}

// buildAplicarContadoConArticulo builds a CONTADO venta in situación APROBADA
// against a specific Microsip ARTICULO_ID + display name, so callers can
// exercise the per-article IVA path (0%, 16%, etc.) deterministically.
func buildAplicarContadoConArticulo(t *testing.T, userID uuid.UUID, articuloID int, articuloNombre string) *domain.Venta {
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
	almDestino := 11059 // destination warehouse (different from origin)
	v, err := domain.CrearVenta(domain.CrearVentaParams{
		ID:        uuid.New(),
		ClienteID: &cid,
		Cliente: domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{
			Nombre: nombre,
		}),
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: time.Now().UTC(),
		TipoVenta:  domain.TipoVentaContado,
		Productos: []domain.CrearVentaProductoInput{{
			ID:             uuid.New(),
			ArticuloID:     articuloID,
			Articulo:       articuloNombre,
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
		Now:       time.Now().UTC(),
	})
	require.NoError(t, err)
	attachAplicarEvidencia(t, v, userID)
	// Advance to aprobada.
	require.NoError(t, v.EnviarARevision(userID, time.Now().UTC()))
	require.NoError(t, v.Aprobar(userID, time.Now().UTC()))
	return v
}

// attachAplicarEvidencia adds a minimal evidencia imagen to the venta so the
// AplicarVenta defense-in-depth guard (ErrVentaEvidenciaRequerida) passes.
// The blob is not actually written to storage here — these tests don't
// exercise the storage path; only the row goes into MSP_VENTAS_IMAGENES via
// repo.Save.
func attachAplicarEvidencia(t *testing.T, v *domain.Venta, userID uuid.UUID) {
	t.Helper()
	imgID := uuid.New()
	storage, err := domain.NewImagenStorage(
		domain.StorageKindFilesystem,
		"ventas/"+v.ID().String()+"/"+imgID.String()+".jpg",
	)
	require.NoError(t, err)
	_, err = v.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:        imgID,
		Storage:   storage,
		Mime:      domain.MimeJPEG,
		SizeBytes: 8,
		By:        userID,
		Now:       time.Now().UTC(),
	})
	require.NoError(t, err)
}

// buildAplicarCredito builds a CREDITO venta in situación APROBADA.
func buildAplicarCredito(t *testing.T, userID uuid.UUID) *domain.Venta {
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
	nota := "venta a crédito prueba E2E"
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
		FechaVenta:  time.Now().UTC(),
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
		Now:       time.Now().UTC(),
	})
	require.NoError(t, err)
	attachAplicarEvidencia(t, v, userID)
	require.NoError(t, v.EnviarARevision(userID, time.Now().UTC()))
	require.NoError(t, v.Aprobar(userID, time.Now().UTC()))
	return v
}

// countDoctosBySis queries DOCTOS_ENTRE_SIS for docs linked to a DOCTO_PV
// by clave_sis_dest.
func countDoctosBySis(ctx context.Context, q firebird.Querier, doctoPVID int, clavesDest string) int {
	var n int
	row := q.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM DOCTOS_ENTRE_SIS
		WHERE CLAVE_SIS_FTE='PV' AND CLAVE_SIS_DEST=? AND DOCTO_FTE_ID=?`,
		clavesDest, doctoPVID,
	)
	_ = row.Scan(&n)
	return n
}

// readFoliosConsecutivo reads FOLIOS_CAJAS.CONSECUTIVO for the given caja.
func readFoliosConsecutivo(ctx context.Context, q firebird.Querier, cajaID int) int {
	var n int
	row := q.QueryRowContext(ctx, `SELECT CONSECUTIVO FROM FOLIOS_CAJAS WHERE CAJA_ID=? AND TIPO_DOCTO='V'`, cajaID)
	_ = row.Scan(&n)
	return n
}

// ─── E2E tests ────────────────────────────────────────────────────────────────

// TestE2E_AplicarVenta_Contado writes a CONTADO venta to Microsip's DOCTOS_PV
// family inside a rollback-only transaction and verifies:
//   - DOCTOS_PV row has APLICADO='S'
//   - DOCTOS_ENTRE_SIS generated 1 IN document (inventario)
//   - DOCTOS_ENTRE_SIS generated 2 CC documents (cargo + pago for CONTADO)
//   - FOLIOS_CAJAS incremented by 1
//
// Nothing persists — WithTestTransaction always rolls back.
func TestE2E_AplicarVenta_Contado(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		v := buildAplicarContado(t, userID)

		q := firebird.GetQuerier(ctx, pool.DB)
		// Read folio BEFORE aplicar.
		consAntes := readFoliosConsecutivo(ctx, q, testCajaID)

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
		assert.Positive(t, res.DoctoPVID, "DOCTO_PV_ID must be positive")
		assert.NotEmpty(t, res.Folio, "folio must not be empty")

		// Verify DOCTOS_PV.APLICADO='S'.
		var aplicado string
		err = q.QueryRowContext(ctx, `SELECT APLICADO FROM DOCTOS_PV WHERE DOCTO_PV_ID=?`, res.DoctoPVID).Scan(&aplicado)
		require.NoError(t, err)
		assert.Equal(t, "S", aplicado, "DOCTOS_PV.APLICADO must be 'S' after applying")

		// DOCTOS_ENTRE_SIS: 1 inventario doc.
		inDocs := countDoctosBySis(ctx, q, res.DoctoPVID, "IN")
		assert.Equal(t, 1, inDocs, "expected 1 DOCTOS_IN doc (CLAVE_SIS_DEST='IN')")

		// DOCTOS_ENTRE_SIS: 2 CxC docs for CONTADO (cargo + pago).
		ccDocs := countDoctosBySis(ctx, q, res.DoctoPVID, "CC")
		assert.Equal(t, 2, ccDocs, "expected 2 DOCTOS_CC docs for CONTADO (cargo + pago)")

		// FOLIOS_CAJAS incremented.
		consDespues := readFoliosConsecutivo(ctx, q, testCajaID)
		assert.Equal(t, consAntes+1, consDespues, "FOLIOS_CAJAS.CONSECUTIVO must increment by 1")

		t.Logf("contado: DoctoPVID=%d Folio=%s consAntes=%d consDespues=%d",
			res.DoctoPVID, res.Folio, consAntes, consDespues)
	})
}

// TestE2E_AplicarVenta_TresContado applies THREE CONTADO ventas in a single
// rollback transaction, resolving the caja/cajero/vendedor/defaults from the
// real MSP_CFG_* tables (not hard-coded), and verifies for each:
//   - DOCTOS_PV.APLICADO='S', 1 IN doc, 2 CC docs (cargo + pago)
//   - the per-article net/IVA split (article 378 is TASA 0% → IMPUESTOS=0)
//
// and across the three: the folios are distinct + sequential and FOLIOS_CAJAS
// advanced by exactly 3. Nothing persists (WithTestTransaction rolls back).
func TestE2E_AplicarVenta_TresContado(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)
	cfg := ventfb.NewAplicarConfigRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		q := firebird.GetQuerier(ctx, pool.DB)

		// Resolve mapping from the real config tables (FASE 2 + FASE 4 together).
		defs, err := cfg.Defaults(ctx)
		require.NoError(t, err)
		cc, err := cfg.CajaCajero(ctx, testZonaID)
		require.NoError(t, err)

		consInicial := readFoliosConsecutivo(ctx, q, cc.CajaID)

		folios := make([]string, 0, 3)
		for i := 1; i <= 3; i++ {
			v := buildAplicarContado(t, userID)
			in := outbound.MicrosipVentaInput{
				Venta:                v,
				CajaID:               cc.CajaID,
				CajeroID:             cc.CajeroID,
				VendedorID:           cc.VendedorID,
				SucursalID:           defs.SucursalID,
				FormaCobroID:         defs.FormaCobroContadoID,
				NumeroDeVendedoresID: numVendedores1ID,
			}

			res, err := writer.Aplicar(ctx, in)
			require.NoError(t, err, "venta %d should apply", i)

			var aplicado string
			require.NoError(t, q.QueryRowContext(ctx,
				`SELECT APLICADO FROM DOCTOS_PV WHERE DOCTO_PV_ID=?`, res.DoctoPVID).Scan(&aplicado))
			assert.Equal(t, "S", aplicado)
			assert.Equal(t, 1, countDoctosBySis(ctx, q, res.DoctoPVID, "IN"), "venta %d: 1 IN", i)
			assert.Equal(t, 2, countDoctosBySis(ctx, q, res.DoctoPVID, "CC"), "venta %d: 2 CC (cargo+pago)", i)

			var netoRaw, impuestosRaw any
			require.NoError(t, q.QueryRowContext(ctx,
				`SELECT IMPORTE_NETO, TOTAL_IMPUESTOS FROM DOCTOS_PV WHERE DOCTO_PV_ID=?`,
				res.DoctoPVID).Scan(&netoRaw, &impuestosRaw))
			neto, err := firebird.ScanDecimal(netoRaw, 2)
			require.NoError(t, err)
			impuestos, err := firebird.ScanDecimal(impuestosRaw, 2)
			require.NoError(t, err)

			folios = append(folios, res.Folio)
			t.Logf("venta contado #%d → DoctoPVID=%d Folio=%s IMPORTE_NETO=%s TOTAL_IMPUESTOS=%s (IN=1 CC=2)",
				i, res.DoctoPVID, res.Folio, neto.StringFixed(2), impuestos.StringFixed(2))
		}

		// Folios distinct + FOLIOS_CAJAS advanced by exactly 3.
		assert.Len(t, folios, 3)
		assert.NotEqual(t, folios[0], folios[1])
		assert.NotEqual(t, folios[1], folios[2])
		consFinal := readFoliosConsecutivo(ctx, q, cc.CajaID)
		assert.Equal(t, consInicial+3, consFinal, "FOLIOS_CAJAS must advance by 3")
		t.Logf("3 ventas contado aplicadas: folios=%v  FOLIOS_CAJAS %d→%d (rollback, no persiste)",
			folios, consInicial, consFinal)
	})
}

// TestE2E_AplicarVenta_Contado_16PctIVA applies a CONTADO venta using an
// article that carries 16% IVA (article 510) and verifies that the adapter
// computes the net/IVA split correctly per-article (not a flat assumption).
// For a CONTADO sale the unit price comes from MontoSnapshot.Contado() = 3500;
// at 16% IVA the split is neto = 3500/1.16 ≈ 3017.24 and IVA ≈ 482.76.
func TestE2E_AplicarVenta_Contado_16PctIVA(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)
	cfg := ventfb.NewAplicarConfigRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		q := firebird.GetQuerier(ctx, pool.DB)

		defs, err := cfg.Defaults(ctx)
		require.NoError(t, err)
		cc, err := cfg.CajaCajero(ctx, testZonaID)
		require.NoError(t, err)

		v := buildAplicarContadoConArticulo(t, userID, testArticuloID16Pct, "Batería Cinsa 50 Aniv")
		in := outbound.MicrosipVentaInput{
			Venta:                v,
			CajaID:               cc.CajaID,
			CajeroID:             cc.CajeroID,
			VendedorID:           cc.VendedorID,
			SucursalID:           defs.SucursalID,
			FormaCobroID:         defs.FormaCobroContadoID,
			NumeroDeVendedoresID: numVendedores1ID,
		}

		res, err := writer.Aplicar(ctx, in)
		require.NoError(t, err)

		// Verify the per-article IVA split: precio = 4320 (con IVA 16%).
		var netoRaw, impuestosRaw any
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT IMPORTE_NETO, TOTAL_IMPUESTOS FROM DOCTOS_PV WHERE DOCTO_PV_ID=?`,
			res.DoctoPVID).Scan(&netoRaw, &impuestosRaw))
		neto, err := firebird.ScanDecimal(netoRaw, 2)
		require.NoError(t, err)
		impuestos, err := firebird.ScanDecimal(impuestosRaw, 2)
		require.NoError(t, err)

		// CONTADO unit price = MontoSnapshot.Contado() = 3500.
		// At 16% IVA: neto = 3500/1.16 ≈ 3017.24 ; impuesto ≈ 482.76.
		assert.InDelta(t, 3017.24, neto.InexactFloat64(), 0.01,
			"IMPORTE_NETO must equal contado_price/1.16 for a 16%% article")
		assert.InDelta(t, 482.76, impuestos.InexactFloat64(), 0.01,
			"TOTAL_IMPUESTOS must equal contado_price - neto for a 16%% article")

		// Cascade still correct.
		assert.Equal(t, 1, countDoctosBySis(ctx, q, res.DoctoPVID, "IN"))
		assert.Equal(t, 2, countDoctosBySis(ctx, q, res.DoctoPVID, "CC"))

		t.Logf("contado 16%% IVA: DoctoPVID=%d Folio=%s precio_contado=3500.00 IMPORTE_NETO=%s TOTAL_IMPUESTOS=%s",
			res.DoctoPVID, res.Folio, neto.StringFixed(2), impuestos.StringFixed(2))
	})
}

// TestE2E_AplicarVenta_Credito writes a CREDITO venta to DOCTOS_PV and verifies:
//   - DOCTOS_PV.APLICADO='S'
//   - 1 IN doc (inventario)
//   - 1 CC doc (only cargo — no pago for CREDITO)
//   - LIBRES_CARGOS_CC row exists for the cargo
//   - Since enganche=500 > 0, one additional DOCTOS_CC enganche doc exists
//   - FOLIOS_CAJAS incremented
//
// Everything rolls back.
func TestE2E_AplicarVenta_Credito(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		v := buildAplicarCredito(t, userID)

		q := firebird.GetQuerier(ctx, pool.DB)
		consAntes := readFoliosConsecutivo(ctx, q, testCajaID)

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
		assert.NotEmpty(t, res.Folio)

		// APLICADO='S'.
		var aplicado string
		err = q.QueryRowContext(ctx, `SELECT APLICADO FROM DOCTOS_PV WHERE DOCTO_PV_ID=?`, res.DoctoPVID).Scan(&aplicado)
		require.NoError(t, err)
		assert.Equal(t, "S", aplicado)

		// 1 IN document.
		assert.Equal(t, 1, countDoctosBySis(ctx, q, res.DoctoPVID, "IN"),
			"expected 1 DOCTOS_IN doc")

		// 1 CC document (cargo only; no pago for CREDITO).
		assert.Equal(t, 1, countDoctosBySis(ctx, q, res.DoctoPVID, "CC"),
			"expected 1 DOCTOS_CC doc (cargo) for CREDITO")

		// LIBRES_CARGOS_CC must exist for the cargo.
		var cargoCCID int
		err = q.QueryRowContext(ctx, `
			SELECT D.DOCTO_CC_ID
			FROM DOCTOS_ENTRE_SIS E
			JOIN DOCTOS_CC D ON D.DOCTO_CC_ID=E.DOCTO_DEST_ID
			WHERE E.CLAVE_SIS_FTE='PV' AND E.CLAVE_SIS_DEST='CC' AND E.DOCTO_FTE_ID=?`,
			res.DoctoPVID,
		).Scan(&cargoCCID)
		require.NoError(t, err, "cargo CC doc must exist via DOCTOS_ENTRE_SIS")

		var libresRows int
		err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM LIBRES_CARGOS_CC WHERE DOCTO_CC_ID=?`, cargoCCID).Scan(&libresRows)
		require.NoError(t, err)
		assert.Equal(t, 1, libresRows, "LIBRES_CARGOS_CC must have 1 row for the cargo")

		// Enganche > 0 → one additional DOCTOS_CC (enganche abono).
		// The enganche doc is NOT linked via DOCTOS_ENTRE_SIS with CLAVE_SIS_FTE='PV';
		// it is linked via IMPORTES_DOCTOS_CC.DOCTO_CC_ACR_ID pointing to the cargo.
		var engancheDocCount int
		err = q.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM IMPORTES_DOCTOS_CC
			WHERE DOCTO_CC_ACR_ID=? AND TIPO_IMPTE='R'`,
			cargoCCID,
		).Scan(&engancheDocCount)
		require.NoError(t, err)
		assert.Equal(t, 1, engancheDocCount, "IMPORTES_DOCTOS_CC must have 1 enganche importe row")

		// FOLIOS_CAJAS incremented.
		consDespues := readFoliosConsecutivo(ctx, q, testCajaID)
		assert.Equal(t, consAntes+1, consDespues, "FOLIOS_CAJAS must increment by 1")

		t.Logf("credito: DoctoPVID=%d Folio=%s cargoCCID=%d consAntes=%d consDespues=%d",
			res.DoctoPVID, res.Folio, cargoCCID, consAntes, consDespues)
	})
}

// TestE2E_AplicarVenta_Credito_CamposNuevos verifies the five-correction write
// path for a CREDITO venta:
//   - LIBRES_CARGOS_CC.TIEMPO_A_CORTO_PLAZOMESES = configured 4
//   - LIBRES_CARGOS_CC.MONTO_A_CORTO_PLAZO       = venta corto-plazo monto
//   - LIBRES_CARGOS_CC.VENDEDOR_1/2/3            = the mapped LISTA_ATRIB_ID per slot
//   - LIBRES_CARGOS_CC.NUMERO_DE_VENDEDORES      = the resolved list id
//   - FORMAS_COBRO_DOCTOS for the enganche       = configured FORMA_COBRO_ID 157
//
// Everything rolls back.
func TestE2E_AplicarVenta_Credito_CamposNuevos(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	// Mirror production wiring: tiempo corto plazo = 4, forma cobro enganche = 157.
	writer := microsip.NewVentaWriter(pool).
		WithTiempoCortoPlazoMeses(4).
		WithFormaCobroEnganche(157)

	// Sentinel LISTA_ATRIB_IDs for the single vendedor slot. These are the
	// resolved values the app layer would pass; the writer binds them verbatim.
	const vendedorListaID1 = 19985001

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		v := buildAplicarCredito(t, userID) // 1 vendedor, monto corto plazo 8200.00

		q := firebird.GetQuerier(ctx, pool.DB)

		fpID := formaDePagoSemanalID
		cmID := creditoEnMeses12ID
		in := outbound.MicrosipVentaInput{
			Venta:                v,
			CajaID:               testCajaID,
			CajeroID:             testCajeroID,
			VendedorID:           testVendedorID,
			VendedorListaIDs:     [3]int{vendedorListaID1, -1, -1},
			SucursalID:           testSucursalID,
			FormaCobroID:         formaCobroCreditoID,
			FormaDePagoID:        &fpID,
			CreditoEnMesesID:     &cmID,
			NumeroDeVendedoresID: numVendedores1ID,
		}

		res, err := writer.Aplicar(ctx, in)
		require.NoError(t, err)

		// Resolve the cargo CC doc generated by the cascade.
		var cargoCCID int
		err = q.QueryRowContext(ctx, `
			SELECT D.DOCTO_CC_ID
			FROM DOCTOS_ENTRE_SIS E
			JOIN DOCTOS_CC D ON D.DOCTO_CC_ID=E.DOCTO_DEST_ID
			WHERE E.CLAVE_SIS_FTE='PV' AND E.CLAVE_SIS_DEST='CC' AND E.DOCTO_FTE_ID=?`,
			res.DoctoPVID,
		).Scan(&cargoCCID)
		require.NoError(t, err, "cargo CC doc must exist via DOCTOS_ENTRE_SIS")

		// LIBRES_CARGOS_CC: new columns populated.
		var tiempoCorto, vend1, vend2, vend3, numVend int
		var montoCorto string
		err = q.QueryRowContext(ctx, `
			SELECT TIEMPO_A_CORTO_PLAZOMESES, MONTO_A_CORTO_PLAZO,
			       VENDEDOR_1, VENDEDOR_2, VENDEDOR_3, NUMERO_DE_VENDEDORES
			FROM LIBRES_CARGOS_CC WHERE DOCTO_CC_ID=?`,
			cargoCCID,
		).Scan(&tiempoCorto, &montoCorto, &vend1, &vend2, &vend3, &numVend)
		require.NoError(t, err)
		assert.Equal(t, 4, tiempoCorto, "TIEMPO_A_CORTO_PLAZOMESES")
		assert.Equal(t, "8200", montoCorto, "MONTO_A_CORTO_PLAZO (entero, sin decimales)")
		assert.Equal(t, vendedorListaID1, vend1, "VENDEDOR_1 = LISTA_ATRIB_ID slot 1")
		assert.Equal(t, -1, vend2, "VENDEDOR_2 = -1 (sin segundo vendedor)")
		assert.Equal(t, -1, vend3, "VENDEDOR_3 = -1 (sin tercer vendedor)")
		assert.Equal(t, numVendedores1ID, numVend, "NUMERO_DE_VENDEDORES")

		// Enganche docto.
		var engancheDoctoID int
		err = q.QueryRowContext(ctx, `
			SELECT DOCTO_CC_ID FROM IMPORTES_DOCTOS_CC
			WHERE DOCTO_CC_ACR_ID=? AND TIPO_IMPTE='R'`,
			cargoCCID,
		).Scan(&engancheDoctoID)
		require.NoError(t, err, "enganche docto must exist")

		// FORMAS_COBRO_DOCTOS for the enganche with FORMA_COBRO_ID=157.
		var formaCobroID int
		err = q.QueryRowContext(ctx, `
			SELECT FORMA_COBRO_ID FROM FORMAS_COBRO_DOCTOS
			WHERE NOM_TABLA_DOCTOS='DOCTOS_CC' AND DOCTO_ID=?`,
			engancheDoctoID,
		).Scan(&formaCobroID)
		require.NoError(t, err, "FORMAS_COBRO_DOCTOS row must exist for the enganche")
		assert.Equal(t, 157, formaCobroID, "FORMA_COBRO_ID del enganche")
	})
}
