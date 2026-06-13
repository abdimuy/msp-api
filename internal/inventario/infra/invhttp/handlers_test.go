//nolint:misspell // inventario vocabulary is Spanish (traspaso, almacén, artículo, etc.) per project convention.
package invhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	inventarioapp "github.com/abdimuy/msp-api/internal/inventario/app"
	inventariodomain "github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/inventario/infra/invhttp"
	inventariooutbound "github.com/abdimuy/msp-api/internal/inventario/ports/outbound"
)

// ─── fakes ──────────────────────────────────────────────────────────────────

// fakeTraspasoRepo is an in-memory TraspasoRepo.
type fakeTraspasoRepo struct {
	byDoctoIn map[int]*inventariodomain.Traspaso
	byVenta   map[uuid.UUID][]*inventariodomain.Traspaso
}

func newFakeTraspasoRepo() *fakeTraspasoRepo {
	return &fakeTraspasoRepo{
		byDoctoIn: map[int]*inventariodomain.Traspaso{},
		byVenta:   map[uuid.UUID][]*inventariodomain.Traspaso{},
	}
}

func (r *fakeTraspasoRepo) Save(_ context.Context, _ *inventariodomain.Traspaso) (int, error) {
	return 0, nil
}

func (r *fakeTraspasoRepo) FindByID(_ context.Context, doctoInID int) (*inventariodomain.Traspaso, error) {
	tr, ok := r.byDoctoIn[doctoInID]
	if !ok {
		return nil, inventariodomain.ErrTraspasoNoEncontrado
	}
	return tr, nil
}

func (r *fakeTraspasoRepo) ListByVentaID(_ context.Context, ventaID uuid.UUID) ([]*inventariodomain.Traspaso, error) {
	return r.byVenta[ventaID], nil
}

func (r *fakeTraspasoRepo) MarcarDirectoReversado(_ context.Context, _ int) error {
	return nil
}

// fakeExistenciaQuery is an in-memory ExistenciaQuery.
type fakeExistenciaQuery struct {
	stock map[[2]int]decimal.Decimal
}

func newFakeExistenciaQuery() *fakeExistenciaQuery {
	return &fakeExistenciaQuery{stock: map[[2]int]decimal.Decimal{}}
}

func (q *fakeExistenciaQuery) Existencia(_ context.Context, articuloID, almacenID int) (decimal.Decimal, error) {
	v, ok := q.stock[[2]int{articuloID, almacenID}]
	if !ok {
		return decimal.Zero, nil
	}
	return v, nil
}

func (q *fakeExistenciaQuery) ExistenciasPorAlmacen(_ context.Context, _ int) ([]inventariodomain.Existencia, error) {
	return nil, nil
}

// fakeAlmacenRepo is an in-memory AlmacenRepo.
type fakeAlmacenRepo struct {
	all []inventariodomain.Almacen
}

func newFakeAlmacenRepo(items []inventariodomain.Almacen) *fakeAlmacenRepo {
	return &fakeAlmacenRepo{all: items}
}

func (r *fakeAlmacenRepo) FindByID(_ context.Context, id int) (*inventariodomain.Almacen, error) {
	for i := range r.all {
		if r.all[i].ID == id {
			return &r.all[i], nil
		}
	}
	return nil, inventariodomain.ErrAlmacenNoEncontrado
}

func (r *fakeAlmacenRepo) ListAll(_ context.Context) ([]inventariodomain.Almacen, error) {
	return r.all, nil
}

// fakeFolioMinter always returns a fixed folio.
type fakeFolioMinter struct{}

func (fakeFolioMinter) MintFolio(_ context.Context) (inventariodomain.Folio, error) {
	return inventariodomain.NewFolio("MSA000001")
}

// fixedClock returns a constant time.
type fixedClock struct{ T time.Time }

func (c fixedClock) Now() time.Time { return c.T }

// noopOutbox swallows every Enqueue call.
type noopOutbox struct{}

func (noopOutbox) Enqueue(_ context.Context, _ string, _ uuid.UUID, _ string, _ any) error {
	return nil
}

// ─── test harness ───────────────────────────────────────────────────────────

// testComponents builds a service with all fakes wired.
func testComponents(t *testing.T) (
	*inventarioapp.Service,
	*fakeTraspasoRepo,
	*fakeExistenciaQuery,
	*fakeAlmacenRepo,
) {
	t.Helper()
	repo := newFakeTraspasoRepo()
	exist := newFakeExistenciaQuery()
	almacenes := newFakeAlmacenRepo([]inventariodomain.Almacen{
		inventariodomain.NewAlmacen(1, "Almacén Principal"),
		inventariodomain.NewAlmacen(2, "Almacén Secundario"),
	})
	clock := fixedClock{T: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)}
	svc := inventarioapp.NewService(repo, exist, fakeFolioMinter{}, almacenes, clock, noopOutbox{}, nil)
	return svc, repo, exist, almacenes
}

// planter is a chi middleware that plants cu on the request context.
func planter(cu auth.CurrentUser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.PlantCurrentUser(r.Context(), cu)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// fullPerms returns a CurrentUser holding every inventario permission.
func fullPerms() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-test",
		Email:       "tester@muebleriamsp.mx",
		Nombre:      "Tester",
		Permisos: []string{
			string(authdomain.PermInventarioVer),
			string(authdomain.PermTraspasosVer),
			string(authdomain.PermStockConsultar),
		},
	}
}

// buildRouter mounts the inventario routes with a planted CurrentUser.
func buildRouter(t *testing.T, svc *inventarioapp.Service, cu auth.CurrentUser) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Use(planter(cu))
	invhttp.MountRouter(r, svc)
	return r
}

// buildRouterNoAuth mounts without planting a CurrentUser.
func buildRouterNoAuth(t *testing.T, svc *inventarioapp.Service) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	invhttp.MountRouter(r, svc)
	return r
}

// seedTraspaso creates a canned Traspaso hydrated into the fake repo under
// DOCTO_IN_ID=101. Returns the doctoInID (101) and the ventaID it is linked to.
func seedTraspaso(t *testing.T, repo *fakeTraspasoRepo) (int, uuid.UUID) {
	t.Helper()
	ventaID := uuid.New()
	folio, err := inventariodomain.NewFolio("MSA000001")
	require.NoError(t, err)
	det := inventariodomain.HydrateDetalle(inventariodomain.HydrateDetalleParams{
		ID:         uuid.New(),
		ArticuloID: 42,
		Cantidad:   inventariodomain.HydrateCantidad(decimal.NewFromFloat(1)),
	})
	tr := inventariodomain.HydrateTraspaso(inventariodomain.HydrateTraspasoParams{
		ID:             uuid.New(),
		Folio:          folio,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC),
		Descripcion:    "Traspaso de prueba",
		VentaID:        &ventaID,
		TipoReverso:    false,
		DoctoInID:      intPtr(101),
		Detalles:       []*inventariodomain.TraspasoDetalle{det},
		CreatedAt:      time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
	const fixedDoctoInID = 101
	repo.byDoctoIn[fixedDoctoInID] = tr
	repo.byVenta[ventaID] = append(repo.byVenta[ventaID], tr)
	return fixedDoctoInID, ventaID
}

func intPtr(v int) *int { return &v }

// ─── ObtenerTraspaso ────────────────────────────────────────────────────────

func TestObtenerTraspaso_OK(t *testing.T) {
	t.Parallel()

	svc, repo, _, _ := testComponents(t)
	doctoInID, _ := seedTraspaso(t, repo)
	r := buildRouter(t, svc, fullPerms())

	req := httptest.NewRequest(http.MethodGet, "/traspasos/101", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out invhttp.TraspasoResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, doctoInID, *out.DoctoInID)
	assert.Equal(t, 1, out.AlmacenOrigen)
	assert.Equal(t, 2, out.AlmacenDestino)
	assert.Equal(t, "MSA000001", out.Folio)
	require.Len(t, out.Detalles, 1)
	assert.Equal(t, 42, out.Detalles[0].ArticuloID)
	assert.Equal(t, "1.0000", out.Detalles[0].Cantidad)
}

func TestObtenerTraspaso_NotFound(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := buildRouter(t, svc, fullPerms())

	req := httptest.NewRequest(http.MethodGet, "/traspasos/999", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestObtenerTraspaso_Unauthenticated(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := buildRouterNoAuth(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/traspasos/101", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestObtenerTraspaso_MissingPerm_Returns403(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	cu := auth.CurrentUser{
		ID:       uuid.New(),
		Permisos: []string{string(authdomain.PermInventarioVer)}, // missing PermTraspasosVer
	}
	r := buildRouter(t, svc, cu)

	req := httptest.NewRequest(http.MethodGet, "/traspasos/101", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestObtenerTraspaso_BadPathParam_Returns400(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := buildRouter(t, svc, fullPerms())

	req := httptest.NewRequest(http.MethodGet, "/traspasos/abc", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Huma should reject a non-integer path param before it reaches the handler.
	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
}

// ─── ListarTraspasosPorVenta ─────────────────────────────────────────────────

func TestListarTraspasosPorVenta_OK(t *testing.T) {
	t.Parallel()

	svc, repo, _, _ := testComponents(t)
	_, ventaID := seedTraspaso(t, repo)
	r := buildRouter(t, svc, fullPerms())

	req := httptest.NewRequest(http.MethodGet, "/traspasos?venta_id="+ventaID.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		Items []invhttp.TraspasoResponse `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, ventaID.String(), *out.Items[0].VentaID)
}

func TestListarTraspasosPorVenta_EmptyResult(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := buildRouter(t, svc, fullPerms())

	req := httptest.NewRequest(http.MethodGet, "/traspasos?venta_id="+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		Items []invhttp.TraspasoResponse `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Empty(t, out.Items)
}

func TestListarTraspasosPorVenta_MissingVentaID_Returns400(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := buildRouter(t, svc, fullPerms())

	req := httptest.NewRequest(http.MethodGet, "/traspasos", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Huma enforces required:"true" on venta_id, so missing param → 422.
	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
}

func TestListarTraspasosPorVenta_MalformedUUID_Returns422(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := buildRouter(t, svc, fullPerms())

	req := httptest.NewRequest(http.MethodGet, "/traspasos?venta_id=not-a-uuid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
}

func TestListarTraspasosPorVenta_Unauthenticated(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := buildRouterNoAuth(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/traspasos?venta_id="+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ─── ConsultarStock ──────────────────────────────────────────────────────────

func TestConsultarStock_OK(t *testing.T) {
	t.Parallel()

	svc, _, exist, _ := testComponents(t)
	exist.stock[[2]int{42, 1}] = decimal.NewFromFloat(5.5)
	r := buildRouter(t, svc, fullPerms())

	req := httptest.NewRequest(http.MethodGet, "/inventario/stock?articulo_id=42&almacen_id=1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out invhttp.StockResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, 42, out.ArticuloID)
	assert.Equal(t, 1, out.AlmacenID)
	assert.Equal(t, "5.5000", out.Cantidad)
}

func TestConsultarStock_ZeroStock(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := buildRouter(t, svc, fullPerms())

	req := httptest.NewRequest(http.MethodGet, "/inventario/stock?articulo_id=99&almacen_id=2", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out invhttp.StockResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, "0.0000", out.Cantidad)
}

func TestConsultarStock_MissingParam_Returns400(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := buildRouter(t, svc, fullPerms())

	cases := []string{
		"/inventario/stock",
		"/inventario/stock?articulo_id=42",
		"/inventario/stock?almacen_id=1",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assert.GreaterOrEqual(t, rec.Code, 400)
			assert.Less(t, rec.Code, 500)
		})
	}
}

func TestConsultarStock_Unauthenticated(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := buildRouterNoAuth(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/inventario/stock?articulo_id=1&almacen_id=1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestConsultarStock_MissingPerm_Returns403(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	cu := auth.CurrentUser{
		ID:       uuid.New(),
		Permisos: []string{string(authdomain.PermInventarioVer)}, // missing PermStockConsultar
	}
	r := buildRouter(t, svc, cu)

	req := httptest.NewRequest(http.MethodGet, "/inventario/stock?articulo_id=1&almacen_id=1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ─── ListarAlmacenes ─────────────────────────────────────────────────────────

func TestListarAlmacenes_OK(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := buildRouter(t, svc, fullPerms())

	req := httptest.NewRequest(http.MethodGet, "/inventario/almacenes", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		Items []invhttp.AlmacenResponse `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Len(t, out.Items, 2)
	assert.Equal(t, 1, out.Items[0].ID)
	assert.Equal(t, "Almacén Principal", out.Items[0].Nombre)
}

func TestListarAlmacenes_Unauthenticated(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := buildRouterNoAuth(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/inventario/almacenes", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListarAlmacenes_MissingPerm_Returns403(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	cu := auth.CurrentUser{
		ID:       uuid.New(),
		Permisos: []string{string(authdomain.PermTraspasosVer)}, // missing PermInventarioVer
	}
	r := buildRouter(t, svc, cu)

	req := httptest.NewRequest(http.MethodGet, "/inventario/almacenes", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ─── OpenAPI registration ────────────────────────────────────────────────────

func TestOpenAPI_PathsRegistered(t *testing.T) {
	t.Parallel()

	svc, _, _, _ := testComponents(t)
	r := chi.NewRouter()
	invhttp.MountRouter(r, svc)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	doc := rec.Body.String()
	for _, want := range []string{
		"/traspasos/{id}",
		"/traspasos",
		"/inventario/stock",
		"/inventario/almacenes",
		"bearerAuth",
	} {
		assert.Contains(t, doc, want, "expected openapi to contain %q", want)
	}
}

// ─── Compile-time assertions ─────────────────────────────────────────────────

// TestInventarioOutboundInterfaces verifies the fake types satisfy the
// outbound ports at compile time.
var (
	_ inventariooutbound.TraspasoRepo    = (*fakeTraspasoRepo)(nil)
	_ inventariooutbound.ExistenciaQuery = (*fakeExistenciaQuery)(nil)
	_ inventariooutbound.AlmacenRepo     = (*fakeAlmacenRepo)(nil)
	_ inventariooutbound.Clock           = fixedClock{}
	_ inventariooutbound.OutboxEnqueuer  = noopOutbox{}
	_ inventariooutbound.FolioMinter     = fakeFolioMinter{}
)
