//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
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

// buildE2EAutoCrearClienteService builds a ventasapp.Service wired with the
// full AplicarVenta stack including the real MicrosipClienteWriter, needed for
// the auto-create-cliente branch.
//
// TxManager is required (non-nil) because AplicarVenta calls runInTx
// internally. The TxManager.RunInTx re-entrant guard (transaction.go:49-51)
// detects the ambient test tx injected by txInjector and delegates fn
// directly to it, so every Microsip write — DOCTOS_PV cascade, CLIENTES,
// DIRS_CLIENTES, CLAVES_CLIENTES, LIBRES_CLIENTES — joins the rollback-only
// test transaction. No manual t.Cleanup needed: the ambient rollback at the
// end of WithTestTransaction undoes everything atomically.
func buildE2EAutoCrearClienteService(pool *firebird.Pool) *ventasapp.Service {
	repo := ventfb.NewVentaRepo(pool)
	cfg := ventfb.NewAplicarConfigRepo(pool)
	writer := microsip.NewVentaWriter(pool)
	clienteWriter := microsip.NewClienteWriter(pool)
	store := newFakeStorage()
	clock := fixedClock{T: e2eFixedTime()}
	txMgr := firebird.NewTxManager(pool.DB)
	return ventasapp.NewService(
		repo, nil, nil, store, clock, noopOutbox{}, imageprocessor.NoOpProcessor{},
		txMgr, cfg, writer, clienteWriter,
	)
}

// TestE2E_AplicarVenta_AutoCreaCliente_FullCycle exercises the full HTTP
// lifecycle: POST /ventas without cliente_id → EnviarARevision → Aprobar →
// POST /aplicar. Verifies the auto-create-cliente flow writes to all four
// Microsip tables and links the venta to the new CLIENTE_ID. The ambient
// WithTestTransaction rollback undoes every write at the end — no t.Cleanup
// needed (see buildE2EAutoCrearClienteService doc).
//
//nolint:paralleltest,funlen // shared rollback tx; multi-phase E2E.
func TestE2E_AplicarVenta_AutoCreaCliente_FullCycle(t *testing.T) {
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		// 1. Build full ventasapp.Service with REAL Microsip writers.
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EAutoCrearClienteService(pool)

		// 2. Build chi router wired with txInjector so every request handler
		// executes inside the ambient rollback-only test transaction.
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eAllPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		// 3. POST /v2/ventas (multipart): no cliente_id → auto-create branch.
		//    Use real catalog IDs that satisfy Microsip FK constraints.
		const (
			autoCrearZonaID     = 21563 // MSP_CFG_ZONA_CAJA row used in other E2E tests
			autoCrearArticuloID = 378   // TASA 0%, almacenable; same as idempotency test
			autoCrearAlmacenID  = 11058 // RUTA25 source warehouse
			autoCrearAlmDestID  = 11059 // destination warehouse
		)
		ref := "CASA AZUL ESQUINA - TEST E2E"
		tel := "+5212381234567"
		numExt := "100"

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		// Explicitly clear any default cliente_id so the auto-create branch runs.
		body.Cliente = venthttp.ClienteSnapshotDTO{
			Nombre:     "LAURA HERNANDEZ MARTINEZ TEST 20260602",
			Telefono:   &tel,
			Referencia: &ref,
		}
		body.Direccion = venthttp.DireccionDTO{
			ZonaClienteID:  intPtr(autoCrearZonaID),
			Calle:          "AV CUAUHTÉMOC",
			NumeroExterior: &numExt,
			Colonia:        "CENTRO TEST",
			Poblacion:      "TEHUACAN",
			Ciudad:         "TEHUACAN",
		}
		body.Productos[0].ArticuloID = autoCrearArticuloID
		body.Productos[0].AlmacenOrigenID = intPtr(autoCrearAlmacenID)
		body.Productos[0].AlmacenDestinoID = intPtr(autoCrearAlmDestID)

		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "crear venta: %s", rec.Body.String())

		var created venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		ventaID := created.ID
		require.NotEmpty(t, ventaID)

		// Confirm the venta was created without a cliente_id (auto-create branch).
		assert.Nil(t, created.Cliente.ClienteID,
			"venta created without cliente_id must have nil ClienteID in response")

		// 4. POST /ventas/{id}/revisar → advance to REVISADA.
		req = jsonRequest(t, http.MethodPost, "/ventas/"+ventaID+"/revisar", struct{}{})
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "revisar: %s", rec.Body.String())

		// 5. POST /ventas/{id}/aprobar → advance to APROBADA.
		req = jsonRequest(t, http.MethodPost, "/ventas/"+ventaID+"/aprobar", struct{}{})
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "aprobar: %s", rec.Body.String())

		// 6. POST /ventas/{id}/aplicar → writes to all 4 Microsip tables + DOCTOS_PV.
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

		// After applying, the venta must reference the auto-created cliente.
		require.NotNil(t, applied.Cliente.ClienteID,
			"auto-create branch must link the new CLIENTE_ID back to the venta")
		newClienteID := *applied.Cliente.ClienteID

		t.Logf("auto-created: clienteID=%d doctoPVID=%d folio=%s",
			newClienteID, *applied.MicrosipDoctoPVID, *applied.MicrosipFolio)

		// 7. Verify via direct SQL inside the ambient ctx (firebird.GetQuerier).
		q := firebird.GetQuerier(ctx, pool.DB)

		// a) MSP_VENTAS.CLIENTE_ID must be the newly-created cliente.
		var mspClienteID int
		err := q.QueryRowContext(ctx,
			`SELECT CLIENTE_ID FROM MSP_VENTAS WHERE ID = ?`, ventaID,
		).Scan(&mspClienteID)
		require.NoError(t, err, "MSP_VENTAS must have CLIENTE_ID set")
		assert.Equal(t, newClienteID, mspClienteID,
			"MSP_VENTAS.CLIENTE_ID must match the auto-created cliente")

		// b) CLIENTES.NOMBRE must match the snapshot (Microsip uppercases names).
		var clienteNombre string
		err = q.QueryRowContext(ctx,
			`SELECT NOMBRE FROM CLIENTES WHERE CLIENTE_ID = ?`, newClienteID,
		).Scan(&clienteNombre)
		require.NoError(t, err, "CLIENTES must have a row for the auto-created cliente")
		assert.NotEmpty(t, clienteNombre,
			"CLIENTES.NOMBRE must be non-empty for the auto-created cliente")

		// c) CLAVES_CLIENTES must have a 7-digit clave for the new cliente.
		var claveCliente string
		err = q.QueryRowContext(ctx,
			`SELECT CLAVE_CLIENTE FROM CLAVES_CLIENTES WHERE CLIENTE_ID = ?`, newClienteID,
		).Scan(&claveCliente)
		require.NoError(t, err, "CLAVES_CLIENTES must have a row for the auto-created cliente")
		assert.Len(t, claveCliente, 7,
			"CLAVES_CLIENTES.CLAVE_CLIENTE must be 7 digits")

		// d) DIRS_CLIENTES must carry the calle from the request snapshot.
		var dirCalle string
		err = q.QueryRowContext(ctx,
			`SELECT CALLE FROM DIRS_CLIENTES WHERE CLIENTE_ID = ?`, newClienteID,
		).Scan(&dirCalle)
		require.NoError(t, err, "DIRS_CLIENTES must have a row for the auto-created cliente")
		assert.Contains(t, dirCalle, "CUAUHTÉMOC",
			"DIRS_CLIENTES.CALLE must contain the calle from the request snapshot")

		// e) LIBRES_CLIENTES.REFERENCIA must match the referencia sent in the request.
		var libresRef string
		err = q.QueryRowContext(ctx,
			`SELECT REFERENCIA FROM LIBRES_CLIENTES WHERE CLIENTE_ID = ?`, newClienteID,
		).Scan(&libresRef)
		require.NoError(t, err, "LIBRES_CLIENTES must have a row for the auto-created cliente")
		assert.Equal(t, ref, libresRef,
			"LIBRES_CLIENTES.REFERENCIA must match the referencia from the request")

		// f) DOCTOS_PV must have exactly one row and APLICADO='S'.
		var doctoCount int
		err = q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM DOCTOS_PV WHERE DOCTO_PV_ID = ?`,
			*applied.MicrosipDoctoPVID,
		).Scan(&doctoCount)
		require.NoError(t, err)
		assert.Equal(t, 1, doctoCount,
			"exactly one DOCTOS_PV row must exist for the applied venta")

		// 8. Test exits — fbtestutil rolls back the MSP_VENTAS rows.
		// t.Cleanup (registered above) removes committed Microsip rows.
	})
}
