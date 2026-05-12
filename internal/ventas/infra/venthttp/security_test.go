//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"bytes"
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
