package microsiphttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	microsipapp "github.com/abdimuy/msp-api/internal/microsip/app"
	"github.com/abdimuy/msp-api/internal/microsip/domain"
	"github.com/abdimuy/msp-api/internal/microsip/infra/microsiphttp"
)

// ─── Fake repos ──────────────────────────────────────────────────────────

type fakeAlmacenRepo struct {
	almacenes []domain.Almacen
	articulos []domain.ArticuloAlmacen
	err       error
}

func (f *fakeAlmacenRepo) Listar(_ context.Context) ([]domain.Almacen, error) {
	return f.almacenes, f.err
}

func (f *fakeAlmacenRepo) Obtener(_ context.Context, id int) (*domain.Almacen, error) {
	if f.err != nil {
		return nil, f.err
	}
	for i, a := range f.almacenes {
		if a.ID == id {
			return &f.almacenes[i], nil
		}
	}
	return nil, nil
}

func (f *fakeAlmacenRepo) ListarArticulos(_ context.Context, _ int, _ string) ([]domain.ArticuloAlmacen, error) {
	return f.articulos, f.err
}

type fakeZonaRepo struct {
	zonas []domain.ZonaCliente
	err   error
}

func (f *fakeZonaRepo) Listar(_ context.Context) ([]domain.ZonaCliente, error) {
	return f.zonas, f.err
}

type fakeCiudadRepo struct {
	ciudades []domain.Ciudad
	err      error
}

func (f *fakeCiudadRepo) Listar(_ context.Context) ([]domain.Ciudad, error) {
	return f.ciudades, f.err
}

// ─── Harness ─────────────────────────────────────────────────────────────

func newServer(t *testing.T, almRepo *fakeAlmacenRepo, zRepo *fakeZonaRepo) http.Handler {
	t.Helper()
	return newServerFull(t, almRepo, zRepo, &fakeCiudadRepo{})
}

func newServerFull(
	t *testing.T, almRepo *fakeAlmacenRepo, zRepo *fakeZonaRepo, cRepo *fakeCiudadRepo,
) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	svc := microsipapp.NewService(almRepo, zRepo, cRepo)
	microsiphttp.MountRouter(r, svc)
	return r
}

// ─── Tests ───────────────────────────────────────────────────────────────

func TestListarAlmacenes_OK(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeAlmacenRepo{
		almacenes: []domain.Almacen{
			{ID: 19, Nombre: "MUEBLERIA MSP", Existencias: 1234},
			{ID: 20, Nombre: "BODEGA", Existencias: 100},
		},
	}, &fakeZonaRepo{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/almacenes", nil)
	srv.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Items []struct {
			AlmacenID   int    `json:"almacen_id"`
			Almacen     string `json:"almacen"`
			Existencias int64  `json:"existencias"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Items, 2)
	assert.Equal(t, 19, body.Items[0].AlmacenID)
	assert.Equal(t, "MUEBLERIA MSP", body.Items[0].Almacen)
	assert.Equal(t, int64(1234), body.Items[0].Existencias)
}

func TestListarAlmacenes_Error500(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeAlmacenRepo{err: errors.New("boom")}, &fakeZonaRepo{})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/almacenes", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestObtenerAlmacen_OK(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeAlmacenRepo{
		almacenes: []domain.Almacen{{ID: 7, Nombre: "A", Existencias: 0}},
	}, &fakeZonaRepo{})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/almacenes/7", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		AlmacenID int    `json:"almacen_id"`
		Almacen   string `json:"almacen"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, 7, body.AlmacenID)
	assert.Equal(t, "A", body.Almacen)
}

func TestObtenerAlmacen_NotFound(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeAlmacenRepo{}, &fakeZonaRepo{})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/almacenes/999", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestListarArticulosDelAlmacen_OK(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeAlmacenRepo{
		articulos: []domain.ArticuloAlmacen{
			{
				ArticuloID: 1, Articulo: "LICUADORA OSTER", Existencias: 5,
				LineaArticuloID: 12, LineaArticulo: "ELECTRO", Precios: "MUEBLERIAS:1234.56",
			},
		},
	}, &fakeZonaRepo{})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/almacenes/19/articulos?buscar=licuadora", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Items []struct {
			ArticuloID      int    `json:"articulo_id"`
			Articulo        string `json:"articulo"`
			Existencias     int64  `json:"existencias"`
			LineaArticuloID int    `json:"linea_articulo_id"`
			LineaArticulo   string `json:"linea_articulo"`
			Precios         string `json:"precios"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Items, 1)
	assert.Equal(t, "LICUADORA OSTER", body.Items[0].Articulo)
	assert.Equal(t, "MUEBLERIAS:1234.56", body.Items[0].Precios)
}

func TestListarArticulosDelAlmacen_NoBuscar(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeAlmacenRepo{}, &fakeZonaRepo{})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/almacenes/19/articulos", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Items []any `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Empty(t, body.Items)
}

func TestListarZonasCliente_OK(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeAlmacenRepo{}, &fakeZonaRepo{
		zonas: []domain.ZonaCliente{
			{ID: 5, Nombre: "TEHUACAN - JUAN PEREZ"},
			{ID: 6, Nombre: "PUEBLA"},
		},
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/zonas-cliente", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Items []struct {
			ZonaClienteID int    `json:"zona_cliente_id"`
			ZonaCliente   string `json:"zona_cliente"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Items, 2)
	assert.Equal(t, "TEHUACAN - JUAN PEREZ", body.Items[0].ZonaCliente)
	assert.Equal(t, "PUEBLA", body.Items[1].ZonaCliente)
}

func TestListarCiudades_OK(t *testing.T) {
	t.Parallel()
	srv := newServerFull(t, &fakeAlmacenRepo{}, &fakeZonaRepo{}, &fakeCiudadRepo{
		ciudades: []domain.Ciudad{
			{ID: 338, Nombre: "TEHUACAN", EstadoID: 337, Estado: "PUEBLA"},
			{ID: 48662, Nombre: "OAXACA", EstadoID: 11523, Estado: "OAXACA"},
		},
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ciudades", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Items []struct {
			CiudadID int    `json:"ciudad_id"`
			Ciudad   string `json:"ciudad"`
			EstadoID int    `json:"estado_id"`
			Estado   string `json:"estado"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Items, 2)

	// Each ciudad ships its own estado: the catalog spans several, so a fixed
	// estado would mismatch them.
	assert.Equal(t, "TEHUACAN", body.Items[0].Ciudad)
	assert.Equal(t, 337, body.Items[0].EstadoID)
	assert.Equal(t, "PUEBLA", body.Items[0].Estado)
	assert.Equal(t, "OAXACA", body.Items[1].Ciudad)
	assert.Equal(t, 11523, body.Items[1].EstadoID, "OAXACA must not inherit Puebla's estado")
}

func TestListarCiudades_EmptyIsArrayNotNull(t *testing.T) {
	t.Parallel()
	srv := newServerFull(t, &fakeAlmacenRepo{}, &fakeZonaRepo{}, &fakeCiudadRepo{})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ciudades", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	// Gson turns a null into a null field and the app NPEs on it.
	assert.Contains(t, rec.Body.String(), `"items":[]`)
}

func TestListarCiudades_RepoErrorIs500(t *testing.T) {
	t.Parallel()
	srv := newServerFull(t, &fakeAlmacenRepo{}, &fakeZonaRepo{}, &fakeCiudadRepo{
		err: errors.New("firebird down"),
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ciudades", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
