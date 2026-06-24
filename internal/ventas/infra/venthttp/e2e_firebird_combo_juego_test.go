//nolint:misspell,paralleltest,funlen // Spanish vocabulary; integration tests share pool and must not run in parallel; multi-phase E2E.
package venthttp_test

// e2e_firebird_combo_juego_test.go exercises the full composition stack for the
// combo→juego feature: HTTP POST /ventas (with combo + productos) → revisar →
// aprobar → aplicar, with the real JuegoResolver wired into the Service via
// WithJuegos. Verifies end-to-end that after aplicar the DOCTOS_PV row contains
// ROL='J' lines for combos, ROL='C' lines for their components, and the
// components' inventory has been discharged.
//
// Gating: e2eTestPool skips the test when FB_DATABASE is unset. When
// FB_DATABASE IS set, e2eDiscoverDosComponentes calls t.Fatalf on failure so a
// depleted dev DB never produces a false-green suite.
//
// All writes run inside WithTestTransaction (rollback-only). No explicit
// t.Cleanup needed: the ambient rollback undoes every Microsip write atomically.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/infra/microsip"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// e2eJuegoLineaArticuloID is the LINEA_ARTICULO_ID used when the JuegoResolver
// must create a new juego article. Matches the constant used by Task 4's
// writer-level tests (aplicar_e2e_combo_juego_test.go).
const e2eJuegoLineaArticuloID = 11774

// ─── service builder ─────────────────────────────────────────────────────────

// buildE2EComboJuegoService builds a ventasapp.Service fully wired with the
// real JuegoResolver, VentaWriter, AplicarConfig, and a TxManager.
//
// TxManager is required (non-nil) because AplicarVenta calls runInTx
// internally. The TxManager.RunInTx re-entrant guard detects the ambient test
// tx injected by txInjector and delegates fn directly to it, so every Microsip
// write joins the rollback-only test transaction.
func buildE2EComboJuegoService(pool *firebird.Pool) *ventasapp.Service {
	repo := ventfb.NewVentaRepo(pool)
	cfg := ventfb.NewAplicarConfigRepo(pool)
	writer := microsip.NewVentaWriter(pool)
	clienteWriter := microsip.NewClienteWriter(pool)
	resolver := microsip.NewJuegoResolver(pool)
	store := newFakeStorage()
	clock := fixedClock{T: e2eFixedTime()}
	txMgr := firebird.NewTxManager(pool.DB)
	return ventasapp.NewService(
		repo, nil, nil, store, clock, noopOutbox{}, imageprocessor.NoOpProcessor{},
		txMgr, cfg, writer, clienteWriter,
	).WithJuegos(resolver, true, e2eJuegoLineaArticuloID)
}

// ─── discovery helper ────────────────────────────────────────────────────────

// e2eComboComponente describes one discovered component article.
type e2eComboComponente struct {
	articuloID int
	almacenID  int
}

// e2eDiscoverDosComponentes finds two DISTINCT almacenable, active, non-juego
// articles in the SAME almacén, each with a clave (ROL=17). Calls t.Fatalf
// when the DB lacks the minimum seed — never silently passes on a depleted DB.
func e2eDiscoverDosComponentes(ctx context.Context, t *testing.T, q firebird.Querier) (e2eComboComponente, e2eComboComponente) {
	t.Helper()

	var almacenID int
	if err := q.QueryRowContext(ctx, `
		SELECT FIRST 1 a.ALMACEN_ID
		FROM (
			SELECT DISTINCT s.ARTICULO_ID, s.ALMACEN_ID
			FROM SALDOS_IN s
			JOIN ARTICULOS ar ON ar.ARTICULO_ID = s.ARTICULO_ID
			WHERE ar.ES_ALMACENABLE = 'S' AND ar.ESTATUS = 'A' AND ar.ES_JUEGO <> 'S'
			  AND EXISTS (SELECT 1 FROM CLAVES_ARTICULOS c
			               WHERE c.ARTICULO_ID = s.ARTICULO_ID
			                 AND c.ROL_CLAVE_ART_ID = 17)
		) a
		GROUP BY a.ALMACEN_ID
		HAVING COUNT(*) >= 2
		ORDER BY COUNT(*) DESC`).Scan(&almacenID); err != nil {
		t.Fatalf("e2eDiscoverDosComponentes: no almacén with >=2 almacenable components (clave rol=17) in dev DB — fixture gap: %v", err)
	}

	rows, err := q.QueryContext(ctx, `
		SELECT FIRST 2 s.ARTICULO_ID
		FROM SALDOS_IN s
		JOIN ARTICULOS ar ON ar.ARTICULO_ID = s.ARTICULO_ID
		WHERE ar.ES_ALMACENABLE = 'S' AND ar.ESTATUS = 'A' AND ar.ES_JUEGO <> 'S'
		  AND s.ALMACEN_ID = ?
		  AND EXISTS (SELECT 1 FROM CLAVES_ARTICULOS c
		               WHERE c.ARTICULO_ID = s.ARTICULO_ID
		                 AND c.ROL_CLAVE_ART_ID = 17)
		GROUP BY s.ARTICULO_ID
		ORDER BY s.ARTICULO_ID`,
		almacenID,
	)
	require.NoError(t, err, "query componentes por almacén")
	defer rows.Close() //nolint:errcheck // best-effort after iteration

	var ids []int
	for rows.Next() {
		var id int
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	if len(ids) < 2 {
		t.Fatalf("e2eDiscoverDosComponentes: almacén %d has fewer than 2 qualifying components — fixture gap", almacenID)
	}

	return e2eComboComponente{articuloID: ids[0], almacenID: almacenID},
		e2eComboComponente{articuloID: ids[1], almacenID: almacenID}
}

// e2eSalidasInventario reads SALDOS_IN.SALIDAS_UNIDADES for an article+almacén
// in the current year+month. SALDOS_IN has one row per (ARTICULO_ID, ALMACEN_ID,
// ANO, MES) — without the ANO/MES filter the query returns multiple rows and
// QueryRowContext picks an arbitrary one, making the delta measure unreliable.
// Returns decimal.Zero when no row exists (zero baseline before any discharge).
func e2eSalidasInventario(ctx context.Context, t *testing.T, q firebird.Querier, articuloID, almacenID int) decimal.Decimal {
	t.Helper()
	var raw any
	err := q.QueryRowContext(ctx,
		`SELECT CAST(SALIDAS_UNIDADES AS NUMERIC(18,6))
		 FROM SALDOS_IN
		 WHERE ARTICULO_ID = ? AND ALMACEN_ID = ?
		   AND ANO = EXTRACT(YEAR FROM CURRENT_DATE)
		   AND MES = EXTRACT(MONTH FROM CURRENT_DATE)`,
		articuloID, almacenID,
	).Scan(&raw)
	if err != nil {
		return decimal.Zero
	}
	v, scanErr := firebird.ScanDecimal(raw, 6)
	require.NoError(t, scanErr)
	return v
}

// ─── DET-line helpers ─────────────────────────────────────────────────────────

type e2eDetLinea struct {
	articuloID int
	rol        string
	unidades   decimal.Decimal
}

func e2eReadDetLineas(ctx context.Context, t *testing.T, q firebird.Querier, doctoPVID int) []e2eDetLinea {
	t.Helper()
	rows, err := q.QueryContext(ctx, `
		SELECT ARTICULO_ID, ROL, CAST(UNIDADES AS NUMERIC(18,6))
		FROM DOCTOS_PV_DET
		WHERE DOCTO_PV_ID = ?
		ORDER BY DOCTO_PV_DET_ID`,
		doctoPVID,
	)
	require.NoError(t, err, "query DOCTOS_PV_DET")
	defer rows.Close() //nolint:errcheck // best-effort after iteration

	var out []e2eDetLinea
	for rows.Next() {
		var l e2eDetLinea
		var rawU any
		require.NoError(t, rows.Scan(&l.articuloID, &l.rol, &rawU))
		u, scanErr := firebird.ScanDecimal(rawU, 6)
		require.NoError(t, scanErr)
		l.unidades = u
		out = append(out, l)
	}
	require.NoError(t, rows.Err())
	return out
}

func e2eFilterByRol(lineas []e2eDetLinea, rol string) []e2eDetLinea {
	var out []e2eDetLinea
	for _, l := range lineas {
		if l.rol == rol {
			out = append(out, l)
		}
	}
	return out
}

func e2eFindByArticulo(lineas []e2eDetLinea, articuloID int) (e2eDetLinea, bool) {
	for _, l := range lineas {
		if l.articuloID == articuloID {
			return l, true
		}
	}
	return e2eDetLinea{}, false
}

// e2eDistintoAlmacen returns a warehouse ID different from origen so the
// domain's equal-warehouse guard never trips.
func e2eDistintoAlmacen(origen int) int {
	if origen == 11058 {
		return 11059
	}
	return 11058
}

// ─── Test: composition e2e — combo venta with juego resolver wired ────────────

// TestE2E_ComboJuego_FullCycle_WithResolver exercises the full HTTP lifecycle
// with the JuegoResolver wired into the Service:
//
//  1. POST /ventas with one combo of two real components (discovered from the live DB).
//  2. POST /revisar → REVISADA.
//  3. POST /aprobar → APROBADA.
//  4. POST /aplicar → APLICADA; resolver creates or matches the juego.
//
// Verifies after aplicar:
//   - DOCTOS_PV has exactly 1 ROL='J' line for the juego.
//   - DOCTOS_PV has exactly 2 ROL='C' lines (one per component).
//   - Each component's SALDOS_IN.SALIDAS_UNIDADES increased by comboQty × per-juego-unidades.
//   - The ambient WithTestTransaction rollback undoes every write at test end.
func TestE2E_ComboJuego_FullCycle_WithResolver(t *testing.T) {
	pool := e2eTestPool(t) // skips when FB_DATABASE is unset

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		// 1. Discover two almacenable components from the live DB.
		c1, c2 := e2eDiscoverDosComponentes(ctx, t, q)
		almOrigen := c1.almacenID
		almDestino := e2eDistintoAlmacen(almOrigen)

		// 2. Build the Service with the juego resolver wired.
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EComboJuegoService(pool)

		// 3. Build chi router wired with txInjector and all ventas permissions.
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eAllPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		// zona 21563 is testZonaID — MSP_CFG_ZONA_CAJA row used in other E2E tests.
		const e2eZonaID = 21563
		comboID := uuid.NewString()

		// comboQty=2, c1 per-juego=1, c2 per-juego=2 → discharge: c1+=2, c2+=4
		comboQty := decimal.RequireFromString("2.0000")
		c1PerJuego := decimal.RequireFromString("1.0000")
		c2PerJuego := decimal.RequireFromString("2.0000")

		body := venthttp.CrearVentaBody{
			ID: uuid.NewString(),
			Cliente: venthttp.ClienteSnapshotDTO{
				ClienteID: intPtr(11486), // testClienteID
				Nombre:    "García Morales Beatriz",
			},
			Direccion: venthttp.DireccionDTO{
				Calle:         "Av. Hidalgo",
				Colonia:       "Centro",
				Poblacion:     "Tehuacán",
				Ciudad:        "Puebla",
				ZonaClienteID: intPtr(e2eZonaID),
			},
			GPS:        venthttp.GPSDTO{Latitud: 18.46, Longitud: -97.39},
			FechaVenta: e2eFixedTime().Format("2006-01-02T15:04:05Z"),
			TipoVenta:  "CONTADO",
			Montos:     venthttp.MontosDTO{Anual: "5000.00", CortoPlazo: "4500.00", Contado: "4000.00"},
			Combos: []venthttp.ComboDTO{{
				ID:               comboID,
				Nombre:           "Combo Recámara E2E",
				PrecioAnual:      "5000.00",
				PrecioCorto:      "4500.00",
				PrecioContado:    "4000.00",
				Cantidad:         comboQty.StringFixed(4),
				AlmacenOrigenID:  almOrigen,
				AlmacenDestinoID: almDestino,
			}},
			Productos: []venthttp.ProductoDTO{
				{
					ID:            uuid.NewString(),
					ArticuloID:    c1.articuloID,
					Articulo:      "Componente 1 E2E",
					Cantidad:      c1PerJuego.StringFixed(4), // per-juego unidades for c1
					PrecioAnual:   "100.00",
					PrecioCorto:   "90.00",
					PrecioContado: "80.00",
					ComboID:       strPtr(comboID),
					// combo children must NOT carry their own almacenes
				},
				{
					ID:            uuid.NewString(),
					ArticuloID:    c2.articuloID,
					Articulo:      "Componente 2 E2E",
					Cantidad:      c2PerJuego.StringFixed(4), // per-juego unidades for c2
					PrecioAnual:   "100.00",
					PrecioCorto:   "90.00",
					PrecioContado: "80.00",
					ComboID:       strPtr(comboID),
				},
			},
			Vendedores: []venthttp.VendedorDTO{{
				ID:        uuid.NewString(),
				UsuarioID: usuarioID.String(),
				Email:     "ruta25@muebleriamsp.mx",
				Nombre:    "Vendedor Ruta 25",
			}},
		}

		// 4. POST /ventas — crear.
		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "crear venta: %s", rec.Body.String())

		var created venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		ventaID := created.ID
		require.NotEmpty(t, ventaID)

		// 5. POST /ventas/{id}/revisar → REVISADA.
		req = jsonRequest(t, http.MethodPost, "/ventas/"+ventaID+"/revisar", struct{}{})
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "revisar: %s", rec.Body.String())

		// 6. POST /ventas/{id}/aprobar → APROBADA.
		req = jsonRequest(t, http.MethodPost, "/ventas/"+ventaID+"/aprobar", struct{}{})
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "aprobar: %s", rec.Body.String())

		// Snapshot inventory baselines AFTER state transitions but BEFORE aplicar
		// so the delta calculation is clean.
		beforeC1 := e2eSalidasInventario(ctx, t, q, c1.articuloID, almOrigen)
		beforeC2 := e2eSalidasInventario(ctx, t, q, c2.articuloID, almOrigen)

		// 7. POST /ventas/{id}/aplicar → writes to Microsip via JuegoResolver + VentaWriter.
		req = jsonRequest(t, http.MethodPost, "/ventas/"+ventaID+"/aplicar", struct{}{})
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "aplicar: %s", rec.Body.String())

		var applied venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &applied))

		require.NotNil(t, applied.MicrosipDoctoPVID,
			"applied venta must carry MicrosipDoctoPVID")
		require.NotNil(t, applied.MicrosipFolio,
			"applied venta must carry MicrosipFolio")

		doctoPVID := *applied.MicrosipDoctoPVID
		t.Logf("combo juego e2e: DoctoPVID=%d folio=%s c1=%d c2=%d almacen=%d",
			doctoPVID, *applied.MicrosipFolio, c1.articuloID, c2.articuloID, almOrigen)

		// 8. Verify DOCTOS_PV_DET structure.
		lineas := e2eReadDetLineas(ctx, t, q, doctoPVID)

		// Exactly 1 ROL='J' line for the juego.
		jLineas := e2eFilterByRol(lineas, "J")
		require.Len(t, jLineas, 1,
			"expected exactly 1 ROL='J' line for the combo juego; got lines=%v", lineas)
		jLinea := jLineas[0]

		// The ROL='J' UNIDADES must equal the comboQty.
		assert.True(t, comboQty.Equal(jLinea.unidades),
			"ROL='J' UNIDADES must equal comboQty=%s; got=%s",
			comboQty.StringFixed(4), jLinea.unidades.StringFixed(4))

		// Exactly 2 ROL='C' lines — one per component.
		cLineas := e2eFilterByRol(lineas, "C")
		require.Len(t, cLineas, 2,
			"expected 2 ROL='C' component lines; got=%v", lineas)

		l1, ok1 := e2eFindByArticulo(cLineas, c1.articuloID)
		require.True(t, ok1, "ROL='C' for component c1=%d must exist", c1.articuloID)
		wantU1 := comboQty.Mul(c1PerJuego)
		assert.True(t, wantU1.Equal(l1.unidades),
			"ROL='C' c1 UNIDADES want=%s got=%s",
			wantU1.StringFixed(4), l1.unidades.StringFixed(4))

		l2, ok2 := e2eFindByArticulo(cLineas, c2.articuloID)
		require.True(t, ok2, "ROL='C' for component c2=%d must exist", c2.articuloID)
		wantU2 := comboQty.Mul(c2PerJuego)
		assert.True(t, wantU2.Equal(l2.unidades),
			"ROL='C' c2 UNIDADES want=%s got=%s",
			wantU2.StringFixed(4), l2.unidades.StringFixed(4))

		// 9. Verify inventory discharge: SALIDAS_UNIDADES delta for each component.
		afterC1 := e2eSalidasInventario(ctx, t, q, c1.articuloID, almOrigen)
		deltaC1 := afterC1.Sub(beforeC1)
		assert.True(t, wantU1.Equal(deltaC1),
			"inventory discharge c1=%d: SALIDAS_UNIDADES delta want=%s got=%s",
			c1.articuloID, wantU1.StringFixed(4), deltaC1.StringFixed(4))

		afterC2 := e2eSalidasInventario(ctx, t, q, c2.articuloID, almOrigen)
		deltaC2 := afterC2.Sub(beforeC2)
		assert.True(t, wantU2.Equal(deltaC2),
			"inventory discharge c2=%d: SALIDAS_UNIDADES delta want=%s got=%s",
			c2.articuloID, wantU2.StringFixed(4), deltaC2.StringFixed(4))

		// 10. Verify the juego article exists and has ES_JUEGO='S'.
		var esJuego string
		err := q.QueryRowContext(ctx,
			`SELECT ES_JUEGO FROM ARTICULOS WHERE ARTICULO_ID = ?`, jLinea.articuloID,
		).Scan(&esJuego)
		require.NoError(t, err, "ARTICULOS must have a row for the juego")
		assert.Equal(t, "S", esJuego,
			"ARTICULOS.ES_JUEGO must be 'S' for the created juego")

		// Test exits — fbtestutil rolls back every MSP_VENTAS, DOCTOS_PV, ARTICULOS
		// (juego), JUEGOS_DET, and LIBRES_ARTICULOS row written during this test.
	})
}
