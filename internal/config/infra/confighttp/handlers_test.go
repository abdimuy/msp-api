package confighttp_test

import (
	"context"
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
	configapp "github.com/abdimuy/msp-api/internal/config/app"
	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
	"github.com/abdimuy/msp-api/internal/config/infra/confighttp"
)

// ─── fakes ───────────────────────────────────────────────────────────────────

type fakeConfigRepo struct {
	mappings        map[uuid.UUID]configdomain.VendedorMapping
	zonaCajaConfigs map[int]configdomain.ZonaCajaConfig
}

func newFakeConfigRepo() *fakeConfigRepo {
	return &fakeConfigRepo{
		mappings:        map[uuid.UUID]configdomain.VendedorMapping{},
		zonaCajaConfigs: map[int]configdomain.ZonaCajaConfig{},
	}
}

func (f *fakeConfigRepo) ListarVendedorMappings(_ context.Context) ([]configdomain.VendedorMapping, error) {
	out := make([]configdomain.VendedorMapping, 0, len(f.mappings))
	for _, m := range f.mappings {
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeConfigRepo) UpsertVendedorMapping(_ context.Context, m configdomain.VendedorMapping) error {
	f.mappings[m.UsuarioID] = m
	return nil
}

func (f *fakeConfigRepo) DeleteVendedorMapping(_ context.Context, usuarioID uuid.UUID) error {
	delete(f.mappings, usuarioID)
	return nil
}

func (f *fakeConfigRepo) ListarZonaCajaConfigs(_ context.Context) ([]configdomain.ZonaCajaConfig, error) {
	out := make([]configdomain.ZonaCajaConfig, 0, len(f.zonaCajaConfigs))
	for _, c := range f.zonaCajaConfigs {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeConfigRepo) UpsertZonaCajaConfig(_ context.Context, c configdomain.ZonaCajaConfig) error {
	f.zonaCajaConfigs[c.ZonaClienteID] = c
	return nil
}

type fakeCatalogoReader struct {
	nombres     map[int]string
	identidades []configdomain.IdentidadMicrosip

	zonas, cajas, cajeros, vendedoresCat, cobradores []configdomain.CatalogoRef
}

func (f *fakeCatalogoReader) ResolverNombresLista(_ context.Context, ids []int) (map[int]string, error) {
	out := make(map[int]string, len(ids))
	for _, id := range ids {
		if n, ok := f.nombres[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

func (f *fakeCatalogoReader) ListarIdentidadesMicrosip(_ context.Context) ([]configdomain.IdentidadMicrosip, error) {
	return f.identidades, nil
}

func (f *fakeCatalogoReader) ListaIDPerteneceAtributo(_ context.Context, _, _ int) (bool, error) {
	return true, nil
}

func (f *fakeCatalogoReader) ListarZonas(_ context.Context) ([]configdomain.CatalogoRef, error) {
	return f.zonas, nil
}

func (f *fakeCatalogoReader) ListarCajas(_ context.Context) ([]configdomain.CatalogoRef, error) {
	return f.cajas, nil
}

func (f *fakeCatalogoReader) ListarCajeros(_ context.Context) ([]configdomain.CatalogoRef, error) {
	return f.cajeros, nil
}

func (f *fakeCatalogoReader) ListarVendedoresCatalogo(_ context.Context) ([]configdomain.CatalogoRef, error) {
	return f.vendedoresCat, nil
}

func (f *fakeCatalogoReader) ListarCobradores(_ context.Context) ([]configdomain.CatalogoRef, error) {
	return f.cobradores, nil
}

// Existence checks always report true — the handler tests exercise gating
// and DTO shape, not app-layer validation (covered in internal/config/app).
func (f *fakeCatalogoReader) ZonaExiste(_ context.Context, _ int) (bool, error)     { return true, nil }
func (f *fakeCatalogoReader) CajaExiste(_ context.Context, _ int) (bool, error)     { return true, nil }
func (f *fakeCatalogoReader) CajeroExiste(_ context.Context, _ int) (bool, error)   { return true, nil }
func (f *fakeCatalogoReader) VendedorExiste(_ context.Context, _ int) (bool, error) { return true, nil }
func (f *fakeCatalogoReader) CobradorExiste(_ context.Context, _ int) (bool, error) { return true, nil }

type fakeUsuariosReader struct {
	usuarios []configdomain.AppUsuario
}

func (f *fakeUsuariosReader) ListarUsuarios(_ context.Context) ([]configdomain.AppUsuario, error) {
	return f.usuarios, nil
}

// ─── harness ─────────────────────────────────────────────────────────────────

func planter(cu auth.CurrentUser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.PlantCurrentUser(r.Context(), cu)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func fullPerms() auth.CurrentUser {
	return auth.CurrentUser{
		ID:     uuid.New(),
		Email:  "tester@muebleriamsp.mx",
		Nombre: "Tester",
		Permisos: []string{
			string(auth.PermConfigLeer),
			string(auth.PermConfigAdministrar),
		},
	}
}

func buildRouter(svc *configapp.Service, cu *auth.CurrentUser) http.Handler {
	r := chi.NewRouter()
	if cu != nil {
		r.Use(planter(*cu))
	}
	confighttp.MountRouter(r, svc)
	return r
}

// newSweepRequest builds a request for the security sweep tables. PUT needs a
// JSON body that satisfies the endpoint's schema — Huma validates the body
// before the handler (and thus the auth check) ever runs, so an empty or
// missing body would 422 before we could observe the 401/403 we're testing
// for. /config/vendedores/{id} has every body field optional (a *int with
// omitempty) so "{}" passes; /config/zonas-cajas/{id} has four required ints
// (the -1 sentinel must be explicit), so it needs a fully populated body.
func newSweepRequest(method, path string) *http.Request {
	if method != http.MethodPut {
		return httptest.NewRequest(method, path, http.NoBody)
	}
	body := "{}"
	if strings.Contains(path, "/config/zonas-cajas/") {
		body = `{"caja_id":-1,"cajero_id":-1,"vendedor_id":-1,"cobrador_id":-1}`
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// ─── security sweep ──────────────────────────────────────────────────────────

func TestConfig_Security_NoCurrentUser_Returns401(t *testing.T) {
	t.Parallel()
	svc := configapp.NewService(newFakeConfigRepo(), &fakeCatalogoReader{}, &fakeUsuariosReader{}, &fakeCatalogoReader{}, &fakeCatalogoReader{})
	r := buildRouter(svc, nil)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/config/vendedores"},
		{http.MethodGet, "/config/vendedores/opciones"},
		{http.MethodPut, "/config/vendedores/" + uuid.NewString()},
		{http.MethodDelete, "/config/vendedores/" + uuid.NewString()},
		{http.MethodGet, "/config/zonas-cajas"},
		{http.MethodGet, "/config/zonas-cajas/opciones"},
		{http.MethodPut, "/config/zonas-cajas/12271"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, newSweepRequest(rt.method, rt.path))
			assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})
	}
}

func TestConfig_Security_MissingPerm_Returns403(t *testing.T) {
	t.Parallel()
	svc := configapp.NewService(newFakeConfigRepo(), &fakeCatalogoReader{}, &fakeUsuariosReader{}, &fakeCatalogoReader{}, &fakeCatalogoReader{})

	cases := []struct {
		method string
		path   string
		held   []string
	}{
		{http.MethodGet, "/config/vendedores", []string{string(auth.PermConfigAdministrar)}},
		{http.MethodGet, "/config/vendedores/opciones", []string{string(auth.PermConfigAdministrar)}},
		{http.MethodPut, "/config/vendedores/" + uuid.NewString(), []string{string(auth.PermConfigLeer)}},
		{http.MethodDelete, "/config/vendedores/" + uuid.NewString(), []string{string(auth.PermConfigLeer)}},
		{http.MethodGet, "/config/zonas-cajas", []string{string(auth.PermConfigAdministrar)}},
		{http.MethodGet, "/config/zonas-cajas/opciones", []string{string(auth.PermConfigAdministrar)}},
		{http.MethodPut, "/config/zonas-cajas/12271", []string{string(auth.PermConfigLeer)}},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			cu := auth.CurrentUser{ID: uuid.New(), Permisos: tc.held}
			r := buildRouter(svc, &cu)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, newSweepRequest(tc.method, tc.path))
			assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		})
	}
}

// ─── happy paths ─────────────────────────────────────────────────────────────

func TestListarVendedores_OK(t *testing.T) {
	t.Parallel()
	usuarioID := uuid.New()
	repo := newFakeConfigRepo()
	l1 := 101
	repo.mappings[usuarioID] = configdomain.VendedorMapping{UsuarioID: usuarioID, ListaID1: &l1}
	catalogo := &fakeCatalogoReader{nombres: map[int]string{101: "Rosa Martínez"}}
	usuarios := &fakeUsuariosReader{usuarios: []configdomain.AppUsuario{
		{ID: usuarioID, Nombre: "Rosa Martínez", Email: "rosa.martinez@muebleriamsp.mx"},
	}}
	svc := configapp.NewService(repo, catalogo, usuarios, catalogo, catalogo)
	cu := fullPerms()
	r := buildRouter(svc, &cu)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config/vendedores", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items []struct {
			UsuarioID string `json:"usuario_id"`
			Estado    string `json:"estado"`
			Mapping   struct {
				V1 *struct {
					ListaID int    `json:"lista_id"`
					Nombre  string `json:"nombre"`
				} `json:"v1"`
			} `json:"mapping"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Items, 1)
	assert.Equal(t, "1/3", body.Items[0].Estado)
	require.NotNil(t, body.Items[0].Mapping.V1)
	assert.Equal(t, 101, body.Items[0].Mapping.V1.ListaID)
	assert.Equal(t, "Rosa Martínez", body.Items[0].Mapping.V1.Nombre)
}

func TestListarOpciones_OK(t *testing.T) {
	t.Parallel()
	catalogo := &fakeCatalogoReader{identidades: []configdomain.IdentidadMicrosip{
		{Nombre: "Daniel Hernández", MatchCount: 2},
	}}
	svc := configapp.NewService(newFakeConfigRepo(), catalogo, &fakeUsuariosReader{}, catalogo, catalogo)
	cu := fullPerms()
	r := buildRouter(svc, &cu)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config/vendedores/opciones", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items []struct {
			Nombre     string `json:"nombre"`
			MatchCount int    `json:"match_count"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Items, 1)
	assert.Equal(t, "Daniel Hernández", body.Items[0].Nombre)
	assert.Equal(t, 2, body.Items[0].MatchCount)
}

func TestAsignarVendedor_OK(t *testing.T) {
	t.Parallel()
	usuarioID := uuid.New()
	usuarios := &fakeUsuariosReader{usuarios: []configdomain.AppUsuario{
		{ID: usuarioID, Nombre: "Brenda López", Email: "brenda.lopez@muebleriamsp.mx"},
	}}
	svc := configapp.NewService(newFakeConfigRepo(), &fakeCatalogoReader{}, usuarios, &fakeCatalogoReader{}, &fakeCatalogoReader{})
	cu := fullPerms()
	r := buildRouter(svc, &cu)

	body := `{"vendedor_lista_id_1": 55}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/config/vendedores/"+usuarioID.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Item struct {
			UsuarioID string `json:"usuario_id"`
			Estado    string `json:"estado"`
		} `json:"item"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, usuarioID.String(), resp.Item.UsuarioID)
	assert.Equal(t, "1/3", resp.Item.Estado)
}

func TestAsignarVendedor_UsuarioIDInvalido_Retorna422(t *testing.T) {
	t.Parallel()
	svc := configapp.NewService(newFakeConfigRepo(), &fakeCatalogoReader{}, &fakeUsuariosReader{}, &fakeCatalogoReader{}, &fakeCatalogoReader{})
	cu := fullPerms()
	r := buildRouter(svc, &cu)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/config/vendedores/no-es-un-uuid", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

func TestEliminarVendedor_OK(t *testing.T) {
	t.Parallel()
	usuarioID := uuid.New()
	repo := newFakeConfigRepo()
	repo.mappings[usuarioID] = configdomain.VendedorMapping{UsuarioID: usuarioID}
	svc := configapp.NewService(repo, &fakeCatalogoReader{}, &fakeUsuariosReader{}, &fakeCatalogoReader{}, &fakeCatalogoReader{})
	cu := fullPerms()
	r := buildRouter(svc, &cu)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/config/vendedores/"+usuarioID.String(), http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		OK bool `json:"ok"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.True(t, body.OK)
	_, stillThere := repo.mappings[usuarioID]
	assert.False(t, stillThere)
}

func TestEliminarVendedor_UsuarioIDInvalido_Retorna422(t *testing.T) {
	t.Parallel()
	svc := configapp.NewService(newFakeConfigRepo(), &fakeCatalogoReader{}, &fakeUsuariosReader{}, &fakeCatalogoReader{}, &fakeCatalogoReader{})
	cu := fullPerms()
	r := buildRouter(svc, &cu)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/config/vendedores/no-es-un-uuid", http.NoBody))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

// ─── zonas/cajas: happy paths ────────────────────────────────────────────────

func TestListarZonasCajas_OK(t *testing.T) {
	t.Parallel()
	repo := newFakeConfigRepo()
	repo.zonaCajaConfigs[12271] = configdomain.ZonaCajaConfig{
		ZonaClienteID: 12271, CajaID: 12151, CajeroID: 22368, VendedorID: 88240, CobradorID: 11294,
	}
	catalogo := &fakeCatalogoReader{
		zonas:         []configdomain.CatalogoRef{{ID: 12271, Nombre: "R/01"}},
		cajas:         []configdomain.CatalogoRef{{ID: 12151, Nombre: "CAJA1"}},
		cajeros:       []configdomain.CatalogoRef{{ID: 22368, Nombre: "RUTA01"}},
		vendedoresCat: []configdomain.CatalogoRef{{ID: 88240, Nombre: "RUTA01"}},
		cobradores:    []configdomain.CatalogoRef{{ID: 11294, Nombre: "RUTA 01 - JUAN CARLOS CASTRO"}},
	}
	svc := configapp.NewService(repo, catalogo, &fakeUsuariosReader{}, catalogo, catalogo)
	cu := fullPerms()
	r := buildRouter(svc, &cu)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config/zonas-cajas", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items []struct {
			ZonaClienteID int `json:"zona_cliente_id"`
			Caja          *struct {
				ID     int    `json:"id"`
				Nombre string `json:"nombre"`
			} `json:"caja"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Items, 1)
	assert.Equal(t, 12271, body.Items[0].ZonaClienteID)
	require.NotNil(t, body.Items[0].Caja)
	assert.Equal(t, "CAJA1", body.Items[0].Caja.Nombre)
}

func TestListarOpcionesZonasCajas_OK(t *testing.T) {
	t.Parallel()
	catalogo := &fakeCatalogoReader{
		zonas: []configdomain.CatalogoRef{{ID: 12271, Nombre: "R/01"}},
		cajas: []configdomain.CatalogoRef{{ID: 12151, Nombre: "CAJA1"}},
	}
	svc := configapp.NewService(newFakeConfigRepo(), catalogo, &fakeUsuariosReader{}, catalogo, catalogo)
	cu := fullPerms()
	r := buildRouter(svc, &cu)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config/zonas-cajas/opciones", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Zonas []struct {
			ID     int    `json:"id"`
			Nombre string `json:"nombre"`
		} `json:"zonas"`
		Cajas []struct {
			ID     int    `json:"id"`
			Nombre string `json:"nombre"`
		} `json:"cajas"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Zonas, 1)
	assert.Equal(t, "R/01", body.Zonas[0].Nombre)
	require.Len(t, body.Cajas, 1)
	assert.Equal(t, "CAJA1", body.Cajas[0].Nombre)
}

func TestAsignarZonaCaja_OK(t *testing.T) {
	t.Parallel()
	catalogo := &fakeCatalogoReader{
		zonas: []configdomain.CatalogoRef{{ID: 12271, Nombre: "R/01"}},
	}
	svc := configapp.NewService(newFakeConfigRepo(), catalogo, &fakeUsuariosReader{}, catalogo, catalogo)
	cu := fullPerms()
	r := buildRouter(svc, &cu)

	body := `{"caja_id": 12151, "cajero_id": 22368, "vendedor_id": 88240, "cobrador_id": 11294}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/config/zonas-cajas/12271", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Item struct {
			ZonaClienteID int `json:"zona_cliente_id"`
			Caja          *struct {
				ID int `json:"id"`
			} `json:"caja"`
		} `json:"item"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 12271, resp.Item.ZonaClienteID)
	require.NotNil(t, resp.Item.Caja)
	assert.Equal(t, 12151, resp.Item.Caja.ID)
}

func TestAsignarZonaCaja_ZonaClienteIDInvalido_Retorna422(t *testing.T) {
	t.Parallel()
	svc := configapp.NewService(newFakeConfigRepo(), &fakeCatalogoReader{}, &fakeUsuariosReader{}, &fakeCatalogoReader{}, &fakeCatalogoReader{})
	cu := fullPerms()
	r := buildRouter(svc, &cu)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/config/zonas-cajas/no-es-un-entero", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}
