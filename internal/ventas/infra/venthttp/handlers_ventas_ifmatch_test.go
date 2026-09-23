//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// getVentaViaHTTP fetches the venta and returns its DTO, mainly to read the
// current version.
func getVentaViaHTTP(t *testing.T, r http.Handler, id string) venthttp.VentaDTO {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/ventas/"+id, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

// jsonRequestWithIfMatch is jsonRequest plus an If-Match header. An empty
// ifMatch omits the header entirely (Go's http.Header.Set with "" would
// still send an empty header, which is not the same as absent).
func jsonRequestWithIfMatch(t *testing.T, method, target string, body any, ifMatch string) *http.Request {
	t.Helper()
	req := jsonRequest(t, method, target, body)
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	return req
}

// quoted formats an int as the entity-tag If-Match value the docs promise:
// the version between quotes.
func quoted(v int) string { return strconv.Quote(strconv.Itoa(v)) }

// ─── version travels on read and on every edit response ───────────────────

func TestObtenerVenta_ReportsVersion(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)

	out := getVentaViaHTTP(t, r, id)
	assert.Equal(t, 0, out.Version, "a freshly-created venta has never been edited: version 0")
}

func TestActualizarHeader_ResponseCarriesIncrementedVersion(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	before := getVentaViaHTTP(t, r, id)

	req := jsonRequest(t, http.MethodPatch, "/ventas/"+id, validHeaderBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, before.Version+1, out.Version)
}

// ─── absent If-Match: unconditional write, exactly today's behavior ───────
// This is the desktop compatibility contract and the single most important
// test in this file: the desktop app never sends If-Match and must keep
// writing successfully, unconditionally, forever.

func TestActualizarHeader_SinIfMatch_EsIncondicionalYVersionSube(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)

	req := jsonRequest(t, http.MethodPatch, "/ventas/"+id, validHeaderBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// A second unconditional write against the now-stale in-memory state (the
	// caller never re-read) must ALSO succeed — this is the desktop's actual
	// usage pattern: patch, patch again, never carrying a precondition.
	req2 := jsonRequest(t, http.MethodPatch, "/ventas/"+id, validHeaderBody())
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code, rec2.Body.String())
}

// ─── If-Match with the current version: succeeds, version increments ──────

func TestActualizarHeader_IfMatchVersionVigente_OK(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	before := getVentaViaHTTP(t, r, id)

	req := jsonRequestWithIfMatch(t, http.MethodPatch, "/ventas/"+id, validHeaderBody(), quoted(before.Version))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, before.Version+1, out.Version)
}

// ─── If-Match with a stale version: 412, and nothing was written ──────────

func TestActualizarHeader_IfMatchVersionVieja_412YNadaEscrito(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	before := getVentaViaHTTP(t, r, id)

	req := jsonRequestWithIfMatch(t, http.MethodPatch, "/ventas/"+id, validHeaderBody(), quoted(before.Version+1))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusPreconditionFailed, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "venta_version_conflicto")

	// Nothing was written: the aggregate the server holds is unchanged.
	after := getVentaViaHTTP(t, r, id)
	assert.Equal(t, before, after, "a rejected write must leave the venta byte-for-byte as it was")
}

// ─── malformed If-Match: 422, a client mistake, not a race ────────────────

func TestActualizarHeader_IfMatchMalformado_422(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)

	cases := []string{"abc", "3", `""`, `"3.5"`}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			req := jsonRequestWithIfMatch(t, http.MethodPatch, "/ventas/"+id, validHeaderBody(), raw)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), "if_match_invalido")
		})
	}
}

// ─── guard order at the HTTP surface: estado antes que version ────────────
// A venta that already left borrador must answer 409 venta_no_editable, even
// carrying a correct If-Match — never 412. Otherwise the phone would retry a
// write that can never succeed.

func TestActualizarHeader_VentaRevisada_ConIfMatchCorrecto_409NoEditable(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	before := getVentaViaHTTP(t, r, id)

	revisarReq := httptest.NewRequest(http.MethodPost, "/ventas/"+id+"/revisar", nil)
	revisarRec := httptest.NewRecorder()
	r.ServeHTTP(revisarRec, revisarReq)
	require.Equal(t, http.StatusOK, revisarRec.Code, revisarRec.Body.String())

	afterRevisar := getVentaViaHTTP(t, r, id)
	require.Greater(t, afterRevisar.Version, before.Version, "revisar must itself bump the version")

	// If-Match carries the CURRENT (post-revisar) version — a correct
	// precondition — yet the write must still be rejected for the state, not
	// accepted, and not rejected as a version conflict.
	req := jsonRequestWithIfMatch(t, http.MethodPatch, "/ventas/"+id, validHeaderBody(), quoted(afterRevisar.Version))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "venta_no_editable")
	assert.NotContains(t, rec.Body.String(), "venta_version_conflicto")
}

// ─── the other three phone-facing endpoints wire If-Match the same way ────
// Spot checks, not full coverage of every guard — ActualizarHeader above
// already proves the full contract; these confirm the wiring reaches
// ActualizarCliente, ReemplazarLineas and ReemplazarVendedores too.

func TestActualizarCliente_IfMatchVersionVieja_412(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	before := getVentaViaHTTP(t, r, id)

	req := jsonRequestWithIfMatch(t, http.MethodPatch, "/ventas/"+id+"/cliente", validClienteBody(), quoted(before.Version+1))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusPreconditionFailed, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "venta_version_conflicto")
}

func TestReemplazarLineas_IfMatchVersionVieja_412(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	before := getVentaViaHTTP(t, r, id)

	body := venthttp.ReemplazarLineasBody{Productos: validProductosBody().Productos}
	req := jsonRequestWithIfMatch(t, http.MethodPut, "/ventas/"+id+"/lineas", body, quoted(before.Version+1))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusPreconditionFailed, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "venta_version_conflicto")
}

func TestReemplazarVendedores_IfMatchVersionVieja_412(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	id := seedVentaViaHTTP(t, r)
	before := getVentaViaHTTP(t, r, id)

	req := jsonRequestWithIfMatch(t, http.MethodPut, "/ventas/"+id+"/vendedores", validVendedoresBody(), quoted(before.Version+1))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusPreconditionFailed, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "venta_version_conflicto")
}
