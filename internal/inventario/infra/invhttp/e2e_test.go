//nolint:misspell // inventario vocabulary is Spanish (traspaso, almacén, artículo, etc.) per project convention.
package invhttp_test

// e2e_test.go contains end-to-end tests that exercise the full HTTP stack
// (chi router + Huma middlewares + authn/authz + handler + app service) with
// in-memory fakes as the data layer. They verify that the composition of all
// layers produces the expected HTTP status codes for authorized requests.
//
// Tests that require a real Firebird database belong in a separate
// e2e_firebird_test.go file (see the ventas module for the pattern).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/inventario/infra/invhttp"
)

// buildE2ERouter assembles the full chi + Huma stack as it would appear at
// the composition root (cmd/api/server.go), substituting in-memory fakes
// for every I/O adapter.
func buildE2ERouter(t *testing.T, cu auth.CurrentUser) (http.Handler, *fakeTraspasoRepo, *fakeExistenciaQuery) {
	t.Helper()
	svc, repo, exist, _ := testComponents(t)
	r := chi.NewRouter()
	r.Use(planter(cu))
	invhttp.MountRouter(r, svc)
	return r, repo, exist
}

// e2eFullPerms returns a CurrentUser with every inventario permission.
func e2eFullPerms() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-e2e",
		Email:       "admin@muebleriamsp.mx",
		Nombre:      "Admin E2E",
		Permisos: []string{
			string(authdomain.PermInventarioVer),
			string(authdomain.PermTraspasosVer),
			string(authdomain.PermStockConsultar),
		},
	}
}

// TestE2E_Inventario_ObtenerTraspaso_FullStack verifies that a properly
// authorized GET /traspasos/{id} returns 200 through the full stack.
func TestE2E_Inventario_ObtenerTraspaso_FullStack(t *testing.T) {
	t.Parallel()

	r, repo, _ := buildE2ERouter(t, e2eFullPerms())
	_, _ = seedTraspaso(t, repo)

	req := httptest.NewRequest(http.MethodGet, "/traspasos/101", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out invhttp.TraspasoResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.NotEmpty(t, out.ID)
	assert.Equal(t, "MSA000001", out.Folio)
}

// TestE2E_Inventario_ListarTraspasosPorVenta_FullStack verifies a properly
// authorized GET /traspasos?venta_id returns 200.
func TestE2E_Inventario_ListarTraspasosPorVenta_FullStack(t *testing.T) {
	t.Parallel()

	r, repo, _ := buildE2ERouter(t, e2eFullPerms())
	_, ventaID := seedTraspaso(t, repo)

	req := httptest.NewRequest(http.MethodGet, "/traspasos?venta_id="+ventaID.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		Items []invhttp.TraspasoResponse `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Len(t, out.Items, 1)
}

// TestE2E_Inventario_ConsultarStock_FullStack verifies a properly authorized
// GET /inventario/stock returns 200.
func TestE2E_Inventario_ConsultarStock_FullStack(t *testing.T) {
	t.Parallel()

	r, _, exist := buildE2ERouter(t, e2eFullPerms())
	exist.stock[[2]int{10, 1}] = decimal.NewFromFloat(3.0)

	req := httptest.NewRequest(http.MethodGet, "/inventario/stock?articulo_id=10&almacen_id=1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out invhttp.StockResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, "3.0000", out.Cantidad)
}

// TestE2E_Inventario_ListarAlmacenes_FullStack verifies a properly authorized
// GET /inventario/almacenes returns 200.
func TestE2E_Inventario_ListarAlmacenes_FullStack(t *testing.T) {
	t.Parallel()

	r, _, _ := buildE2ERouter(t, e2eFullPerms())

	req := httptest.NewRequest(http.MethodGet, "/inventario/almacenes", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		Items []invhttp.AlmacenResponse `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.NotEmpty(t, out.Items)
}
