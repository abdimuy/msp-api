//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// seedVentaViaHTTP creates a venta through the API so subsequent edit
// requests have a target. Returns the venta's ID.
func seedVentaViaHTTP(t *testing.T, r http.Handler) string {
	t.Helper()
	body := validCreateBody()
	req := crearVentaMultipartRequest(t, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "seed venta: %s", rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out.ID
}

// validHeaderBody returns an ActualizarHeaderBody with every required field
// populated; tests mutate fields before sending.
func validHeaderBody() venthttp.ActualizarHeaderBody {
	return venthttp.ActualizarHeaderBody{
		Direccion: venthttp.DireccionDTO{
			Calle: "Av. Editada", Colonia: "Centro", Poblacion: "Mérida", Ciudad: "Mérida",
		},
		GPS:        venthttp.GPSDTO{Latitud: 20.97, Longitud: -89.62},
		FechaVenta: time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
}

// validClienteBody returns an ActualizarClienteBody with the minimum
// required fields.
func validClienteBody() venthttp.ActualizarClienteBody {
	return venthttp.ActualizarClienteBody{
		Cliente: venthttp.ClienteSnapshotDTO{Nombre: "Cliente Editado"},
	}
}

// validProductosBody returns a ReemplazarProductosBody with a single
// stand-alone producto.
func validProductosBody() venthttp.ReemplazarProductosBody {
	return venthttp.ReemplazarProductosBody{
		Productos: []venthttp.ProductoDTO{{
			ID: uuid.NewString(), ArticuloID: 99, Articulo: "Otro",
			Cantidad: "2", PrecioAnual: "100", PrecioCorto: "90", PrecioContado: "80",
			AlmacenOrigenID: intPtr(1), AlmacenDestinoID: intPtr(2),
		}},
	}
}

// validCombosBody returns a ReemplazarCombosBody with a single combo.
func validCombosBody() venthttp.ReemplazarCombosBody {
	return venthttp.ReemplazarCombosBody{
		Combos: []venthttp.ComboDTO{{
			ID: uuid.NewString(), Nombre: "Bundle",
			PrecioAnual: "500", PrecioCorto: "450", PrecioContado: "400",
			Cantidad: "2", AlmacenOrigenID: 1, AlmacenDestinoID: 2,
		}},
	}
}

// validVendedoresBody returns a ReemplazarVendedoresBody with a single
// vendedor.
func validVendedoresBody() venthttp.ReemplazarVendedoresBody {
	return venthttp.ReemplazarVendedoresBody{
		Vendedores: []venthttp.VendedorDTO{{
			ID: uuid.NewString(), UsuarioID: uuid.NewString(),
			Email: "v@x.com", Nombre: "V",
		}},
	}
}

// limitedPerms returns a CurrentUser with only PermVentasCrear/Ver/Listar.
// Used to assert 403 on edit endpoints.
func limitedPerms(id uuid.UUID) auth.CurrentUser {
	return auth.CurrentUser{
		ID: id,
		Permisos: []string{
			string(authdomain.PermVentasCrear),
			string(authdomain.PermVentasVer),
			string(authdomain.PermVentasListar),
			string(authdomain.PermVentasCancelar),
		},
	}
}

// ─── ActualizarHeader (PATCH /v2/ventas/{id}) ──────────────────────────────

func TestActualizarHeader_OK(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)
	id := seedVentaViaHTTP(t, r)

	req := jsonRequest(t, http.MethodPatch, "/ventas/"+id, validHeaderBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	// Address text is folded to ALL CAPS by the domain (Microsip convention).
	assert.Equal(t, "AV. EDITADA", out.Direccion.Calle)
}

func TestActualizarHeader_Unauthenticated(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouterNoAuth(t, svc)
	req := jsonRequest(t, http.MethodPatch, "/ventas/"+uuid.NewString(), validHeaderBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestActualizarHeader_PermissionDenied(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	cuFull := fullPerms(uuid.New())
	rFull := buildRouter(t, svc, cuFull)
	id := seedVentaViaHTTP(t, rFull)

	cu := limitedPerms(uuid.New())
	r := buildRouter(t, svc, cu)
	req := jsonRequest(t, http.MethodPatch, "/ventas/"+id, validHeaderBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestActualizarHeader_NotFound(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	req := jsonRequest(t, http.MethodPatch, "/ventas/"+uuid.NewString(), validHeaderBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestActualizarHeader_InvalidBody(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	body := validHeaderBody()
	body.FechaVenta = "not-a-date"
	req := jsonRequest(t, http.MethodPatch, "/ventas/"+id, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestActualizarHeader_RejectsCancelada(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)

	// Cancel first.
	cancelReq := jsonRequest(t, http.MethodPatch, "/ventas/"+id+"/cancel",
		venthttp.CancelarVentaBody{Reason: "test"})
	cancelRec := httptest.NewRecorder()
	r.ServeHTTP(cancelRec, cancelReq)
	require.Equal(t, http.StatusOK, cancelRec.Code, cancelRec.Body.String())

	// Now try to edit.
	req := jsonRequest(t, http.MethodPatch, "/ventas/"+id, validHeaderBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

// ─── ActualizarCliente (PATCH /v2/ventas/{id}/cliente) ─────────────────────

func TestActualizarCliente_OK(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)

	req := jsonRequest(t, http.MethodPatch, "/ventas/"+id+"/cliente", validClienteBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	// Cliente nombre is folded to ALL CAPS by the domain (Microsip convention).
	assert.Equal(t, "CLIENTE EDITADO", out.Cliente.Nombre)
}

func TestActualizarCliente_PermissionDenied(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	rFull := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, rFull)
	r := buildRouter(t, svc, limitedPerms(uuid.New()))
	req := jsonRequest(t, http.MethodPatch, "/ventas/"+id+"/cliente", validClienteBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestActualizarCliente_NotFound(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	req := jsonRequest(t, http.MethodPatch, "/ventas/"+uuid.NewString()+"/cliente", validClienteBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestActualizarCliente_InvalidBody(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	body := validClienteBody()
	body.Cliente.Nombre = "" // empty → domain validation kicks in
	req := jsonRequest(t, http.MethodPatch, "/ventas/"+id+"/cliente", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

// ─── ReemplazarProductos (PUT /v2/ventas/{id}/productos) ───────────────────

func TestReemplazarProductos_OK(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)

	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/productos", validProductosBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Len(t, out.Productos, 1)
	assert.Equal(t, "Otro", out.Productos[0].Articulo)
}

func TestReemplazarProductos_PermissionDenied(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	rFull := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, rFull)
	r := buildRouter(t, svc, limitedPerms(uuid.New()))
	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/productos", validProductosBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestReemplazarProductos_NotFound(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	req := jsonRequest(t, http.MethodPut, "/ventas/"+uuid.NewString()+"/productos", validProductosBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestReemplazarProductos_EmptyRejected(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/productos",
		venthttp.ReemplazarProductosBody{Productos: []venthttp.ProductoDTO{}})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestReemplazarProductos_RejectsCancelada(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	cancelReq := jsonRequest(t, http.MethodPatch, "/ventas/"+id+"/cancel",
		venthttp.CancelarVentaBody{Reason: "test"})
	cancelRec := httptest.NewRecorder()
	r.ServeHTTP(cancelRec, cancelReq)
	require.Equal(t, http.StatusOK, cancelRec.Code)

	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/productos", validProductosBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

// ─── ReemplazarCombos (PUT /v2/ventas/{id}/combos) ─────────────────────────

func TestReemplazarCombos_OK(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/combos", validCombosBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Len(t, out.Combos, 1)
	assert.Equal(t, "Bundle", out.Combos[0].Nombre)
	assert.Equal(t, "2.0000", out.Combos[0].Cantidad)
}

func TestReemplazarCombos_PermissionDenied(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	rFull := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, rFull)
	r := buildRouter(t, svc, limitedPerms(uuid.New()))
	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/combos", validCombosBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ─── ReemplazarVendedores (PUT /v2/ventas/{id}/vendedores) ─────────────────

func TestReemplazarVendedores_OK(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/vendedores", validVendedoresBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Len(t, out.Vendedores, 1)
}

func TestReemplazarVendedores_PermissionDenied(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	rFull := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, rFull)
	r := buildRouter(t, svc, limitedPerms(uuid.New()))
	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/vendedores", validVendedoresBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestReemplazarVendedores_EmptyRejected(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/vendedores",
		venthttp.ReemplazarVendedoresBody{Vendedores: []venthttp.VendedorDTO{}})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

// ─── Path-param validation ─────────────────────────────────────────────────

func TestEditHandlers_InvalidUUIDInPath(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))

	cases := []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, "/ventas/not-a-uuid", validHeaderBody()},
		{http.MethodPatch, "/ventas/not-a-uuid/cliente", validClienteBody()},
		{http.MethodPut, "/ventas/not-a-uuid/productos", validProductosBody()},
		{http.MethodPut, "/ventas/not-a-uuid/combos", validCombosBody()},
		{http.MethodPut, "/ventas/not-a-uuid/vendedores", validVendedoresBody()},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			req := jsonRequest(t, tc.method, tc.path, tc.body)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
				"body=%s", rec.Body.String())
		})
	}
}
