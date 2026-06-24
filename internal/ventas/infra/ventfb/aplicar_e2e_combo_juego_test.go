//nolint:misspell,paralleltest // Spanish vocabulary; integration tests share pool and must not run in parallel.
package ventfb_test

// aplicar_e2e_combo_juego_test.go exercises Task 4: the Microsip VentaWriter
// emitting ROL='J' parent lines per combo plus the camino-B ROL='C' component
// lines (linked via SUB_MOVTOS_PV) so the APLICADO='S' flip discharges the
// components' inventory.
//
// These tests drive the writer DIRECTLY with a hand-built MicrosipVentaInput
// (populating JuegosPorCombo) instead of the app.Service, because the resolver
// feature is not wired into the test harness. The juego itself is created or
// matched through the real JuegoResolver (Task 2) so the ARTICULO_ID passed in
// JuegosPorCombo is a genuine Microsip juego whose JUEGOS_DET recipe matches.
//
// All writes run inside WithTestTransaction (rollback-only). Gating: tests skip
// when FB_DATABASE is unset (requireFBEnv) or when the dev DB lacks the minimum
// component seed (discoverDosComponentes skips with a clear reason).

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

// comboLineaArticuloID is a LINEA_ARTICULO_ID known to exist in the dev DB
// (same value used by the JuegoResolver spike). Passed to the resolver when it
// must create a new juego.
const comboLineaArticuloID = 11774

// ─── discovery ────────────────────────────────────────────────────────────────

// componenteSeed is one discovered, well-stocked, almacenable component article
// usable as a juego component.
type componenteSeed struct {
	articuloID int
	almacenID  int
}

// discoverDosComponentes finds two DISTINCT almacenable, active, non-juego
// articles in the SAME almacén, each with a clave (ROL=17) and ample on-hand
// existencia, so the combo's components discharge real stock from one almacén.
// Skips the calling test when the dev DB lacks the minimum seed.
func discoverDosComponentes(ctx context.Context, t *testing.T, q firebird.Querier) (componenteSeed, componenteSeed) {
	t.Helper()
	const minExistencia = 100

	// Find an almacén with at least two qualifying components.
	var almacenID int
	if err := q.QueryRowContext(ctx, `
		SELECT FIRST 1 s.ALMACEN_ID
		FROM (
			SELECT s.ARTICULO_ID, s.ALMACEN_ID
			FROM SALDOS_IN s
			JOIN ARTICULOS a ON a.ARTICULO_ID = s.ARTICULO_ID
			WHERE a.ES_ALMACENABLE = 'S' AND a.ESTATUS = 'A' AND a.ES_JUEGO <> 'S'
			  AND EXISTS (SELECT 1 FROM CLAVES_ARTICULOS c
			               WHERE c.ARTICULO_ID = s.ARTICULO_ID
			                 AND c.ROL_CLAVE_ART_ID = 17)
			GROUP BY s.ARTICULO_ID, s.ALMACEN_ID
			HAVING SUM(s.ENTRADAS_UNIDADES - s.SALIDAS_UNIDADES) >= ?
		) s
		GROUP BY s.ALMACEN_ID
		HAVING COUNT(*) >= 2
		ORDER BY COUNT(*) DESC`,
		minExistencia,
	).Scan(&almacenID); err != nil {
		t.Skip("no almacén with >=2 well-stocked almacenable components (clave rol=17, existencia >= 100) in dev DB — skipping combo juego e2e tests")
	}

	rows, err := q.QueryContext(ctx, `
		SELECT FIRST 2 s.ARTICULO_ID
		FROM SALDOS_IN s
		JOIN ARTICULOS a ON a.ARTICULO_ID = s.ARTICULO_ID
		WHERE a.ES_ALMACENABLE = 'S' AND a.ESTATUS = 'A' AND a.ES_JUEGO <> 'S'
		  AND s.ALMACEN_ID = ?
		  AND EXISTS (SELECT 1 FROM CLAVES_ARTICULOS c
		               WHERE c.ARTICULO_ID = s.ARTICULO_ID
		                 AND c.ROL_CLAVE_ART_ID = 17)
		GROUP BY s.ARTICULO_ID
		HAVING SUM(s.ENTRADAS_UNIDADES - s.SALIDAS_UNIDADES) >= ?
		ORDER BY s.ARTICULO_ID`,
		almacenID, minExistencia,
	)
	require.NoError(t, err, "query componentes")
	defer rows.Close() //nolint:errcheck // best-effort after iteration

	var ids []int
	for rows.Next() {
		var id int
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	require.Len(t, ids, 2, "expected exactly 2 component articles in almacén %d", almacenID)

	return componenteSeed{articuloID: ids[0], almacenID: almacenID},
		componenteSeed{articuloID: ids[1], almacenID: almacenID}
}

// distintoAlmacen returns an almacén id different from the given one, so combo /
// producto almacén-origen and almacén-destino never collide (the domain forbids
// equal warehouses). The writer discharges inventory from the ORIGEN, so the
// destino value here is only a structural placeholder.
func distintoAlmacen(origen int) int {
	if origen == 11058 {
		return 11059
	}
	return 11058
}

// ─── combo venta builder ──────────────────────────────────────────────────────

// comboVentaSpec describes a combo to build into a venta: its components (with
// per-juego unidades), the combo quantity, prices, and origin almacén.
type comboVentaSpec struct {
	comboID       uuid.UUID
	componentes   []domain.RecetaComponente // articuloID + per-juego unidades
	comboCantidad decimal.Decimal
	comboPrecios  domain.MontoSnapshot
	almacenOrigen int
	tipoVenta     domain.TipoVenta
	plan          *domain.PlanCredito
}

// buildComboVenta builds an APROBADA venta with one combo and its child
// productos. Each child producto's Cantidad is the per-juego unidades (the
// domain's RecetaDeCombo reads p.Cantidad() as the recipe quantity), so the
// writer's ROL='C' line gets UNIDADES = comboCantidad × per-juego unidades.
func buildComboVenta(t *testing.T, userID uuid.UUID, spec comboVentaSpec) *domain.Venta {
	t.Helper()
	nombre, err := domain.NewNombreCliente("Ramírez Flores Beatriz")
	require.NoError(t, err)
	cid := testClienteID
	zona := testZonaID
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle:         "Av. Hidalgo",
		Colonia:       "Centro",
		Poblacion:     "Tehuacán",
		Ciudad:        "Puebla",
		ZonaClienteID: &zona,
	})
	require.NoError(t, err)
	gps, err := domain.NewGPSCoords(18.46, -97.40)
	require.NoError(t, err)

	childPrecios, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("100.00"),
		decimal.RequireFromString("90.00"),
		decimal.RequireFromString("80.00"),
	)
	require.NoError(t, err)

	productos := make([]domain.CrearVentaProductoInput, 0, len(spec.componentes))
	for _, comp := range spec.componentes {
		productos = append(productos, domain.CrearVentaProductoInput{
			ID:         uuid.New(),
			ArticuloID: comp.ArticuloID(),
			Articulo:   "Componente Combo",
			Cantidad:   comp.Unidades(),
			Precios:    childPrecios,
			ComboID:    &spec.comboID,
			// Combo children inherit almacén from the parent combo.
		})
	}

	params := domain.CrearVentaParams{
		ID:        uuid.New(),
		ClienteID: &cid,
		Cliente: domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{
			Nombre: nombre,
		}),
		Direccion:  dir,
		GPS:        gps,
		FechaVenta: time.Now().UTC(),
		TipoVenta:  spec.tipoVenta,
		Combos: []domain.CrearVentaComboInput{{
			ID:             spec.comboID,
			Nombre:         "Combo Recámara E2E",
			Precios:        spec.comboPrecios,
			Cantidad:       spec.comboCantidad,
			AlmacenOrigen:  spec.almacenOrigen,
			AlmacenDestino: distintoAlmacen(spec.almacenOrigen),
		}},
		Productos: productos,
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID:        uuid.New(),
			UsuarioID: userID,
			Email:     "ruta25@muebleriamsp.mx",
			Nombre:    "Vendedor Ruta 25",
		}},
		CreatedBy: userID,
		Now:       time.Now().UTC(),
	}
	if spec.tipoVenta == domain.TipoVentaCredito {
		params.PlanCredito = spec.plan
		dc, dcErr := domain.NewDiaCobranzaSemana(domain.DiaSemanaLunes)
		require.NoError(t, dcErr)
		params.DiaCobranza = &dc
	}

	v, err := domain.CrearVenta(params)
	require.NoError(t, err)
	attachAplicarEvidencia(t, v, userID)
	require.NoError(t, v.EnviarARevision(userID, time.Now().UTC()))
	require.NoError(t, v.Aprobar(userID, time.Now().UTC()))
	return v
}

// ─── DET-line read helpers ─────────────────────────────────────────────────────

type detLinea struct {
	articuloID int
	rol        string
	unidades   decimal.Decimal
	precioImp  decimal.Decimal
}

// readDetLineas reads every DOCTOS_PV_DET line for a DOCTO_PV, ordered by id.
func readDetLineas(ctx context.Context, t *testing.T, q firebird.Querier, doctoPVID int) []detLinea {
	t.Helper()
	rows, err := q.QueryContext(ctx, `
		SELECT ARTICULO_ID, ROL,
		       CAST(UNIDADES AS NUMERIC(18,6)),
		       CAST(PRECIO_UNITARIO_IMPTO AS NUMERIC(18,6))
		FROM DOCTOS_PV_DET
		WHERE DOCTO_PV_ID = ?
		ORDER BY DOCTO_PV_DET_ID`,
		doctoPVID,
	)
	require.NoError(t, err, "query DOCTOS_PV_DET")
	defer rows.Close() //nolint:errcheck // best-effort after iteration

	var out []detLinea
	for rows.Next() {
		var l detLinea
		var rawU, rawP any
		require.NoError(t, rows.Scan(&l.articuloID, &l.rol, &rawU, &rawP))
		l.unidades, err = firebird.ScanDecimal(rawU, 6)
		require.NoError(t, err)
		l.precioImp, err = firebird.ScanDecimal(rawP, 6)
		require.NoError(t, err)
		out = append(out, l)
	}
	require.NoError(t, rows.Err())
	return out
}

// filterByRol returns the subset of lines with the given ROL.
func filterByRol(lineas []detLinea, rol string) []detLinea {
	var out []detLinea
	for _, l := range lineas {
		if l.rol == rol {
			out = append(out, l)
		}
	}
	return out
}

// findLineaByArticulo returns the first line for an articuloID (and whether found).
func findLineaByArticulo(lineas []detLinea, articuloID int) (detLinea, bool) {
	for _, l := range lineas {
		if l.articuloID == articuloID {
			return l, true
		}
	}
	return detLinea{}, false
}

// resolveJuego runs the real JuegoResolver to match-or-create the juego for the
// combo's derived recipe (mirroring app.resolveJuegosPorCombo) and returns its
// ARTICULO_ID plus whether it was created.
func resolveJuego(ctx context.Context, t *testing.T, pool *firebird.Pool, v *domain.Venta, comboID uuid.UUID) (int, bool) {
	t.Helper()
	receta, err := v.RecetaDeCombo(comboID)
	require.NoError(t, err, "derive receta from combo")
	res, err := microsip.NewJuegoResolver(pool).Resolve(ctx, outbound.MicrosipJuegoInput{
		Receta:          receta,
		NombrePropuesto: "JUEGO E2E " + uuid.NewString(),
		LineaArticuloID: comboLineaArticuloID,
	})
	require.NoError(t, err, "resolver juego")
	require.Positive(t, res.ArticuloID)
	return res.ArticuloID, res.Creado
}

// ─── Test 1: 1 combo, juego pre-seeded (match) — inventory discharge ─────────

// TestE2E_AplicarComboJuego_MatchDescargaInventario seeds a juego via the
// resolver, then runs the SAME recipe again (match path), writes a CREDITO
// combo venta, and verifies after APLICADO='S':
//   - exactly 1 ROL='J' line for the juego, UNIDADES = comboCantidad
//   - one ROL='C' line per component, UNIDADES = comboCantidad × receta.unidades
//   - each component's SALDOS_IN.SALIDAS_UNIDADES increased by that amount
//   - the DOCTOS_CC cargo (crédito) was generated.
func TestE2E_AplicarComboJuego_MatchDescargaInventario(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)
		userID := seedUsuarioRow(ctx, t, pool)

		c1, c2 := discoverDosComponentes(ctx, t, q)
		almacen := c1.almacenID

		// Recipe: c1 × 2 per juego, c2 × 1 per juego.
		comp1, err := domain.NewRecetaComponente(c1.articuloID, decimal.RequireFromString("2.00"))
		require.NoError(t, err)
		comp2, err := domain.NewRecetaComponente(c2.articuloID, decimal.RequireFromString("1.00"))
		require.NoError(t, err)
		componentes := []domain.RecetaComponente{comp1, comp2}

		// Build a CREDITO combo venta (combo qty = 3).
		comboQty := decimal.RequireFromString("3.00")
		comboPrecios, err := domain.NewMontoSnapshot(
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

		comboID := uuid.New()
		v := buildComboVenta(t, userID, comboVentaSpec{
			comboID:       comboID,
			componentes:   componentes,
			comboCantidad: comboQty,
			comboPrecios:  comboPrecios,
			almacenOrigen: almacen,
			tipoVenta:     domain.TipoVentaCredito,
			plan:          &plan,
		})

		// Pre-seed the juego (create), then resolve again to exercise the match.
		seedID, created := resolveJuego(ctx, t, pool, v, comboID)
		require.True(t, created, "first resolve must create the juego")
		juegoID, createdAgain := resolveJuego(ctx, t, pool, v, comboID)
		require.False(t, createdAgain, "second resolve must MATCH the seeded juego")
		require.Equal(t, seedID, juegoID, "match must return the same juego")

		// Baselines for both components (after juego creation, before aplicar).
		beforeC1 := snapshotSalidasInventario(ctx, t, q, c1.articuloID, almacen)
		beforeC2 := snapshotSalidasInventario(ctx, t, q, c2.articuloID, almacen)

		fpID := formaDePagoSemanalID
		cmID := creditoEnMeses12ID
		res, err := writer.Aplicar(ctx, outbound.MicrosipVentaInput{
			Venta:                v,
			CajaID:               testCajaID,
			CajeroID:             testCajeroID,
			VendedorID:           testVendedorID,
			SucursalID:           testSucursalID,
			FormaCobroID:         formaCobroCreditoID,
			FormaDePagoID:        &fpID,
			CreditoEnMesesID:     &cmID,
			NumeroDeVendedoresID: numVendedores1ID,
			JuegosPorCombo:       map[uuid.UUID]int{comboID: juegoID},
		})
		require.NoError(t, err)

		lineas := readDetLineas(ctx, t, q, res.DoctoPVID)

		// Exactly 1 ROL='J' line for the juego.
		jLineas := filterByRol(lineas, "J")
		require.Len(t, jLineas, 1, "expected exactly 1 ROL='J' line; got lines=%v", lineas)
		assert.Equal(t, juegoID, jLineas[0].articuloID, "ROL='J' must be the resolved juego")
		assert.True(t, comboQty.Equal(jLineas[0].unidades),
			"ROL='J' UNIDADES must equal comboCantidad=%s; got=%s", comboQty.StringFixed(6), jLineas[0].unidades.StringFixed(6))

		// One ROL='C' line per component with UNIDADES = comboQty × receta.unidades.
		cLineas := filterByRol(lineas, "C")
		require.Len(t, cLineas, 2, "expected 2 ROL='C' component lines; got=%v", cLineas)

		l1, ok := findLineaByArticulo(cLineas, c1.articuloID)
		require.True(t, ok, "ROL='C' for component %d must exist", c1.articuloID)
		wantU1 := comboQty.Mul(comp1.Unidades())
		assert.True(t, wantU1.Equal(l1.unidades),
			"componente %d ROL='C' UNIDADES want=%s got=%s", c1.articuloID, wantU1.StringFixed(6), l1.unidades.StringFixed(6))
		assert.True(t, l1.precioImp.IsZero(), "ROL='C' precio must be 0; got=%s", l1.precioImp.StringFixed(6))

		l2, ok := findLineaByArticulo(cLineas, c2.articuloID)
		require.True(t, ok, "ROL='C' for component %d must exist", c2.articuloID)
		wantU2 := comboQty.Mul(comp2.Unidades())
		assert.True(t, wantU2.Equal(l2.unidades),
			"componente %d ROL='C' UNIDADES want=%s got=%s", c2.articuloID, wantU2.StringFixed(6), l2.unidades.StringFixed(6))

		// Inventory discharge: each component's SALIDAS_UNIDADES rose by its qty.
		assertSalidasInventarioDelta(ctx, t, q, c1.articuloID, almacen, beforeC1, wantU1)
		assertSalidasInventarioDelta(ctx, t, q, c2.articuloID, almacen, beforeC2, wantU2)

		// CREDITO cargo generated.
		assert.Equal(t, 1, countDoctosBySis(ctx, q, res.DoctoPVID, "CC"),
			"expected 1 DOCTOS_CC (cargo) for crédito")
		assert.Equal(t, 1, countDoctosBySis(ctx, q, res.DoctoPVID, "IN"),
			"expected 1 DOCTOS_IN (inventario) generated by the ROL='C' cascade")

		t.Logf("combo juego match+discharge: DoctoPVID=%d juego=%d c1=%d(+%s) c2=%d(+%s)",
			res.DoctoPVID, juegoID,
			c1.articuloID, wantU1.StringFixed(4),
			c2.articuloID, wantU2.StringFixed(4))
	})
}

// ─── Test 2: 1 combo, juego does NOT exist (create) ───────────────────────────

// TestE2E_AplicarComboJuego_CreaJuego uses a unique recipe so the resolver must
// CREATE the juego, then writes the venta with ROL='J'/ROL='C'.
func TestE2E_AplicarComboJuego_CreaJuego(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)
		userID := seedUsuarioRow(ctx, t, pool)

		c1, c2 := discoverDosComponentes(ctx, t, q)
		almacen := c1.almacenID

		// Unique fractional unidades make a recipe unlikely to pre-exist → create.
		comp1, err := domain.NewRecetaComponente(c1.articuloID, decimal.RequireFromString("1.50"))
		require.NoError(t, err)
		comp2, err := domain.NewRecetaComponente(c2.articuloID, decimal.RequireFromString("2.50"))
		require.NoError(t, err)
		componentes := []domain.RecetaComponente{comp1, comp2}

		comboQty := decimal.RequireFromString("1.00")
		comboPrecios, err := domain.NewMontoSnapshot(
			decimal.RequireFromString("5000.00"),
			decimal.RequireFromString("4500.00"),
			decimal.RequireFromString("4000.00"),
		)
		require.NoError(t, err)

		comboID := uuid.New()
		v := buildComboVenta(t, userID, comboVentaSpec{
			comboID:       comboID,
			componentes:   componentes,
			comboCantidad: comboQty,
			comboPrecios:  comboPrecios,
			almacenOrigen: almacen,
			tipoVenta:     domain.TipoVentaContado,
		})

		juegoID, created := resolveJuego(ctx, t, pool, v, comboID)
		require.True(t, created, "unique recipe must create a new juego")

		res, err := writer.Aplicar(ctx, outbound.MicrosipVentaInput{
			Venta:                v,
			CajaID:               testCajaID,
			CajeroID:             testCajeroID,
			VendedorID:           testVendedorID,
			SucursalID:           testSucursalID,
			FormaCobroID:         formaCobroContadoID,
			NumeroDeVendedoresID: numVendedores1ID,
			JuegosPorCombo:       map[uuid.UUID]int{comboID: juegoID},
		})
		require.NoError(t, err)

		lineas := readDetLineas(ctx, t, q, res.DoctoPVID)
		jLineas := filterByRol(lineas, "J")
		require.Len(t, jLineas, 1)
		assert.Equal(t, juegoID, jLineas[0].articuloID)
		require.Len(t, filterByRol(lineas, "C"), 2, "expected 2 ROL='C' lines")

		t.Logf("combo juego create: DoctoPVID=%d nuevoJuego=%d", res.DoctoPVID, juegoID)
	})
}

// ─── Test 3: mixed — standalone producto (ROL='N') + combo (ROL='J'+'C') ─────

// TestE2E_AplicarComboJuego_Mixto verifies a venta with one standalone producto
// AND one combo: the standalone discharges as ROL='N', the combo as ROL='J' +
// ROL='C'. Both inventories move correctly.
func TestE2E_AplicarComboJuego_Mixto(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)
		userID := seedUsuarioRow(ctx, t, pool)

		c1, c2 := discoverDosComponentes(ctx, t, q)
		almacen := c1.almacenID

		// Combo recipe: only c1 (×1 per juego). Standalone line: c2.
		comp1, err := domain.NewRecetaComponente(c1.articuloID, decimal.RequireFromString("1.00"))
		require.NoError(t, err)

		beforeC1 := snapshotSalidasInventario(ctx, t, q, c1.articuloID, almacen)
		beforeC2 := snapshotSalidasInventario(ctx, t, q, c2.articuloID, almacen)

		comboQty := decimal.RequireFromString("2.00")
		comboPrecios, err := domain.NewMontoSnapshot(
			decimal.RequireFromString("3000.00"),
			decimal.RequireFromString("2700.00"),
			decimal.RequireFromString("2400.00"),
		)
		require.NoError(t, err)
		standalonePrecios, err := domain.NewMontoSnapshot(
			decimal.RequireFromString("1200.00"),
			decimal.RequireFromString("1100.00"),
			decimal.RequireFromString("1000.00"),
		)
		require.NoError(t, err)

		// Build base combo venta, then append a standalone producto manually via
		// the same CrearVenta call (combo + standalone together).
		nombre, err := domain.NewNombreCliente("Torres Mendoza Lucía")
		require.NoError(t, err)
		cid := testClienteID
		zona := testZonaID
		dir, err := domain.NewDireccion(domain.NewDireccionParams{
			Calle: "Av. Reforma", Colonia: "Centro", Poblacion: "Tehuacán", Ciudad: "Puebla", ZonaClienteID: &zona,
		})
		require.NoError(t, err)
		gps, err := domain.NewGPSCoords(18.46, -97.41)
		require.NoError(t, err)
		childPrecios, err := domain.NewMontoSnapshot(
			decimal.RequireFromString("100.00"),
			decimal.RequireFromString("90.00"),
			decimal.RequireFromString("80.00"),
		)
		require.NoError(t, err)

		comboID := uuid.New()
		standaloneQty := decimal.RequireFromString("4.00")
		almOrigen := almacen
		almDestino := distintoAlmacen(almacen)
		v, err := domain.CrearVenta(domain.CrearVentaParams{
			ID:        uuid.New(),
			ClienteID: &cid,
			Cliente:   domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nombre}),
			Direccion: dir, GPS: gps,
			FechaVenta: time.Now().UTC(),
			TipoVenta:  domain.TipoVentaContado,
			Combos: []domain.CrearVentaComboInput{{
				ID: comboID, Nombre: "Combo Mixto E2E", Precios: comboPrecios,
				Cantidad: comboQty, AlmacenOrigen: almacen, AlmacenDestino: almDestino,
			}},
			Productos: []domain.CrearVentaProductoInput{
				{ // combo child (recipe component c1)
					ID: uuid.New(), ArticuloID: c1.articuloID, Articulo: "Componente Combo",
					Cantidad: comp1.Unidades(), Precios: childPrecios, ComboID: &comboID,
				},
				{ // standalone producto c2
					ID: uuid.New(), ArticuloID: c2.articuloID, Articulo: "Producto Suelto",
					Cantidad: standaloneQty, Precios: standalonePrecios,
					AlmacenOrigen: &almOrigen, AlmacenDestino: &almDestino,
				},
			},
			Vendedores: []domain.CrearVentaVendedorInput{{
				ID: uuid.New(), UsuarioID: userID, Email: "ruta25@muebleriamsp.mx", Nombre: "Vendedor Ruta 25",
			}},
			CreatedBy: userID, Now: time.Now().UTC(),
		})
		require.NoError(t, err)
		attachAplicarEvidencia(t, v, userID)
		require.NoError(t, v.EnviarARevision(userID, time.Now().UTC()))
		require.NoError(t, v.Aprobar(userID, time.Now().UTC()))

		juegoID, _ := resolveJuego(ctx, t, pool, v, comboID)

		res, err := writer.Aplicar(ctx, outbound.MicrosipVentaInput{
			Venta:                v,
			CajaID:               testCajaID,
			CajeroID:             testCajeroID,
			VendedorID:           testVendedorID,
			SucursalID:           testSucursalID,
			FormaCobroID:         formaCobroContadoID,
			NumeroDeVendedoresID: numVendedores1ID,
			JuegosPorCombo:       map[uuid.UUID]int{comboID: juegoID},
		})
		require.NoError(t, err)

		lineas := readDetLineas(ctx, t, q, res.DoctoPVID)
		require.Len(t, filterByRol(lineas, "J"), 1, "1 ROL='J' for combo")
		require.Len(t, filterByRol(lineas, "C"), 1, "1 ROL='C' for the single recipe component")

		// Standalone producto must be present as ROL='N'.
		nLineas := filterByRol(lineas, "N")
		_, okStandalone := findLineaByArticulo(nLineas, c2.articuloID)
		require.True(t, okStandalone, "standalone producto %d must be a ROL='N' line", c2.articuloID)
		// The combo child c1 must NOT appear as a ROL='N' priced line.
		_, c1AsN := findLineaByArticulo(nLineas, c1.articuloID)
		assert.False(t, c1AsN, "combo child %d must NOT be emitted as ROL='N'", c1.articuloID)

		// Inventory: combo component c1 discharged comboQty×1; standalone c2 by standaloneQty.
		assertSalidasInventarioDelta(ctx, t, q, c1.articuloID, almacen, beforeC1, comboQty.Mul(comp1.Unidades()))
		assertSalidasInventarioDelta(ctx, t, q, c2.articuloID, almacen, beforeC2, standaloneQty)

		t.Logf("mixto: DoctoPVID=%d juego=%d standalone=%d", res.DoctoPVID, juegoID, c2.articuloID)
	})
}

// ─── Test 4: no combos — regression vs current ROL='N' behavior ──────────────

// TestE2E_AplicarComboJuego_SinCombos applies the standard CONTADO venta (no
// combos, no JuegosPorCombo) and asserts it behaves exactly as the legacy path:
// 1 ROL='N' line, 1 IN doc, inventory discharge of 1 unit.
func TestE2E_AplicarComboJuego_SinCombos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)
		userID := seedUsuarioRow(ctx, t, pool)

		before := snapshotSalidasInventario(ctx, t, q, testArticuloID, testAlmacenID)

		v := buildAplicarContado(t, userID)
		res, err := writer.Aplicar(ctx, outbound.MicrosipVentaInput{
			Venta:                v,
			CajaID:               testCajaID,
			CajeroID:             testCajeroID,
			VendedorID:           testVendedorID,
			SucursalID:           testSucursalID,
			FormaCobroID:         formaCobroContadoID,
			NumeroDeVendedoresID: numVendedores1ID,
			// JuegosPorCombo intentionally nil.
		})
		require.NoError(t, err)

		lineas := readDetLineas(ctx, t, q, res.DoctoPVID)
		require.Len(t, lineas, 1, "expected exactly 1 line")
		assert.Equal(t, "N", lineas[0].rol, "standalone producto must be ROL='N'")
		assert.Equal(t, testArticuloID, lineas[0].articuloID)
		assert.Empty(t, filterByRol(lineas, "J"))
		assert.Empty(t, filterByRol(lineas, "C"))

		assertSalidasInventarioDelta(ctx, t, q, testArticuloID, testAlmacenID, before, decimal.RequireFromString("1.0000"))
		t.Logf("sin combos (regresión): DoctoPVID=%d 1 línea ROL='N'", res.DoctoPVID)
	})
}

// ─── Test 5: feature OFF — combo children flatten to ROL='N' ─────────────────

// TestE2E_AplicarComboJuego_FeatureOff builds a combo venta but passes an EMPTY
// JuegosPorCombo (feature off). The combo children must flatten to ROL='N'
// priced lines exactly as the legacy code does — no ROL='J' / ROL='C' lines.
func TestE2E_AplicarComboJuego_FeatureOff(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)
		userID := seedUsuarioRow(ctx, t, pool)

		c1, c2 := discoverDosComponentes(ctx, t, q)
		almacen := c1.almacenID

		comp1, err := domain.NewRecetaComponente(c1.articuloID, decimal.RequireFromString("1.00"))
		require.NoError(t, err)
		comp2, err := domain.NewRecetaComponente(c2.articuloID, decimal.RequireFromString("1.00"))
		require.NoError(t, err)
		componentes := []domain.RecetaComponente{comp1, comp2}

		comboPrecios, err := domain.NewMontoSnapshot(
			decimal.RequireFromString("3000.00"),
			decimal.RequireFromString("2700.00"),
			decimal.RequireFromString("2400.00"),
		)
		require.NoError(t, err)

		comboID := uuid.New()
		v := buildComboVenta(t, userID, comboVentaSpec{
			comboID:       comboID,
			componentes:   componentes,
			comboCantidad: decimal.RequireFromString("1.00"),
			comboPrecios:  comboPrecios,
			almacenOrigen: almacen,
			tipoVenta:     domain.TipoVentaContado,
		})

		res, err := writer.Aplicar(ctx, outbound.MicrosipVentaInput{
			Venta:                v,
			CajaID:               testCajaID,
			CajeroID:             testCajeroID,
			VendedorID:           testVendedorID,
			SucursalID:           testSucursalID,
			FormaCobroID:         formaCobroContadoID,
			NumeroDeVendedoresID: numVendedores1ID,
			JuegosPorCombo:       map[uuid.UUID]int{}, // feature OFF
		})
		require.NoError(t, err)

		lineas := readDetLineas(ctx, t, q, res.DoctoPVID)
		assert.Empty(t, filterByRol(lineas, "J"), "feature off → no ROL='J'")
		assert.Empty(t, filterByRol(lineas, "C"), "feature off → no ROL='C'")
		// Both combo children flatten to ROL='N'.
		nLineas := filterByRol(lineas, "N")
		require.Len(t, nLineas, 2, "feature off → 2 ROL='N' lines (flattened children)")
		_, ok1 := findLineaByArticulo(nLineas, c1.articuloID)
		_, ok2 := findLineaByArticulo(nLineas, c2.articuloID)
		assert.True(t, ok1 && ok2, "both combo children must appear as ROL='N'")

		t.Logf("feature off: DoctoPVID=%d 2 líneas ROL='N' aplanadas", res.DoctoPVID)
	})
}

// ─── Test 6: header montos = ROL='N' + ROL='J' sums (ROL='C' contribute 0) ───

// TestE2E_AplicarComboJuego_HeaderMontos verifies that DOCTOS_PV.IMPORTE_NETO +
// TOTAL_IMPUESTOS equal the standalone ROL='N' line plus the combo ROL='J' line
// (the ROL='C' lines contribute nothing). Uses TASA-0% friendly maths: with a
// 0% IVA juego/article, neto == price and impuestos == 0.
func TestE2E_AplicarComboJuego_HeaderMontos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)
		userID := seedUsuarioRow(ctx, t, pool)

		c1, _ := discoverDosComponentes(ctx, t, q)
		almacen := c1.almacenID

		comp1, err := domain.NewRecetaComponente(c1.articuloID, decimal.RequireFromString("1.00"))
		require.NoError(t, err)

		// Combo at 7000 contado × qty 2 = 14000. New juego is created TASA 0%
		// (no IMPUESTOS_ARTICULOS row) → neto = 14000, impuestos = 0.
		comboQty := decimal.RequireFromString("2.00")
		comboPrecios, err := domain.NewMontoSnapshot(
			decimal.RequireFromString("8000.00"),
			decimal.RequireFromString("7500.00"),
			decimal.RequireFromString("7000.00"),
		)
		require.NoError(t, err)

		comboID := uuid.New()
		v := buildComboVenta(t, userID, comboVentaSpec{
			comboID:       comboID,
			componentes:   []domain.RecetaComponente{comp1},
			comboCantidad: comboQty,
			comboPrecios:  comboPrecios,
			almacenOrigen: almacen,
			tipoVenta:     domain.TipoVentaContado,
		})

		juegoID, _ := resolveJuego(ctx, t, pool, v, comboID)

		res, err := writer.Aplicar(ctx, outbound.MicrosipVentaInput{
			Venta:                v,
			CajaID:               testCajaID,
			CajeroID:             testCajeroID,
			VendedorID:           testVendedorID,
			SucursalID:           testSucursalID,
			FormaCobroID:         formaCobroContadoID,
			NumeroDeVendedoresID: numVendedores1ID,
			JuegosPorCombo:       map[uuid.UUID]int{comboID: juegoID},
		})
		require.NoError(t, err)

		var netoRaw, impRaw any
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT IMPORTE_NETO, TOTAL_IMPUESTOS FROM DOCTOS_PV WHERE DOCTO_PV_ID=?`,
			res.DoctoPVID).Scan(&netoRaw, &impRaw))
		neto, err := firebird.ScanDecimal(netoRaw, 2)
		require.NoError(t, err)
		imp, err := firebird.ScanDecimal(impRaw, 2)
		require.NoError(t, err)

		// New juego is TASA 0% → neto = contado price × qty, impuestos = 0.
		wantNeto := decimal.RequireFromString("7000.00").Mul(comboQty)
		assert.True(t, wantNeto.Equal(neto),
			"header IMPORTE_NETO want=%s got=%s", wantNeto.StringFixed(2), neto.StringFixed(2))
		assert.True(t, imp.IsZero(),
			"header TOTAL_IMPUESTOS must be 0 for TASA-0%% juego; got=%s", imp.StringFixed(2))

		t.Logf("header montos combo: DoctoPVID=%d neto=%s imp=%s", res.DoctoPVID, neto.StringFixed(2), imp.StringFixed(2))
	})
}
