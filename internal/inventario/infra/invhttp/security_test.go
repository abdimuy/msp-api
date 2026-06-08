//nolint:misspell // inventario vocabulary is Spanish (traspaso, almacén, artículo, etc.) per project convention.
package invhttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/inventario/infra/invhttp"
)

// secRoute is one row of the security sweep table.
type secRoute struct {
	method       string
	path         string
	requiredPerm authdomain.Permission
}

// secProtectedRoutes enumerates every protected inventario route.
// Integer path params use a valid value (101) so Huma's type-coercion does
// not reject them before the auth check runs.
var secProtectedRoutes = []secRoute{
	{http.MethodGet, "/traspasos/101", authdomain.PermTraspasosVer},
	{http.MethodGet, "/traspasos?venta_id=00000000-0000-0000-0000-000000000001", authdomain.PermTraspasosVer},
	{http.MethodGet, "/inventario/stock?articulo_id=1&almacen_id=1", authdomain.PermStockConsultar},
	{http.MethodGet, "/inventario/almacenes", authdomain.PermInventarioVer},
}

// secProblemBody is the minimal projection of a problem-details document.
type secProblemBody struct {
	Status int    `json:"status"`
	Detail string `json:"detail"`
	Title  string `json:"title"`
}

// TestSecurity_Authn_NoCurrentUser_Returns401 verifies that every protected
// inventario endpoint refuses to serve an unauthenticated request.
func TestSecurity_Authn_NoCurrentUser_Returns401(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := chi.NewRouter()
	invhttp.MountRouter(r, svc)

	for _, rt := range secProtectedRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(""))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code,
				"want 401 for %s %s, got %d body=%s", rt.method, rt.path, rec.Code, rec.Body.String())

			var prob secProblemBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prob))
			assert.Equal(t, http.StatusUnauthorized, prob.Status)
		})
	}
}

// TestSecurity_Authz_MissingPerm_Returns403 plants a CurrentUser that holds
// every inventario permission EXCEPT the one the route requires.
func TestSecurity_Authz_MissingPerm_Returns403(t *testing.T) {
	t.Parallel()

	allPerms := []authdomain.Permission{
		authdomain.PermInventarioVer,
		authdomain.PermTraspasosVer,
		authdomain.PermStockConsultar,
	}

	for _, rt := range secProtectedRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			t.Parallel()
			svc, _, _, _ := testComponents(t)

			held := make([]string, 0, len(allPerms)-1)
			for _, p := range allPerms {
				if p == rt.requiredPerm {
					continue
				}
				held = append(held, string(p))
			}
			cu := auth.CurrentUser{ID: uuid.New(), Permisos: held}

			r := buildRouter(t, svc, cu)
			req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(""))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.Equal(t, http.StatusForbidden, rec.Code,
				"want 403 for %s %s, got %d body=%s", rt.method, rt.path, rec.Code, rec.Body.String())
		})
	}
}

// TestSecurity_PathInjection_NonIntID asserts that the route with an integer
// path parameter rejects non-numeric and injection-like input without 5xx.
func TestSecurity_PathInjection_NonIntID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/traspasos/abc"},
		{http.MethodGet, "/traspasos/not-a-number"},
		{http.MethodGet, "/traspasos/-1"},
		// URL-encoded single-quote SQL-injection attempt.
		{http.MethodGet, "/traspasos/%27%20OR%20%271%27%3D%271"},
	}

	svc, _, _, _ := testComponents(t)
	r := buildRouter(t, svc, fullPerms())

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(""))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assert.NotEqual(t, http.StatusInternalServerError, rec.Code,
				"path injection must not produce 500 for %s %s: %s", tc.method, tc.path, rec.Body.String())
		})
	}
}
