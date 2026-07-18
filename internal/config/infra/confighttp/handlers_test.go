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
	mappings map[uuid.UUID]configdomain.VendedorMapping
}

func newFakeConfigRepo() *fakeConfigRepo {
	return &fakeConfigRepo{mappings: map[uuid.UUID]configdomain.VendedorMapping{}}
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

type fakeCatalogoReader struct {
	nombres     map[int]string
	identidades []configdomain.IdentidadMicrosip
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
// (possibly empty) JSON body — Huma validates the body before the handler
// (and thus the auth check) ever runs, so http.NoBody would 400 before we
// could observe the 401/403 we're testing for.
func newSweepRequest(method, path string) *http.Request {
	if method == http.MethodPut {
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		return req
	}
	return httptest.NewRequest(method, path, http.NoBody)
}

// ─── security sweep ──────────────────────────────────────────────────────────

func TestConfig_Security_NoCurrentUser_Returns401(t *testing.T) {
	t.Parallel()
	svc := configapp.NewService(newFakeConfigRepo(), &fakeCatalogoReader{}, &fakeUsuariosReader{})
	r := buildRouter(svc, nil)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/config/vendedores"},
		{http.MethodGet, "/config/vendedores/opciones"},
		{http.MethodPut, "/config/vendedores/" + uuid.NewString()},
		{http.MethodDelete, "/config/vendedores/" + uuid.NewString()},
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
	svc := configapp.NewService(newFakeConfigRepo(), &fakeCatalogoReader{}, &fakeUsuariosReader{})

	cases := []struct {
		method string
		path   string
		held   []string
	}{
		{http.MethodGet, "/config/vendedores", []string{string(auth.PermConfigAdministrar)}},
		{http.MethodGet, "/config/vendedores/opciones", []string{string(auth.PermConfigAdministrar)}},
		{http.MethodPut, "/config/vendedores/" + uuid.NewString(), []string{string(auth.PermConfigLeer)}},
		{http.MethodDelete, "/config/vendedores/" + uuid.NewString(), []string{string(auth.PermConfigLeer)}},
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
	svc := configapp.NewService(repo, catalogo, usuarios)
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
	svc := configapp.NewService(newFakeConfigRepo(), catalogo, &fakeUsuariosReader{})
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
	svc := configapp.NewService(newFakeConfigRepo(), &fakeCatalogoReader{}, usuarios)
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
	svc := configapp.NewService(newFakeConfigRepo(), &fakeCatalogoReader{}, &fakeUsuariosReader{})
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
	svc := configapp.NewService(repo, &fakeCatalogoReader{}, &fakeUsuariosReader{})
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
	svc := configapp.NewService(newFakeConfigRepo(), &fakeCatalogoReader{}, &fakeUsuariosReader{})
	cu := fullPerms()
	r := buildRouter(svc, &cu)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/config/vendedores/no-es-un-uuid", http.NoBody))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}
