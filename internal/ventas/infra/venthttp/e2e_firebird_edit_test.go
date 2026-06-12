//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// e2ePickClienteID returns a real CLIENTE_ID from Microsip's CLIENTES table;
// the test skips when the table is empty.
func e2ePickClienteID(ctx context.Context, t *testing.T, pool *firebird.Pool) int {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var id int
	err := q.QueryRowContext(ctx, `SELECT FIRST 1 CLIENTE_ID FROM CLIENTES`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		t.Skip("CLIENTES table empty — cannot exercise the FK path")
	}
	require.NoError(t, err)
	return id
}

// e2eSeedVenta creates a venta via the API in the active tx context and
// returns the assigned ID.
func e2eSeedVenta(t *testing.T, r http.Handler, usuarioID uuid.UUID) string {
	t.Helper()
	body := validCreateBody()
	body.Vendedores[0].UsuarioID = usuarioID.String()
	req := crearVentaMultipartRequest(t, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "seed body=%s", rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out.ID
}

// TestE2E_Firebird_EditarVentaHeader verifies the PATCH /ventas/{id} round
// trip persists the new header fields against the real Firebird schema.
//
//nolint:paralleltest // E2E tests share a tx and must run serially.
func TestE2E_Firebird_EditarVentaHeader(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		id := e2eSeedVenta(t, r, usuarioID)

		// PATCH header.
		body := validHeaderBody()
		body.Nota = strPtr("corrección de campo")
		req := jsonRequest(t, http.MethodPatch, "/ventas/"+id, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var out venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		// Folded to ALL CAPS by the domain (Microsip convention).
		assert.Equal(t, "AV. EDITADA", out.Direccion.Calle)
		require.NotNil(t, out.Nota)
		assert.Equal(t, "CORRECCIÓN DE CAMPO", *out.Nota)
		// FechaVenta of the request body was 2026-05-01T09:00:00Z. With the
		// BusinessTZ contract this round-trips exactly through Firebird.
		assert.Equal(t, "2026-05-01T09:00:00Z", out.FechaVenta,
			"FechaVenta must round-trip exactly in UTC after the TZ fix")

		// GET round-trip — the persisted row must reflect the edit.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+id, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "AV. EDITADA", got.Direccion.Calle)
		assert.Equal(t, "borrador", got.Situacion)
		assert.Equal(t, "2026-05-01T09:00:00Z", got.FechaVenta)
	})
}

// TestE2E_Firebird_ReemplazarProductos_MultiAlmacen verifies multi-almacen
// origen on a single venta: 2 productos with different almacenes survive
// the write/read cycle.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_ReemplazarProductos_MultiAlmacen(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		id := e2eSeedVenta(t, r, usuarioID)

		body := venthttp.ReemplazarProductosBody{Productos: []venthttp.ProductoDTO{
			{
				ID: uuid.NewString(), ArticuloID: 1, Articulo: "Mesa",
				Cantidad: "1", PrecioAnual: "100", PrecioCorto: "90", PrecioContado: "80",
				AlmacenOrigenID: intPtr(10), AlmacenDestinoID: intPtr(99),
			},
			{
				ID: uuid.NewString(), ArticuloID: 2, Articulo: "Silla",
				Cantidad: "1", PrecioAnual: "50", PrecioCorto: "45", PrecioContado: "40",
				AlmacenOrigenID: intPtr(20), AlmacenDestinoID: intPtr(99),
			},
		}}
		req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/productos", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		req = httptest.NewRequest(http.MethodGet, "/ventas/"+id, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got.Productos, 2)
		origins := map[int]struct{}{}
		for _, p := range got.Productos {
			require.NotNil(t, p.AlmacenOrigenID)
			origins[*p.AlmacenOrigenID] = struct{}{}
		}
		assert.Contains(t, origins, 10)
		assert.Contains(t, origins, 20)
	})
}

// TestE2E_Firebird_Combo_ConCantidad verifies combo Cantidad and the
// combo-child producto inherit/propagate correctly.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_Combo_ConCantidad(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		// Create venta with one combo + one producto inside that combo.
		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		comboID := uuid.NewString()
		body.Combos = []venthttp.ComboDTO{{
			ID: comboID, Nombre: "Recámara Completa",
			PrecioAnual: "5000", PrecioCorto: "4500", PrecioContado: "4000",
			Cantidad: "3", AlmacenOrigenID: 1, AlmacenDestinoID: 2,
		}}
		body.Productos = []venthttp.ProductoDTO{{
			ID: uuid.NewString(), ArticuloID: 42, Articulo: "Cama",
			Cantidad: "1", PrecioAnual: "1000", PrecioCorto: "900", PrecioContado: "800",
			ComboID: &comboID, // combo-child: no almacenes
		}}
		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		// Read back and assert.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got.Combos, 1)
		assert.Equal(t, "3.0000", got.Combos[0].Cantidad)
		assert.Equal(t, 1, got.Combos[0].AlmacenOrigenID)
		require.Len(t, got.Productos, 1)
		require.NotNil(t, got.Productos[0].ComboID)
		assert.Equal(t, comboID, *got.Productos[0].ComboID)
		assert.Nil(t, got.Productos[0].AlmacenOrigenID, "combo-child producto must not carry almacenes")
	})
}

// TestE2E_Firebird_ListFilters_ClienteID verifies the cliente_id list filter
// returns only ventas linked to that cliente.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_ListFilters_ClienteID(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		realClienteID := e2ePickClienteID(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		// Seed two ventas: one with cliente_id, one without.
		idWith := e2eSeedVenta(t, r, usuarioID)
		idWithout := e2eSeedVenta(t, r, usuarioID)

		// Link idWith to realClienteID via PATCH /cliente.
		req := jsonRequest(t, http.MethodPatch, "/ventas/"+idWith+"/cliente",
			venthttp.ActualizarClienteBody{Cliente: venthttp.ClienteSnapshotDTO{
				ClienteID: &realClienteID, Nombre: "Linked",
			}})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		// List filtered by cliente_id.
		listURL := "/ventas?cliente_id=" + strconv.Itoa(realClienteID) + "&limit=200"
		req = httptest.NewRequest(http.MethodGet, listURL, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var list venthttp.ListResponse[venthttp.VentaDTO]
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))

		foundWith := false
		for _, item := range list.Items {
			if item.ID == idWith {
				foundWith = true
			}
			if item.ID == idWithout {
				t.Errorf("venta without cliente_id leaked into filter result: id=%s", idWithout)
			}
		}
		assert.True(t, foundWith, "the linked venta must appear in the filtered list")
	})
}

// TestE2E_Firebird_Cliente_FK_Invalida verifies POST /ventas with an
// invalid cliente_id returns 422 (or 5xx if the FK check kicks in at write
// time — we accept either as long as it does not persist).
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_Cliente_FK_Invalida(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)

		// Build service with a real ClienteRepo (Firebird-backed).
		svc := buildE2EServiceWithCliente(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		// A cliente_id guaranteed to NOT exist: MAX+1.
		q := firebird.GetQuerier(ctx, pool.DB)
		var maxID sql.NullInt32
		require.NoError(t, q.QueryRowContext(ctx, `SELECT MAX(CLIENTE_ID) FROM CLIENTES`).Scan(&maxID))
		probe := 999_999_999
		if maxID.Valid {
			probe = int(maxID.Int32) + 1
		}

		body := validCreateBody()
		body.Cliente.ClienteID = &probe
		body.Vendedores[0].UsuarioID = usuarioID.String()
		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
			"invalid cliente_id should reject with 422; body=%s", rec.Body.String())
	})
}

// TestE2E_Firebird_EditarVenta_Cancelada_Rejected verifies that after a
// venta is cancelled, the 5 edit endpoints reject further mutations.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_EditarVenta_Cancelada_Rejected(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		id := e2eSeedVenta(t, r, usuarioID)

		// Cancel it.
		req := jsonRequest(t, http.MethodPatch, "/ventas/"+id+"/cancel",
			venthttp.CancelarVentaBody{Reason: "test"})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		// Every edit endpoint should now return 409.
		matrix := []struct {
			method, path string
			body         any
		}{
			{http.MethodPatch, "/ventas/" + id, validHeaderBody()},
			{http.MethodPatch, "/ventas/" + id + "/cliente", validClienteBody()},
			{http.MethodPut, "/ventas/" + id + "/productos", validProductosBody()},
			{http.MethodPut, "/ventas/" + id + "/combos", validCombosBody()},
			{http.MethodPut, "/ventas/" + id + "/vendedores", validVendedoresBody()},
		}
		for _, tc := range matrix {
			t.Run(tc.method+" "+tc.path, func(t *testing.T) {
				req := jsonRequest(t, tc.method, tc.path, tc.body)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				assert.Equal(t, http.StatusConflict, rec.Code,
					"want 409, got %d body=%s", rec.Code, rec.Body.String())
			})
		}
	})
}

// strPtr is a test helper for *string fields.
func strPtr(s string) *string { return &s }
