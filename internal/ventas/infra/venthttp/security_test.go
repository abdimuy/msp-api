//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// secProtectedRoute is one row of the authn/authz sweep table.
type secProtectedRoute struct {
	method       string
	path         string
	requiredPerm authdomain.Permission
	// bodyBuilder returns the request body + content-type for routes whose
	// Huma input validates a body. For GET/DELETE the function is nil.
	bodyBuilder func(t *testing.T) (io.Reader, string)
}

// jsonCancelBody is a valid CancelarVenta body.
func jsonCancelBody(t *testing.T) (io.Reader, string) {
	t.Helper()
	b, err := json.Marshal(map[string]string{"reason": "test"})
	require.NoError(t, err)
	return bytes.NewReader(b), "application/json"
}

// jsonHeaderEditBody is a valid ActualizarHeader body.
func jsonHeaderEditBody(t *testing.T) (io.Reader, string) {
	t.Helper()
	b, err := json.Marshal(validHeaderBody())
	require.NoError(t, err)
	return bytes.NewReader(b), "application/json"
}

// jsonClienteEditBody is a valid ActualizarCliente body.
func jsonClienteEditBody(t *testing.T) (io.Reader, string) {
	t.Helper()
	b, err := json.Marshal(validClienteBody())
	require.NoError(t, err)
	return bytes.NewReader(b), "application/json"
}

// jsonProductosEditBody is a valid ReemplazarProductos body.
func jsonProductosEditBody(t *testing.T) (io.Reader, string) {
	t.Helper()
	b, err := json.Marshal(validProductosBody())
	require.NoError(t, err)
	return bytes.NewReader(b), "application/json"
}

// jsonCombosEditBody is a valid ReemplazarCombos body.
func jsonCombosEditBody(t *testing.T) (io.Reader, string) {
	t.Helper()
	b, err := json.Marshal(validCombosBody())
	require.NoError(t, err)
	return bytes.NewReader(b), "application/json"
}

// jsonVendedoresEditBody is a valid ReemplazarVendedores body.
func jsonVendedoresEditBody(t *testing.T) (io.Reader, string) {
	t.Helper()
	b, err := json.Marshal(validVendedoresBody())
	require.NoError(t, err)
	return bytes.NewReader(b), "application/json"
}

// jsonCreateBody is a valid CrearVenta body.
func jsonCreateBody(t *testing.T) (io.Reader, string) {
	t.Helper()
	b, err := json.Marshal(validCreateBody())
	require.NoError(t, err)
	return bytes.NewReader(b), "application/json"
}

// multipartImageBody is a valid AdjuntarImagen multipart body.
func multipartImageBody(t *testing.T) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="x.jpg"`)
	hdr.Set("Content-Type", "image/jpeg")
	part, err := mw.CreatePart(hdr)
	require.NoError(t, err)
	_, _ = part.Write([]byte("fake-jpeg-bytes"))
	require.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}

// secProtectedRoutes enumerates every protected route the ventas router
// mounts. UUID segments are static placeholders so the table is reusable
// across the authn and authz sweeps.
var secProtectedRoutes = []secProtectedRoute{
	{http.MethodGet, "/ventas", authdomain.PermVentasListar, nil},
	{http.MethodGet, "/ventas/00000000-0000-0000-0000-000000000001", authdomain.PermVentasVer, nil},
	{http.MethodPost, "/ventas", authdomain.PermVentasCrear, jsonCreateBody},
	{http.MethodPatch, "/ventas/00000000-0000-0000-0000-000000000001/cancel", authdomain.PermVentasCancelar, jsonCancelBody},
	{http.MethodPatch, "/ventas/00000000-0000-0000-0000-000000000001", authdomain.PermVentasEditar, jsonHeaderEditBody},
	{http.MethodPatch, "/ventas/00000000-0000-0000-0000-000000000001/cliente", authdomain.PermVentasEditar, jsonClienteEditBody},
	{http.MethodPut, "/ventas/00000000-0000-0000-0000-000000000001/productos", authdomain.PermVentasEditar, jsonProductosEditBody},
	{http.MethodPut, "/ventas/00000000-0000-0000-0000-000000000001/combos", authdomain.PermVentasEditar, jsonCombosEditBody},
	{http.MethodPut, "/ventas/00000000-0000-0000-0000-000000000001/vendedores", authdomain.PermVentasEditar, jsonVendedoresEditBody},
	{http.MethodPost, "/ventas/00000000-0000-0000-0000-000000000001/imagenes", authdomain.PermVentasSubirImagenes, multipartImageBody},
	{http.MethodDelete, "/ventas/00000000-0000-0000-0000-000000000001/imagenes/00000000-0000-0000-0000-000000000002", authdomain.PermVentasEliminarImagenes, nil},
}

// secProblemBody is the minimal projection of a problem-details document we
// need for the assertions in the sweep.
type secProblemBody struct {
	Status int    `json:"status"`
	Detail string `json:"detail"`
	Title  string `json:"title"`
}

// buildSweepRequest constructs the HTTP request for one row of the sweep.
func buildSweepRequest(t *testing.T, rt secProtectedRoute) *http.Request {
	t.Helper()
	var (
		body io.Reader
		ct   string
	)
	if rt.bodyBuilder != nil {
		body, ct = rt.bodyBuilder(t)
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(rt.method, rt.path, body)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	return req
}

// TestSecurity_Authn_NoCurrentUser_Returns401 verifies that every protected
// ventas endpoint refuses to serve a request when no auth.CurrentUser is on
// the context. In production chi's authn middleware short-circuits at 401
// before Huma sees the request; this test exercises the in-handler
// defense-in-depth check by mounting the router WITHOUT the planter.
func TestSecurity_Authn_NoCurrentUser_Returns401(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	r := chi.NewRouter()
	venthttp.MountRouter(r, svc)

	for _, rt := range secProtectedRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			t.Parallel()
			req := buildSweepRequest(t, rt)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code, "want 401 for %s %s, got %d body=%s",
				rt.method, rt.path, rec.Code, rec.Body.String())

			var prob secProblemBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prob))
			assert.Equal(t, http.StatusUnauthorized, prob.Status)
		})
	}
}

// TestSecurity_Authz_MissingPerm_Returns403 plants a CurrentUser that holds
// every ventas permission EXCEPT the one each route requires, and asserts the
// handler rejects with 403.
func TestSecurity_Authz_MissingPerm_Returns403(t *testing.T) {
	t.Parallel()

	for _, rt := range secProtectedRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			t.Parallel()
			svc, _, _ := testService()

			full := []authdomain.Permission{
				authdomain.PermVentasListar,
				authdomain.PermVentasVer,
				authdomain.PermVentasCrear,
				authdomain.PermVentasCancelar,
				authdomain.PermVentasEditar,
				authdomain.PermVentasSubirImagenes,
				authdomain.PermVentasEliminarImagenes,
			}
			held := make([]string, 0, len(full)-1)
			for _, p := range full {
				if p == rt.requiredPerm {
					continue
				}
				held = append(held, string(p))
			}
			cu := auth.CurrentUser{ID: uuid.New(), Permisos: held}

			r := buildRouter(t, svc, cu)
			req := buildSweepRequest(t, rt)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.Equal(t, http.StatusForbidden, rec.Code, "want 403 for %s %s, got %d body=%s",
				rt.method, rt.path, rec.Code, rec.Body.String())
		})
	}
}

// TestSecurity_PathInjection_InvalidUUID_NoInternalError asserts that each
// route taking an `id` path parameter rejects non-UUID and SQL-injection-like
// input without producing a 500.
func TestSecurity_PathInjection_InvalidUUID_NoInternalError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/ventas/not-a-uuid"},
		{http.MethodPatch, "/ventas/not-a-uuid/cancel"},
		{http.MethodPatch, "/ventas/not-a-uuid"},
		{http.MethodPatch, "/ventas/not-a-uuid/cliente"},
		{http.MethodPut, "/ventas/not-a-uuid/productos"},
		{http.MethodPut, "/ventas/not-a-uuid/combos"},
		{http.MethodPut, "/ventas/not-a-uuid/vendedores"},
		{http.MethodDelete, "/ventas/not-a-uuid/imagenes/also-not"},
		// URL-encoded single-quote SQL-injection attempt embedded in the path.
		{http.MethodGet, "/ventas/%27%20OR%20%271%27%3D%271"},
	}

	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assert.NotEqual(t, http.StatusInternalServerError, rec.Code,
				"path injection produced 500 for %s %s: %s", tc.method, tc.path, rec.Body.String())
		})
	}
}

// ─── B6: defense-in-depth — body-borne injection attempts ─────────────────

// TestSecurity_JSONInjection_NombreInResponse pins how the API returns a
// nombre containing HTML/script characters. Two contracts:
//
//  1. The value MUST be returned inside a JSON string (between quotes),
//     so a correct JSON consumer can never accidentally execute it.
//  2. The Content-Type MUST be application/json — never text/html — so
//     browsers don't render the body.
//
// We deliberately do NOT require HTML-escaping of `<`/`>`/`&` in the JSON
// payload: Huma's encoder leaves those bytes literal (this is permitted
// by the JSON spec). The defense is correct content-type + structural
// containment, not byte-level escaping. A regression that flipped the
// content type to text/html would be the actual XSS exposure.
func TestSecurity_JSONInjection_NombreInResponse(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))

	body := validCreateBody()
	body.Cliente.Nombre = "<script>alert('xss')</script>"
	req := jsonRequest(t, http.MethodPost, "/ventas", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// Contract 1: response is application/json.
	ct := rec.Header().Get("Content-Type")
	assert.Contains(t, ct, "application/json",
		"response must be Content-Type: application/json (got %q)", ct)

	// Contract 2: payload is inside a JSON string. Parsing succeeds and
	// the nombre round-trips verbatim.
	var got venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got),
		"response must be valid JSON (injected chars must not break parsing)")
	assert.Equal(t, "<script>alert('xss')</script>", got.Cliente.Nombre,
		"value must round-trip exactly — preserved as user input, not executable HTML")
}

// TestSecurity_SQLInjection_InNotaField verifies that string fields are
// passed as parameters to Firebird, never concatenated into SQL — feeding
// the canonical SQL-injection payload via the nota field must result in
// the literal string being stored, with MSP_VENTAS still present.
//
//nolint:paralleltest // Firebird E2E tests are serial.
func TestSecurity_SQLInjection_InNotaField(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		injection := "'; DROP TABLE MSP_VENTAS; --"
		body.Nota = &injection
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		// Round-trip: nota stored as a literal string.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.NotNil(t, got.Nota)
		assert.Equal(t, injection, *got.Nota,
			"injection payload must be stored verbatim, not interpreted as SQL")

		// And the table itself is still there (verified by being able to GET
		// the row at all — if DROP had executed the FindByID above would
		// have failed with a table-not-found error from the driver).
	})
}
