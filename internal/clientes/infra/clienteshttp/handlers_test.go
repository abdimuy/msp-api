//nolint:misspell // clientes vocabulary is Spanish per project convention.
package clienteshttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/auth"
	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/infra/clienteshttp"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ─── Fixed timestamps ─────────────────────────────────────────────────────────

var (
	fixedNow   = time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	fixedFecha = time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	fixedPago  = time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
)

// ─── Fixed clock ──────────────────────────────────────────────────────────────

type testClock struct{}

func (testClock) Now() time.Time { return fixedNow }

// ─── Fake repo ────────────────────────────────────────────────────────────────

type fakeRepo struct {
	cliente    *domain.Cliente
	clienteErr error

	resumen    outbound.ResumenFicha
	resumenErr error

	ventasPage outbound.Page[*domain.VentaCliente]
	ventasErr  error

	detalleByID map[int]outbound.VentaDetalle
	detalleErr  error

	dirCompleto []outbound.DirectorioItem
	dirComplErr error

	basicIDs []int
	basicErr error

	docs    []outbound.SearchDoc
	docsErr error
}

func (f *fakeRepo) ObtenerCliente(_ context.Context, _ int) (*domain.Cliente, error) {
	if f.clienteErr != nil {
		return nil, f.clienteErr
	}
	return f.cliente, nil
}

func (f *fakeRepo) ObtenerResumenFicha(_ context.Context, _ int) (outbound.ResumenFicha, error) {
	if f.resumenErr != nil {
		return outbound.ResumenFicha{}, f.resumenErr
	}
	return f.resumen, nil
}

func (f *fakeRepo) ListarVentas(_ context.Context, _ int, _ outbound.ListParams) (outbound.Page[*domain.VentaCliente], error) {
	if f.ventasErr != nil {
		return outbound.Page[*domain.VentaCliente]{}, f.ventasErr
	}
	return f.ventasPage, nil
}

func (f *fakeRepo) ObtenerVentaDetalle(_ context.Context, doctoPVID int) (outbound.VentaDetalle, error) {
	if f.detalleErr != nil {
		return outbound.VentaDetalle{}, f.detalleErr
	}
	d, ok := f.detalleByID[doctoPVID]
	if !ok {
		return outbound.VentaDetalle{}, domain.ErrVentaNotFound
	}
	return d, nil
}

func (f *fakeRepo) ListarDirectorioCompleto(_ context.Context, _ outbound.FiltroDirectorio) ([]outbound.DirectorioItem, error) {
	if f.dirComplErr != nil {
		return nil, f.dirComplErr
	}
	return f.dirCompleto, nil
}

func (f *fakeRepo) BuscarClienteIDsBasico(_ context.Context, _ string, _ int) ([]int, error) {
	if f.basicErr != nil {
		return nil, f.basicErr
	}
	return f.basicIDs, nil
}

func (f *fakeRepo) LeerDocumentosBusqueda(_ context.Context) ([]outbound.SearchDoc, error) {
	if f.docsErr != nil {
		return nil, f.docsErr
	}
	return f.docs, nil
}

// ─── Fake analytics client ────────────────────────────────────────────────────

type fakeAnalytics struct {
	pulsos   map[int]analytics.ClientePulsoContract
	pulsoErr error
}

func (f *fakeAnalytics) ObtenerPulso(_ context.Context, clienteID int) (analytics.ClientePulsoContract, bool, error) {
	if f.pulsoErr != nil {
		return analytics.ClientePulsoContract{}, false, f.pulsoErr
	}
	p, ok := f.pulsos[clienteID]
	return p, ok, nil
}

func (f *fakeAnalytics) ObtenerPulsos(_ context.Context, ids []int) (map[int]analytics.ClientePulsoContract, error) {
	if f.pulsoErr != nil {
		return nil, f.pulsoErr
	}
	result := make(map[int]analytics.ClientePulsoContract, len(ids))
	for _, id := range ids {
		if p, ok := f.pulsos[id]; ok {
			result[id] = p
		}
	}
	return result, nil
}

// ─── Fake search index ────────────────────────────────────────────────────────

type fakeSearch struct {
	ready        bool
	reconcileErr error
	captured     []outbound.SearchDoc
}

func (f *fakeSearch) EstaListo() bool { return f.ready }

func (f *fakeSearch) Buscar(_ context.Context, _ string, _ int) ([]int, error) {
	return nil, nil
}

func (f *fakeSearch) Reconciliar(_ context.Context, docs []outbound.SearchDoc) error {
	f.captured = docs
	return f.reconcileErr
}

// ─── Fake directory index ─────────────────────────────────────────────────────

// noopDirectoryIndex is a test stub that satisfies outbound.DirectoryIndex.
// Buscar returns the configured result/error; Reconciliar is a no-op.
type noopDirectoryIndex struct {
	resultado outbound.DirectorioResultado
	buscarErr error
}

func (n noopDirectoryIndex) Buscar(_ context.Context, _ outbound.DirectorioQuery) (outbound.DirectorioResultado, error) {
	if n.buscarErr != nil {
		return outbound.DirectorioResultado{}, n.buscarErr
	}
	return n.resultado, nil
}

func (noopDirectoryIndex) Reconciliar(_ context.Context, _ []outbound.DirectorioDoc) error {
	return nil
}

// ─── Service builder ──────────────────────────────────────────────────────────

func buildService(repo outbound.ClientesRepo, ac outbound.AnalyticsClient, si outbound.SearchIndex) *clientesapp.Service {
	return buildServiceWithDirIndex(repo, ac, si, noopDirectoryIndex{})
}

func buildServiceWithDirIndex(repo outbound.ClientesRepo, ac outbound.AnalyticsClient, si outbound.SearchIndex, di outbound.DirectoryIndex) *clientesapp.Service {
	return clientesapp.NewService(repo, ac, si, di, testClock{})
}

// ─── Auth helpers ─────────────────────────────────────────────────────────────

// userWith returns an auth.CurrentUser holding the given permissions.
func userWith(perms ...auth.Permission) auth.CurrentUser {
	codes := make([]string, len(perms))
	for i, p := range perms {
		codes[i] = string(p)
	}
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-test-1",
		Email:       "tester@muebleriamsp.mx",
		Nombre:      "Analista Test",
		Permisos:    codes,
	}
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

// buildRouter mounts MountRouter behind a planter that authenticates as cu.
func buildRouter(svc *clientesapp.Service, cu auth.CurrentUser) http.Handler {
	r := chi.NewRouter()
	r.Use(planter(cu))
	clienteshttp.MountRouter(r, svc)
	return r
}

// buildRouterNoAuth mounts MountRouter without planting any CurrentUser.
func buildRouterNoAuth(svc *clientesapp.Service) http.Handler {
	r := chi.NewRouter()
	clienteshttp.MountRouter(r, svc)
	return r
}

// doJSON issues a request through h and returns the recorder.
func doJSON(h http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			panic("doJSON: marshal: " + err.Error())
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ─── Test data helpers ────────────────────────────────────────────────────────

func newCliente(id int) *domain.Cliente {
	return domain.HydrateCliente(domain.HydrateClienteParams{
		ClienteID:      id,
		Nombre:         "García López Ramón",
		LimiteCredito:  decimal.NewFromInt(50000),
		Notas:          "cliente frecuente",
		Estatus:        "A",
		ZonaClienteID:  1,
		ZonaNombre:     "NORTE",
		CobradorID:     5,
		CobradorNombre: "Martínez Reyes",
		Direccion: domain.HydrateDireccion(domain.HydrateDireccionParams{
			Calle:     "Av. Juárez 123",
			Colonia:   "Centro",
			Poblacion: "Guadalajara",
			Estado:    "Jalisco",
		}),
		Telefono: "3312345678",
	})
}

func newVenta(doctoID, clienteID int) *domain.VentaCliente {
	return domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{
		DoctoPVID:  doctoID,
		ClienteID:  clienteID,
		Fecha:      fixedFecha,
		Folio:      "PV-0042",
		Tipo:       domain.TipoVentaCredito,
		Total:      decimal.NewFromInt(12500),
		SaldoVenta: decimal.NewFromInt(8000),
		NumPagos:   3,
	})
}

func newPulso(clienteID int) analytics.ClientePulsoContract {
	return analytics.ClientePulsoContract{
		ClienteID:         clienteID,
		Score:             75,
		Segmento:          "ACTIVO",
		EstadoPago:        "AL_CORRIENTE",
		RecenciaDias:      45,
		Frecuencia:        8,
		Monetary:          decimal.NewFromInt(95000),
		Saldo:             decimal.NewFromInt(8000),
		PorLiquidarPct:    decimal.NewFromFloat(64.0),
		FechaUltimaCompra: fixedFecha,
		FechaUltimoPago:   fixedPago,
		NextBestProduct:   "Comedor Veracruz",
	}
}

// ─── Scenario 1: GET /clientes — happy path ───────────────────────────────────

func TestListarClientes_HappyPath_200(t *testing.T) {
	t.Parallel()

	doc := outbound.DirectorioDoc{
		ClienteID:      42,
		Nombre:         "García López Ramón",
		ZonaNombre:     "NORTE",
		Telefono:       "3312345678",
		DireccionCorta: "Av. Juárez 123, Centro",
		TienePulso:     true,
		Score:          75,
		Segmento:       "ACTIVO",
		EstadoPago:     "AL_CORRIENTE",
		RecenciaDias:   45,
		Saldo:          decimal.NewFromInt(8000),
	}
	facets := map[string]map[string]int{
		"zona_id": {"1": 200},
	}
	di := noopDirectoryIndex{
		resultado: outbound.DirectorioResultado{
			Items:  []outbound.DirectorioDoc{doc},
			Facets: facets,
			Total:  1,
		},
	}

	svc := buildServiceWithDirIndex(&fakeRepo{}, &fakeAnalytics{}, &fakeSearch{}, di)
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes", nil)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp struct {
		Items []struct {
			ClienteID    int    `json:"cliente_id"`
			Nombre       string `json:"nombre"`
			Zona         string `json:"zona"`
			Score        int    `json:"score"`
			Segmento     string `json:"segmento"`
			EstadoPago   string `json:"estado_pago"`
			TienePulso   bool   `json:"tiene_pulso"`
			RecenciaDias int    `json:"recencia_dias"`
			Saldo        string `json:"saldo"`
		} `json:"items"`
		NextCursor string                    `json:"next_cursor"`
		Facets     map[string]map[string]int `json:"facets"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Items, 1)

	it := resp.Items[0]
	assert.Equal(t, 42, it.ClienteID)
	assert.Equal(t, "García López Ramón", it.Nombre)
	assert.Equal(t, "NORTE", it.Zona)
	assert.Equal(t, 75, it.Score)
	assert.Equal(t, "ACTIVO", it.Segmento)
	assert.Equal(t, "AL_CORRIENTE", it.EstadoPago)
	assert.True(t, it.TienePulso)
	assert.Equal(t, 45, it.RecenciaDias)
	assert.Equal(t, "8000.00", it.Saldo)
	assert.Equal(t, 200, resp.Facets["zona_id"]["1"])
}

// TestListarClientes_MeilisearchUnavailable_503 verifies the directory has NO
// SQL fallback: when the search index returns a service-unavailable apperror,
// the endpoint surfaces HTTP 503 instead of degrading to a partial result.
func TestListarClientes_MeilisearchUnavailable_503(t *testing.T) {
	t.Parallel()

	di := noopDirectoryIndex{
		buscarErr: apperror.NewServiceUnavailable(
			"directorio_search_unavailable",
			"el directorio no está disponible temporalmente",
		),
	}

	svc := buildServiceWithDirIndex(&fakeRepo{}, &fakeAnalytics{}, &fakeSearch{}, di)
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes", nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, "body: %s", rec.Body.String())
}

// TestListarClientes_NoPulso_ZeroScoreAndEmptySegmento verifies that when the
// document has no pulse, score=0, segmento="" and tiene_pulso=false.
func TestListarClientes_NoPulso_ZeroScoreAndEmptySegmento(t *testing.T) {
	t.Parallel()

	doc := outbound.DirectorioDoc{
		ClienteID:  7,
		Nombre:     "Sin Pulso",
		TienePulso: false,
		Saldo:      decimal.Zero,
	}
	di := noopDirectoryIndex{
		resultado: outbound.DirectorioResultado{
			Items: []outbound.DirectorioDoc{doc},
			Total: 1,
		},
	}
	svc := buildServiceWithDirIndex(&fakeRepo{}, &fakeAnalytics{}, &fakeSearch{}, di)
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Items []struct {
			Score      int    `json:"score"`
			Segmento   string `json:"segmento"`
			TienePulso bool   `json:"tiene_pulso"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Items, 1)

	it := resp.Items[0]
	assert.Equal(t, 0, it.Score)
	assert.Empty(t, it.Segmento)
	assert.False(t, it.TienePulso)
}

// ─── Scenario 2: GET /clientes/{id} — ficha ──────────────────────────────────

func TestObtenerFicha_HappyPath_200(t *testing.T) {
	t.Parallel()

	c := newCliente(42)
	pulso := newPulso(42)
	resumen := outbound.ResumenFicha{
		TotalComprado:  decimal.NewFromInt(95000),
		TotalAbonado:   decimal.NewFromInt(87000),
		SaldoTotal:     decimal.NewFromInt(8000),
		PctLiquidado:   decimal.NewFromFloat(91.58),
		NumVentas:      8,
		NumPagos:       24,
		TicketPromedio: decimal.NewFromFloat(11875),
	}

	repo := &fakeRepo{
		cliente: c,
		resumen: resumen,
	}
	ac := &fakeAnalytics{pulsos: map[int]analytics.ClientePulsoContract{42: pulso}}
	svc := buildService(repo, ac, &fakeSearch{})
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes/42", nil)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp struct {
		ClienteID int    `json:"cliente_id"`
		Nombre    string `json:"nombre"`
		Telefono  string `json:"telefono"`
		Zona      string `json:"zona"`
		Cobrador  string `json:"cobrador"`
		Estatus   string `json:"estatus"`
		Resumen   struct {
			TotalComprado string `json:"total_comprado"`
			NumVentas     int    `json:"num_ventas"`
		} `json:"resumen"`
		Pulso *struct {
			Score             int    `json:"score"`
			Segmento          string `json:"segmento"`
			EstadoPago        string `json:"estado_pago"`
			Monetary          string `json:"monetary"`
			FechaUltimaCompra string `json:"fecha_ultima_compra"`
		} `json:"pulso"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.Equal(t, 42, resp.ClienteID)
	assert.Equal(t, "García López Ramón", resp.Nombre)
	assert.Equal(t, "3312345678", resp.Telefono)
	assert.Equal(t, "NORTE", resp.Zona)
	assert.Equal(t, "Martínez Reyes", resp.Cobrador)
	assert.Equal(t, "A", resp.Estatus)
	assert.Equal(t, "95000.00", resp.Resumen.TotalComprado)
	assert.Equal(t, 8, resp.Resumen.NumVentas)

	require.NotNil(t, resp.Pulso, "pulso must be present when TienePulso=true")
	assert.Equal(t, 75, resp.Pulso.Score)
	assert.Equal(t, "ACTIVO", resp.Pulso.Segmento)
	assert.Equal(t, "AL_CORRIENTE", resp.Pulso.EstadoPago)
	assert.Equal(t, "95000.00", resp.Pulso.Monetary)

	// FechaUltimaCompra must be RFC3339 UTC.
	require.NotEmpty(t, resp.Pulso.FechaUltimaCompra)
	parsed, err := time.Parse(time.RFC3339Nano, resp.Pulso.FechaUltimaCompra)
	require.NoError(t, err)
	assert.Equal(t, fixedFecha.UTC(), parsed.UTC())
}

// TestObtenerFicha_NoPulso_PulsoFieldOmitted verifies that when TienePulso=false
// the pulso field is null/omitted in the response.
func TestObtenerFicha_NoPulso_PulsoFieldOmitted(t *testing.T) {
	t.Parallel()

	c := newCliente(10)
	repo := &fakeRepo{cliente: c}
	// No pulse for this client.
	ac := &fakeAnalytics{pulsos: map[int]analytics.ClientePulsoContract{}}
	svc := buildService(repo, ac, &fakeSearch{})
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes/10", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Pulso *struct{} `json:"pulso"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Nil(t, resp.Pulso, "pulso must be null when no analytics data")
}

// TestObtenerFicha_NotFound_404 verifies that a missing client yields 404.
func TestObtenerFicha_NotFound_404(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{clienteErr: domain.ErrClienteNotFound}
	ac := &fakeAnalytics{}
	svc := buildService(repo, ac, &fakeSearch{})
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes/9999", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// ─── Scenario 3: GET /clientes/{id}/ventas ───────────────────────────────────

func TestListarVentasCliente_HappyPath_200(t *testing.T) {
	t.Parallel()

	v := newVenta(100, 42)
	repo := &fakeRepo{
		ventasPage: outbound.Page[*domain.VentaCliente]{
			Items:      []*domain.VentaCliente{v},
			NextCursor: "o50",
		},
	}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes/42/ventas", nil)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp struct {
		Items []struct {
			DoctoPVID  int    `json:"docto_pv_id"`
			Fecha      string `json:"fecha"`
			Folio      string `json:"folio"`
			Tipo       string `json:"tipo"`
			Total      string `json:"total"`
			SaldoVenta string `json:"saldo_venta"`
			NumPagos   int    `json:"num_pagos"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Items, 1)

	it := resp.Items[0]
	assert.Equal(t, 100, it.DoctoPVID)
	assert.Equal(t, "PV-0042", it.Folio)
	assert.Equal(t, "CREDITO", it.Tipo)
	assert.Equal(t, "12500.00", it.Total)
	assert.Equal(t, "8000.00", it.SaldoVenta)
	assert.Equal(t, 3, it.NumPagos)
	assert.Equal(t, "o50", resp.NextCursor)

	// Fecha must be RFC3339 UTC.
	require.NotEmpty(t, it.Fecha)
	parsed, err := time.Parse(time.RFC3339Nano, it.Fecha)
	require.NoError(t, err)
	assert.Equal(t, fixedFecha.UTC(), parsed.UTC())
}

// ─── Scenario 4: GET /clientes/{id}/ventas/{doctoPvId} ───────────────────────

func TestObtenerVentaDetalle_HappyPath_200(t *testing.T) {
	t.Parallel()

	v := newVenta(100, 42)
	producto := domain.HydrateProductoVenta(domain.HydrateProductoVentaParams{
		ArticuloID:      55,
		Nombre:          "Sala Mónaco",
		Unidades:        decimal.NewFromFloat(1.0),
		PrecioUnitario:  decimal.NewFromInt(12500),
		PrecioTotalNeto: decimal.NewFromInt(12500),
		PctjeDscto:      decimal.Zero,
	})
	pago := domain.HydratePago(domain.HydratePagoParams{
		DoctoCCID:  200,
		Fecha:      fixedPago,
		Importe:    decimal.NewFromInt(1500),
		FormaCobro: "Efectivo",
	})
	contrato := &outbound.ContratoCredito{
		Parcialidad:     decimal.NewFromInt(1500),
		Enganche:        decimal.NewFromInt(2500),
		PrecioDeContado: decimal.NewFromInt(11000),
		PlazoMeses:      12,
		FormaDePago:     "mensual",
		Vendedores:      []string{"López Hernández Arturo"},
	}

	repo := &fakeRepo{
		detalleByID: map[int]outbound.VentaDetalle{
			100: {
				Venta:     v,
				Productos: []*domain.ProductoVenta{producto},
				Contrato:  contrato,
				Pagos:     []*domain.Pago{pago},
			},
		},
	}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes/42/ventas/100", nil)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp struct {
		Venta struct {
			DoctoPVID int    `json:"docto_pv_id"`
			Tipo      string `json:"tipo"`
			Total     string `json:"total"`
		} `json:"venta"`
		Productos []struct {
			ArticuloID int    `json:"articulo_id"`
			Nombre     string `json:"nombre"`
			Unidades   string `json:"unidades"`
		} `json:"productos"`
		Contrato *struct {
			PlazoMeses int      `json:"plazo_meses"`
			Vendedores []string `json:"vendedores"`
			Enganche   string   `json:"enganche"`
		} `json:"contrato"`
		Pagos []struct {
			DoctoCCID  int    `json:"docto_cc_id"`
			Importe    string `json:"importe"`
			FormaCobro string `json:"forma_cobro"`
		} `json:"pagos"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.Equal(t, 100, resp.Venta.DoctoPVID)
	assert.Equal(t, "CREDITO", resp.Venta.Tipo)
	assert.Equal(t, "12500.00", resp.Venta.Total)

	require.Len(t, resp.Productos, 1)
	assert.Equal(t, 55, resp.Productos[0].ArticuloID)
	assert.Equal(t, "Sala Mónaco", resp.Productos[0].Nombre)
	assert.Equal(t, "1.00000", resp.Productos[0].Unidades)

	require.NotNil(t, resp.Contrato)
	assert.Equal(t, 12, resp.Contrato.PlazoMeses)
	assert.Equal(t, "2500.00", resp.Contrato.Enganche)
	assert.Equal(t, []string{"López Hernández Arturo"}, resp.Contrato.Vendedores)

	require.Len(t, resp.Pagos, 1)
	assert.Equal(t, 200, resp.Pagos[0].DoctoCCID)
	assert.Equal(t, "1500.00", resp.Pagos[0].Importe)
	assert.Equal(t, "Efectivo", resp.Pagos[0].FormaCobro)
}

// TestObtenerVentaDetalle_NotFound_404 verifies 404 for missing doctoPvID.
func TestObtenerVentaDetalle_NotFound_404(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{detalleByID: map[int]outbound.VentaDetalle{}}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes/42/ventas/9999", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestObtenerVentaDetalle_Contado_NilContrato verifies that a contado sale
// returns null for the contrato field.
func TestObtenerVentaDetalle_Contado_NilContrato(t *testing.T) {
	t.Parallel()

	v := domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{
		DoctoPVID: 200, ClienteID: 42,
		Fecha: fixedFecha, Folio: "PV-0200",
		Tipo: domain.TipoVentaContado, Total: decimal.NewFromInt(5000),
	})
	repo := &fakeRepo{
		detalleByID: map[int]outbound.VentaDetalle{
			200: {Venta: v, Productos: nil, Contrato: nil, Pagos: nil},
		},
	}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes/42/ventas/200", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Contrato *struct{} `json:"contrato"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Nil(t, resp.Contrato)
}

// ─── Scenario 5: POST /clientes/_search/refresh ──────────────────────────────

func TestRefrescarBusqueda_HappyPath_200(t *testing.T) {
	t.Parallel()

	docs := []outbound.SearchDoc{
		{ClienteID: 1, Texto: "García López"},
		{ClienteID: 2, Texto: "Martínez Reyes"},
	}
	repo := &fakeRepo{docs: docs}
	si := &fakeSearch{}
	svc := buildService(repo, &fakeAnalytics{}, si)
	cu := userWith(auth.PermClientesReindexar)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodPost, "/clientes/_search/refresh", nil)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp struct {
		Reindexado bool `json:"reindexado"`
		Documentos int  `json:"documentos"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.Reindexado)
	assert.Equal(t, 2, resp.Documentos)
}

func TestRefrescarBusqueda_RepoError_500(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{docsErr: errors.New("firebird unavailable")}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	cu := userWith(auth.PermClientesReindexar)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodPost, "/clientes/_search/refresh", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

// ─── Auth gate tests ──────────────────────────────────────────────────────────

func TestListarClientes_Unauthenticated_401(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	h := buildRouterNoAuth(svc)
	rec := doJSON(h, http.MethodGet, "/clientes", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListarClientes_NoPermission_403(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	cu := userWith() // no perms
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/clientes", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestObtenerFicha_NoPermission_403(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{cliente: newCliente(42)}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	cu := userWith(auth.PermAnalyticsWinbackRead) // wrong perm
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/clientes/42", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestObtenerFicha_Unauthenticated_401(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{cliente: newCliente(42)}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	h := buildRouterNoAuth(svc)
	rec := doJSON(h, http.MethodGet, "/clientes/42", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListarVentasCliente_NoPermission_403(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{ventasPage: outbound.Page[*domain.VentaCliente]{}}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	cu := userWith() // no perms
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/clientes/42/ventas", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestObtenerVentaDetalle_Unauthenticated_401(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{detalleByID: map[int]outbound.VentaDetalle{}}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	h := buildRouterNoAuth(svc)
	rec := doJSON(h, http.MethodGet, "/clientes/42/ventas/100", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListarVentasCliente_Unauthenticated_401(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{ventasPage: outbound.Page[*domain.VentaCliente]{}}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	h := buildRouterNoAuth(svc)
	rec := doJSON(h, http.MethodGet, "/clientes/42/ventas", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestObtenerVentaDetalle_NoPermission_403(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{detalleByID: map[int]outbound.VentaDetalle{}}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	cu := userWith() // no perms
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/clientes/42/ventas/100", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRefrescarBusqueda_NoPermission_403(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	cu := userWith(auth.PermClientesLeer) // read perm, not reindex
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodPost, "/clientes/_search/refresh", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRefrescarBusqueda_Unauthenticated_401(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{}
	svc := buildService(repo, &fakeAnalytics{}, &fakeSearch{})
	h := buildRouterNoAuth(svc)
	rec := doJSON(h, http.MethodPost, "/clientes/_search/refresh", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ─── CurrentUser context key canary ──────────────────────────────────────────

// TestCurrentUserContext_KeyMatchesAuth verifies that the context planting
// used in tests produces a user readable by the same key the handler uses.
func TestCurrentUserContext_KeyMatchesAuth(t *testing.T) {
	t.Parallel()

	cu := userWith(auth.PermClientesLeer)
	ctx := auth.PlantCurrentUser(context.Background(), cu)
	got, ok := auth.CurrentUserFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, cu.ID, got.ID)
	assert.Equal(t, cu.Email, got.Email)
}
