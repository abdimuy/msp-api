//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// TestRepro_AplicarVentaClienteExistente reproduces "the error happens when
// inserting into Microsip with an already-existing cliente". It runs the FULL
// lifecycle (crear → revisar → aprobar → aplicar) against the real Microsip
// writer, for a venta linked to an EXISTING cliente_id, with and without
// zona_cliente_id. Everything is rolled back (WithTestTransaction).
//
//nolint:paralleltest // shares one rollback tx with the dev DB
func TestRepro_AplicarVentaClienteExistente(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		clienteID := pickActiveClienteID(ctx, t, pool)
		t.Logf("usando CLIENTE_ID existente = %d", clienteID)

		svc := buildE2EAutoCrearClienteService(pool) // full aplicar stack + Microsip writers
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eAllPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		const (
			articuloID = 378
			almOrigen  = 11058
			almDestino = 11059
			zonaID     = 21563
		)

		// crearYAplicar runs the full lifecycle and returns the /aplicar recorder.
		crearYAplicar := func(conZona bool) *httptest.ResponseRecorder {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			body.Cliente = venthttp.ClienteSnapshotDTO{
				ClienteID: &clienteID,
				Nombre:    "CLIENTE EXISTENTE TEST",
			}
			dir := venthttp.DireccionDTO{
				Calle:     "AV TEST",
				Colonia:   "CENTRO",
				Poblacion: "TEHUACAN",
				Ciudad:    "TEHUACAN",
			}
			if conZona {
				z := zonaID
				dir.ZonaClienteID = &z
			}
			body.Direccion = dir
			body.Productos[0].ArticuloID = articuloID
			body.Productos[0].AlmacenOrigenID = intPtr(almOrigen)
			body.Productos[0].AlmacenDestinoID = intPtr(almDestino)

			req := crearVentaMultipartRequest(t, body)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.Equal(t, http.StatusCreated, rec.Code, "crear: %s", rec.Body.String())
			id := body.ID

			for _, step := range []string{"revisar", "aprobar"} {
				req = jsonRequest(t, http.MethodPost, "/ventas/"+id+"/"+step, struct{}{})
				rec = httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				require.Equal(t, http.StatusOK, rec.Code, "%s: %s", step, rec.Body.String())
			}

			req = jsonRequest(t, http.MethodPost, "/ventas/"+id+"/aplicar", struct{}{})
			rec = httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			return rec
		}

		recSinZona := crearYAplicar(false)
		t.Logf("CLIENTE EXISTENTE, SIN zona_cliente_id → aplicar HTTP=%d body=%s",
			recSinZona.Code, recSinZona.Body.String())

		// Prove the existing-cliente apply does NOT create a new cliente.
		q := firebird.GetQuerier(ctx, pool.DB)
		var clientesAntes int
		require.NoError(t, q.QueryRowContext(ctx, `SELECT COUNT(*) FROM CLIENTES`).Scan(&clientesAntes))

		recConZona := crearYAplicar(true)
		require.Equal(t, http.StatusOK, recConZona.Code, "aplicar con zona debe ser 200: %s", recConZona.Body.String())

		var applied venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(recConZona.Body.Bytes(), &applied))
		require.NotNil(t, applied.MicrosipDoctoPVID)
		require.NotNil(t, applied.Cliente.ClienteID)

		var clientesDespues int
		require.NoError(t, q.QueryRowContext(ctx, `SELECT COUNT(*) FROM CLIENTES`).Scan(&clientesDespues))

		// DOCTOS_PV must reference the SAME existing cliente, not a freshly created one.
		var doctoClienteID int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT CLIENTE_ID FROM DOCTOS_PV WHERE DOCTO_PV_ID = ?`, *applied.MicrosipDoctoPVID,
		).Scan(&doctoClienteID))

		t.Logf("CLIENTE EXISTENTE, CON zona → aplicar HTTP=200 docto_pv=%d folio=%v",
			*applied.MicrosipDoctoPVID, applied.MicrosipFolio)
		t.Logf("CLIENTES antes=%d despues=%d (delta=%d) | venta.cliente_id=%d | DOCTOS_PV.CLIENTE_ID=%d (esperado=%d)",
			clientesAntes, clientesDespues, clientesDespues-clientesAntes,
			*applied.Cliente.ClienteID, doctoClienteID, clienteID)

		require.Equal(t, clientesAntes, clientesDespues, "NO debe crearse un cliente nuevo en CLIENTES")
		require.Equal(t, clienteID, *applied.Cliente.ClienteID, "la venta debe seguir ligada al cliente existente")
		require.Equal(t, clienteID, doctoClienteID, "DOCTOS_PV debe apuntar al cliente existente")
	})
}
