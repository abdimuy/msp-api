package authhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
)

// mountWithCurrentUser wraps MountRouter with a middleware that plants the
// supplied auth.CurrentUser on every request, bypassing the firebase verify
// step so tests can exercise the authz layer in isolation.
func mountWithCurrentUser(rig *testRig, cu auth.CurrentUser) chi.Router {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.PlantCurrentUser(req.Context(), cu)))
		})
	})
	// Mount handlers directly — bypassing MountRouter's Authn group so we
	// can isolate authz behavior with a planted CurrentUser.
	h := NewHandlers(rig.svc, rig.usuarios)
	r.Route("/usuarios", func(r chi.Router) {
		r.With(RequirePermission(domain.PermUsuariosListar)).Get("/", h.ListarUsuarios)
		r.With(RequirePermission(domain.PermUsuariosVer)).Get("/{id}", h.ObtenerUsuario)
		r.With(RequirePermission(domain.PermUsuariosActualizar)).Patch("/{id}", h.ActualizarUsuario)
		r.With(RequirePermission(domain.PermUsuariosDesactivar)).Delete("/{id}", h.DesactivarUsuario)
		r.With(RequirePermission(domain.PermUsuariosAsignarRol)).Post("/{id}/roles", h.AsignarRolAUsuario)
		r.With(RequirePermission(domain.PermUsuariosAsignarRol)).Delete("/{id}/roles/{rol_id}", h.RevocarRolDeUsuario)
		r.Post("/ensure-vendedores-by-email", h.EnsureVendedoresByEmail)
	})
	r.Route("/roles", func(r chi.Router) {
		r.With(RequirePermission(domain.PermRolesListar)).Get("/", h.ListarRoles)
		r.With(RequirePermission(domain.PermRolesListar)).Get("/{id}", h.ObtenerRol)
		r.With(RequirePermission(domain.PermRolesCrear)).Post("/", h.CrearRol)
		r.With(RequirePermission(domain.PermRolesActualizar)).Patch("/{id}", h.ActualizarRol)
		r.With(RequirePermission(domain.PermRolesActualizar)).Delete("/{id}", h.DesactivarRol)
		r.With(RequirePermission(domain.PermRolesAsignarPermiso)).Post("/{id}/permisos", h.AsignarPermisoARol)
		r.With(RequirePermission(domain.PermRolesAsignarPermiso)).Delete("/{id}/permisos/{codigo}", h.RevocarPermisoDeRol)
	})
	r.With(RequirePermission(domain.PermPermisosListar)).Get("/permisos", h.ListarPermisos)
	return r
}

// adminCurrentUser builds a CurrentUser with every permission, for tests that
// only care about the handler's data path.
func adminCurrentUser(u *domain.Usuario) auth.CurrentUser {
	codes := make([]string, 0)
	for _, p := range allPermissions() {
		codes = append(codes, string(p))
	}
	return auth.CurrentUser{
		ID:          u.ID(),
		FirebaseUID: u.FirebaseUID().Value(),
		Email:       u.Email().Value(),
		Nombre:      u.Nombre().Value(),
		Permisos:    codes,
	}
}

func TestListarUsuarios_HappyPath(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	rig.seedUsuario(t, "fbuid-2", "u2@example.com", "User Two")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/usuarios/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp ListResponse[UsuarioResponse]
	decodeBody(t, rec, &resp)
	assert.Len(t, resp.Items, 2)
}

func TestListarUsuarios_NoPermission_Returns403(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-no", "no@example.com", "No Perms")

	r := mountWithCurrentUser(rig, auth.CurrentUser{ID: caller.ID()})
	req := httptest.NewRequest(http.MethodGet, "/usuarios/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

func TestObtenerUsuario_NotFound_Returns404(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/usuarios/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestObtenerUsuario_InvalidUUID_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/usuarios/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestObtenerUsuario_HappyPath(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/usuarios/"+target.ID().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp UsuarioResponse
	decodeBody(t, rec, &resp)
	assert.Equal(t, target.ID().String(), resp.ID)
}

func TestActualizarUsuario_HappyPath(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPatch, "/usuarios/"+target.ID().String(), ActualizarUsuarioRequest{
		Email:  "newemail@example.com",
		Nombre: "New Name",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp UsuarioResponse
	decodeBody(t, rec, &resp)
	assert.Equal(t, "newemail@example.com", resp.Email)
}

func TestActualizarUsuario_InvalidEmail_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPatch, "/usuarios/"+target.ID().String(), ActualizarUsuarioRequest{
		Email:  "not-an-email",
		Nombre: "Some Name",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

func TestDesactivarUsuario_Returns204(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodDelete, "/usuarios/"+target.ID().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	u, err := rig.usuarios.FindByID(context.Background(), target.ID())
	require.NoError(t, err)
	assert.False(t, u.Activo())
}

func TestAsignarRol_Returns204(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")
	rol := rig.seedRol(t, "vendedor")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/"+target.ID().String()+"/roles", AsignarRolRequest{RolID: rol.ID().String()})
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestAsignarRol_InvalidRolID_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/"+target.ID().String()+"/roles", AsignarRolRequest{RolID: "not-a-uuid"})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestRevocarRol_Returns204(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")
	rol := rig.seedRol(t, "vendedor")
	require.NoError(t, rig.usuarios.AsignarRol(context.Background(), target.ID(), rol.ID(), caller.ID(), rig.clockTime))

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodDelete, "/usuarios/"+target.ID().String()+"/roles/"+rol.ID().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

// ─── EnsureVendedoresByEmail ────────────────────────────────────────────────

func TestEnsureVendedoresByEmail_HappyPath(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-ev", "ev@muebleriamsp.mx", "Cobrador Uno")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/ensure-vendedores-by-email",
		EnsureVendedoresByEmailRequest{Emails: []string{"juan@muebleriamsp.mx", "maria@muebleriamsp.mx"}},
	)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp EnsureVendedoresResponse
	decodeBody(t, rec, &resp)
	require.Len(t, resp.Vendedores, 2)
	assert.Equal(t, "juan@muebleriamsp.mx", resp.Vendedores[0].Email)
	assert.NotEmpty(t, resp.Vendedores[0].UsuarioID)
	_, parseErr := uuid.Parse(resp.Vendedores[0].UsuarioID)
	require.NoError(t, parseErr, "usuario_id must be a valid UUID")
	assert.Equal(t, "maria@muebleriamsp.mx", resp.Vendedores[1].Email)
	_, parseErr = uuid.Parse(resp.Vendedores[1].UsuarioID)
	require.NoError(t, parseErr, "usuario_id must be a valid UUID")
}

func TestEnsureVendedoresByEmail_NoAuth_401(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)

	// Build a router with NO CurrentUser planted — the handler must detect the
	// missing auth and return 401.
	r := chi.NewRouter()
	h := NewHandlers(rig.svc, rig.usuarios)
	r.Post("/usuarios/ensure-vendedores-by-email", h.EnsureVendedoresByEmail)

	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/ensure-vendedores-by-email",
		EnsureVendedoresByEmailRequest{Emails: []string{"x@muebleriamsp.mx"}},
	)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func TestEnsureVendedoresByEmail_EmptyList_422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-ev2", "ev2@muebleriamsp.mx", "Cobrador Dos")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/ensure-vendedores-by-email",
		EnsureVendedoresByEmailRequest{Emails: []string{}},
	)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

func TestEnsureVendedoresByEmail_TooMany_422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-ev3", "ev3@muebleriamsp.mx", "Cobrador Tres")

	emails := make([]string, 21)
	for i := range emails {
		emails[i] = "v" + string(rune('a'+i)) + "@muebleriamsp.mx"
	}
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/ensure-vendedores-by-email",
		EnsureVendedoresByEmailRequest{Emails: emails},
	)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

func TestEnsureVendedoresByEmail_InvalidEmail_422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-ev4", "ev4@muebleriamsp.mx", "Cobrador Cuatro")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/ensure-vendedores-by-email",
		EnsureVendedoresByEmailRequest{Emails: []string{"not-a-valid-email"}},
	)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

func TestEnsureVendedoresByEmail_InactiveVendedor_409(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-ev5", "ev5@muebleriamsp.mx", "Cobrador Cinco")

	// Seed a VENDEDOR_ONLY usuario and then deactivate it.
	inactive := rig.seedUsuario(t, "fbuid-inactivo", "inactivo@muebleriamsp.mx", "Inactivo Vendedor")
	inactive.Desactivar(caller.ID(), rig.clockTime)
	require.NoError(t, rig.usuarios.Update(context.Background(), inactive))

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/ensure-vendedores-by-email",
		EnsureVendedoresByEmailRequest{Emails: []string{"inactivo@muebleriamsp.mx"}},
	)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	var body map[string]any
	decodeBody(t, rec, &body)
	assert.Equal(t, "vendedor_email_inactivo", body["code"], "response must carry stable error code")
}

// doRequestRouter sends a JSON request through the supplied router. Similar to
// doRequest but the router is built by the caller (handy when bypassing
// MountRouter's authn group).
func doRequestRouter(t *testing.T, r chi.Router, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	rec, err := doJSONRequest(r, method, target, body)
	require.NoError(t, err)
	return rec
}

// (var _ used to silence unused complaints if helpers are pulled lazily.)
var _ outbound.UsuarioRepo = (*fakeUsuarioRepo)(nil)
