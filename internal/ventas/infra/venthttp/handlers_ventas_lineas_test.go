//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// ─── body builders ──────────────────────────────────────────────────────────

// comboDTO builds one combo row for the lineas body.
func comboDTO(id, nombre string) venthttp.ComboDTO {
	return venthttp.ComboDTO{
		ID: id, Nombre: nombre,
		PrecioAnual: "3000", PrecioCorto: "2700", PrecioContado: "2400",
		Cantidad: "1", AlmacenOrigenID: 7, AlmacenDestinoID: 8,
	}
}

// comboChildDTO builds a producto row that belongs to comboID: no almacenes of
// its own, they are inherited from the parent combo.
func comboChildDTO(comboID string, articuloID int) venthttp.ProductoDTO {
	cid := comboID
	return venthttp.ProductoDTO{
		ID: uuid.NewString(), ArticuloID: articuloID, Articulo: "Hijo de combo",
		Cantidad: "2", PrecioAnual: "1000", PrecioCorto: "900", PrecioContado: "800",
		ComboID: &cid,
	}
}

// validLineasBody returns a coherent ReemplazarLineasBody: one combo plus its
// child producto. Exported-ish (package-level) because the security sweep in
// security_test.go reuses it.
func validLineasBody() venthttp.ReemplazarLineasBody {
	comboID := uuid.NewString()
	return venthttp.ReemplazarLineasBody{
		Combos:    []venthttp.ComboDTO{comboDTO(comboID, "Combo Recámara")},
		Productos: []venthttp.ProductoDTO{comboChildDTO(comboID, 88)},
	}
}

// seedVentaConComboViaHTTP creates a venta that already carries a combo and
// its child producto, so the follow-up edit reproduces the production case.
// Returns the venta id and the ORIGINAL combo id.
func seedVentaConComboViaHTTP(t *testing.T, r http.Handler) (string, string) {
	t.Helper()
	body := validCreateBody()
	comboID := uuid.NewString()
	body.Combos = []venthttp.ComboDTO{comboDTO(comboID, "Combo Original")}
	body.Productos = []venthttp.ProductoDTO{comboChildDTO(comboID, 77)}

	req := crearVentaMultipartRequest(t, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "seed venta con combo: %s", rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out.ID, comboID
}

// ─── happy paths ────────────────────────────────────────────────────────────

// TestReemplazarLineas_BorrarComboYCrearOtro_OK is the production case end to
// end over HTTP: one request replaces both collections, the combo is swapped
// for a brand-new id, and the response carries the final venta.
func TestReemplazarLineas_BorrarComboYCrearOtro_OK(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id, viejoComboID := seedVentaConComboViaHTTP(t, r)

	nuevoComboID := uuid.NewString()
	require.NotEqual(t, viejoComboID, nuevoComboID)
	body := venthttp.ReemplazarLineasBody{
		Combos:    []venthttp.ComboDTO{comboDTO(nuevoComboID, "Combo Nuevo")},
		Productos: []venthttp.ProductoDTO{comboChildDTO(nuevoComboID, 99)},
	}

	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/lineas", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Len(t, out.Combos, 1)
	require.Len(t, out.Productos, 1)
	assert.Equal(t, nuevoComboID, out.Combos[0].ID)
	assert.Equal(t, "Combo Nuevo", out.Combos[0].Nombre)
	require.NotNil(t, out.Productos[0].ComboID)
	assert.Equal(t, nuevoComboID, *out.Productos[0].ComboID)
	assert.Equal(t, 99, out.Productos[0].ArticuloID)
}

// TestReemplazarCombos_BorrarYCrear_Sigue422 is the control: the same edit
// through the deprecated single-collection endpoints still fails, in both
// orders, with the same code. This is what production has been hitting.
func TestReemplazarCombos_BorrarYCrear_Sigue422(t *testing.T) {
	t.Parallel()

	t.Run("PUT combos primero", func(t *testing.T) {
		t.Parallel()
		svc, _, _ := testService()
		r := buildRouter(t, svc, fullPerms(uuid.New()))
		id, _ := seedVentaConComboViaHTTP(t, r)
		body := venthttp.ReemplazarCombosBody{Combos: []venthttp.ComboDTO{comboDTO(uuid.NewString(), "Combo Nuevo")}}
		req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/combos", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		assertProblemCode(t, rec, http.StatusUnprocessableEntity, "producto_combo_referencia_invalida")
	})

	t.Run("PUT productos primero", func(t *testing.T) {
		t.Parallel()
		svc, _, _ := testService()
		r := buildRouter(t, svc, fullPerms(uuid.New()))
		id, _ := seedVentaConComboViaHTTP(t, r)
		body := venthttp.ReemplazarProductosBody{Productos: []venthttp.ProductoDTO{comboChildDTO(uuid.NewString(), 99)}}
		req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/productos", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		assertProblemCode(t, rec, http.StatusUnprocessableEntity, "producto_combo_referencia_invalida")
	})
}

// TestReemplazarLineas_CombosVacios_OK: dropping every combo is legal as long
// as the productos that survive are stand-alone.
func TestReemplazarLineas_CombosVacios_OK(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id, _ := seedVentaConComboViaHTTP(t, r)

	body := venthttp.ReemplazarLineasBody{
		Combos: []venthttp.ComboDTO{},
		Productos: []venthttp.ProductoDTO{{
			ID: uuid.NewString(), ArticuloID: 55, Articulo: "Producto Suelto",
			Cantidad: "1", PrecioAnual: "500", PrecioCorto: "450", PrecioContado: "400",
			AlmacenOrigenID: intPtr(1), AlmacenDestinoID: intPtr(2),
		}},
	}
	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/lineas", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Empty(t, out.Combos)
	require.Len(t, out.Productos, 1)
	assert.Nil(t, out.Productos[0].ComboID)
}

// ─── rejections ─────────────────────────────────────────────────────────────

// TestReemplazarLineas_ProductoHaciaComboAusente_422 pins the owner's
// decision: the API does not trust the desktop to send coherent collections.
func TestReemplazarLineas_ProductoHaciaComboAusente_422(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id, _ := seedVentaConComboViaHTTP(t, r)

	presente := uuid.NewString()
	body := venthttp.ReemplazarLineasBody{
		Combos: []venthttp.ComboDTO{comboDTO(presente, "Combo Presente")},
		Productos: []venthttp.ProductoDTO{
			comboChildDTO(presente, 1),
			comboChildDTO(uuid.NewString(), 2), // points nowhere
		},
	}
	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/lineas", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assertProblemCode(t, rec, http.StatusUnprocessableEntity, "producto_combo_referencia_invalida")
}

// TestReemplazarLineas_ProductosVacios_422: Huma's minItems on the body
// rejects an empty productos array before the handler runs.
func TestReemplazarLineas_ProductosVacios_422(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id, _ := seedVentaConComboViaHTTP(t, r)

	body := venthttp.ReemplazarLineasBody{
		Combos:    []venthttp.ComboDTO{},
		Productos: []venthttp.ProductoDTO{},
	}
	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/lineas", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

// TestReemplazarLineas_NoBorrador_409: a canceled venta is not editable.
func TestReemplazarLineas_NoBorrador_409(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id, _ := seedVentaConComboViaHTTP(t, r)

	cancelReq := jsonRequest(t, http.MethodPatch, "/ventas/"+id+"/cancel",
		venthttp.CancelarVentaBody{Reason: "cliente se arrepintió"})
	cancelRec := httptest.NewRecorder()
	r.ServeHTTP(cancelRec, cancelReq)
	require.Equal(t, http.StatusOK, cancelRec.Code, cancelRec.Body.String())

	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/lineas", validLineasBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assertProblemCode(t, rec, http.StatusConflict, "venta_no_editable")
}

// TestReemplazarLineas_Aprobada_409: an approved venta is not editable either.
func TestReemplazarLineas_Aprobada_409(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id, _ := seedVentaConComboViaHTTP(t, r)

	for _, path := range []string{"/ventas/" + id + "/revisar", "/ventas/" + id + "/aprobar"} {
		req := httptest.NewRequest(http.MethodPost, path, http.NoBody)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "%s: %s", path, rec.Body.String())
	}

	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/lineas", validLineasBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assertProblemCode(t, rec, http.StatusConflict, "venta_no_editable")
}

// TestReemplazarLineas_NotFound_404.
func TestReemplazarLineas_NotFound_404(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	req := jsonRequest(t, http.MethodPut, "/ventas/"+uuid.NewString()+"/lineas", validLineasBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assertProblemCode(t, rec, http.StatusNotFound, "venta_not_found")
}

// TestReemplazarLineas_SinPermiso_403: the endpoint requires ventas:editar —
// the same permission the two endpoints it replaces require, so no new grant
// is needed in production.
func TestReemplazarLineas_SinPermiso_403(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	rFull := buildRouter(t, svc, fullPerms(uuid.New()))
	id, _ := seedVentaConComboViaHTTP(t, rFull)

	r := buildRouter(t, svc, limitedPerms(uuid.New())) // has crear/ver/listar/cancelar, not editar
	req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/lineas", validLineasBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// TestReemplazarLineas_SinAutenticar_401: mounted without the CurrentUser
// planter, the in-handler defense-in-depth check refuses the request.
func TestReemplazarLineas_SinAutenticar_401(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouterNoAuth(t, svc)
	req := jsonRequest(t, http.MethodPut, "/ventas/"+uuid.NewString()+"/lineas", validLineasBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

// TestReemplazarLineas_IDInvalido_422.
func TestReemplazarLineas_IDInvalido_422(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	req := jsonRequest(t, http.MethodPut, "/ventas/no-es-uuid/lineas", validLineasBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

// ─── helpers ────────────────────────────────────────────────────────────────

// assertProblemCode asserts the response status and that the RFC 9457 problem
// document carries `code=<want>` among its errors — the machine-readable token
// the desktop branches on.
func assertProblemCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	require.Equal(t, wantStatus, rec.Code, rec.Body.String())
	var prob struct {
		Status int `json:"status"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prob))
	assert.Equal(t, wantStatus, prob.Status)
	found := false
	for _, e := range prob.Errors {
		if e.Message == "code="+wantCode {
			found = true
			break
		}
	}
	assert.True(t, found, "expected code=%s in problem errors; body=%s", wantCode, rec.Body.String())
}
