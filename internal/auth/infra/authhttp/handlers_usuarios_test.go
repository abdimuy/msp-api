package authhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	platform "github.com/abdimuy/msp-api/internal/platform/domain"
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
		r.With(RequirePermission(domain.PermUsuariosCrear)).Post("/", h.CrearUsuario)
		r.With(RequirePermission(domain.PermUsuariosVer)).Get("/{id}", h.ObtenerUsuario)
		r.With(RequirePermission(domain.PermUsuariosActualizar)).Patch("/{id}", h.ActualizarUsuario)
		r.With(RequirePermission(domain.PermUsuariosDesactivar)).Delete("/{id}", h.DesactivarUsuario)
		r.With(RequirePermission(domain.PermUsuariosVer)).Get("/{id}/roles", h.ObtenerRolesDeUsuario)
		r.With(RequirePermission(domain.PermUsuariosAsignarRol)).Post("/{id}/roles", h.AsignarRolAUsuario)
		r.With(RequirePermission(domain.PermUsuariosAsignarRol)).Delete("/{id}/roles/{rol_id}", h.RevocarRolDeUsuario)
		r.With(RequirePermission(domain.PermUsuariosVer)).Get("/{id}/permisos", h.ObtenerPermisosDeUsuario)
		r.Post("/ensure-vendedores-by-email", h.EnsureVendedoresByEmail)
	})
	r.Route("/roles", func(r chi.Router) {
		r.With(RequirePermission(domain.PermRolesListar)).Get("/", h.ListarRoles)
		r.With(RequirePermission(domain.PermRolesListar)).Get("/{id}", h.ObtenerRol)
		r.With(RequirePermission(domain.PermRolesCrear)).Post("/", h.CrearRol)
		r.With(RequirePermission(domain.PermRolesActualizar)).Patch("/{id}", h.ActualizarRol)
		r.With(RequirePermission(domain.PermRolesActualizar)).Delete("/{id}", h.DesactivarRol)
		r.With(RequirePermission(domain.PermRolesAsignarPermiso)).Post("/{id}/permisos", h.AsignarPermisoARol)
		r.With(RequirePermission(domain.PermRolesListar)).Get("/{id}/permisos", h.ObtenerPermisosDeRol)
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

// ─── ObtenerRolesDeUsuario ──────────────────────────────────────────────────

func TestObtenerRolesDeUsuario_HappyPath(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")
	rol := rig.seedRol(t, "vendedor")
	require.NoError(t, rig.usuarios.AsignarRol(context.Background(), target.ID(), rol.ID(), caller.ID(), rig.clockTime))

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/usuarios/"+target.ID().String()+"/roles", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp ListResponse[RolResponse]
	decodeBody(t, rec, &resp)
	require.Len(t, resp.Items, 1)
	assert.Equal(t, rol.ID().String(), resp.Items[0].ID)
	assert.Equal(t, "vendedor", resp.Items[0].Nombre)
}

func TestObtenerRolesDeUsuario_NotFound_Returns404(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/usuarios/"+uuid.New().String()+"/roles", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestObtenerRolesDeUsuario_NoPermission_Returns403(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-no", "no@example.com", "No Perms")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")

	r := mountWithCurrentUser(rig, auth.CurrentUser{ID: caller.ID()})
	req := httptest.NewRequest(http.MethodGet, "/usuarios/"+target.ID().String()+"/roles", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// ─── ObtenerPermisosDeUsuario ───────────────────────────────────────────────

func TestObtenerPermisosDeUsuario_HappyPath_EffectiveUnion(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")
	rig.seedPermiso(t, domain.PermUsuariosListar)
	rig.seedPermiso(t, domain.PermRolesListar)
	// Effective union: usuarios.PermisosFor returns bare codes regardless of
	// how many roles contributed them — the fake models that directly.
	rig.usuarios.Permisos[target.ID()] = []domain.Permission{domain.PermUsuariosListar, domain.PermRolesListar}

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/usuarios/"+target.ID().String()+"/permisos", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp ListResponse[PermisoResponse]
	decodeBody(t, rec, &resp)
	require.Len(t, resp.Items, 2)
	codes := []string{resp.Items[0].Codigo, resp.Items[1].Codigo}
	assert.ElementsMatch(t, []string{string(domain.PermUsuariosListar), string(domain.PermRolesListar)}, codes)
}

func TestObtenerPermisosDeUsuario_OrphanCodeSkipped(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")
	rig.seedPermiso(t, domain.PermUsuariosListar)
	// PermRolesListar is granted but absent from the catalog: it must be
	// silently skipped by the enrichment step.
	rig.usuarios.Permisos[target.ID()] = []domain.Permission{domain.PermUsuariosListar, domain.PermRolesListar}

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/usuarios/"+target.ID().String()+"/permisos", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp ListResponse[PermisoResponse]
	decodeBody(t, rec, &resp)
	require.Len(t, resp.Items, 1)
	assert.Equal(t, string(domain.PermUsuariosListar), resp.Items[0].Codigo)
}

func TestObtenerPermisosDeUsuario_NotFound_Returns404(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/usuarios/"+uuid.New().String()+"/permisos", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestObtenerPermisosDeUsuario_NoPermission_Returns403(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-no", "no@example.com", "No Perms")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")

	r := mountWithCurrentUser(rig, auth.CurrentUser{ID: caller.ID()})
	req := httptest.NewRequest(http.MethodGet, "/usuarios/"+target.ID().String()+"/permisos", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
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

// ─── CrearUsuario ───────────────────────────────────────────────────────────

func TestCrearUsuario_HappyPath_Returns201(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@muebleriamsp.mx", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	tel := "+52 449 123 4567"
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		FirebaseUID: "fbuid-nuevo",
		Email:       "Gabriel.Roque@muebleriamsp.mx",
		Nombre:      "Gabriel Roque",
		Telefono:    &tel,
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp UsuarioResponse
	decodeBody(t, rec, &resp)
	_, parseErr := uuid.Parse(resp.ID)
	require.NoError(t, parseErr, "id must be a valid UUID")
	assert.Equal(t, "fbuid-nuevo", resp.FirebaseUID)
	assert.Equal(t, "gabriel.roque@muebleriamsp.mx", resp.Email, "email is normalized to lowercase")
	assert.Equal(t, "Gabriel Roque", resp.Nombre)
	require.NotNil(t, resp.Telefono)
	assert.Equal(t, "4491234567", *resp.Telefono)
	assert.Nil(t, resp.AlmacenID)
	assert.True(t, resp.Activo)
	assert.Equal(t, rig.clockTime.UTC().Format(time.RFC3339Nano), resp.CreatedAt)
	assert.Equal(t, rig.clockTime.UTC().Format(time.RFC3339Nano), resp.UpdatedAt)

	// The row landed with the uid bound, which is the whole point: a later
	// login resolves through FindByFirebaseUID instead of creating a twin.
	stored, findErr := rig.usuarios.FindByFirebaseUID(context.Background(), "fbuid-nuevo")
	require.NoError(t, findErr)
	assert.Equal(t, resp.ID, stored.ID().String())
	assert.Equal(t, domain.EstatusFirebaseUser, stored.Estatus())
	assert.Equal(t, caller.ID(), stored.CreatedBy(), "created_by must be the actor")
}

func TestCrearUsuario_WithoutTelefono_Returns201(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@muebleriamsp.mx", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		FirebaseUID: "fbuid-sin-tel",
		Email:       "maria.hernandez@muebleriamsp.mx",
		Nombre:      "Maria Hernandez",
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp UsuarioResponse
	decodeBody(t, rec, &resp)
	assert.Nil(t, resp.Telefono)
	assert.True(t, resp.Activo)
}

func TestCrearUsuario_EmailDuplicado_Returns409(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@muebleriamsp.mx", "Admin")
	existing := rig.seedUsuario(t, "fbuid-existente", "repetido@muebleriamsp.mx", "Ya Existe")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		FirebaseUID: "fbuid-otro",
		Email:       existing.Email().Value(),
		Nombre:      "Otro Nombre",
	})
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Equal(t, "usuario_ya_existe", problemCode(t, rec))
}

func TestCrearUsuario_FirebaseUIDDuplicado_Returns409(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@muebleriamsp.mx", "Admin")
	existing := rig.seedUsuario(t, "fbuid-existente", "existente@muebleriamsp.mx", "Ya Existe")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		FirebaseUID: existing.FirebaseUID().Value(),
		Email:       "correo.libre@muebleriamsp.mx",
		Nombre:      "Otro Nombre",
	})
	// This one does NOT come from a pre-check: the email is free, so the
	// service goes straight to Save and UQ_MSP_USUARIOS_FIREBASE_UID is what
	// refuses it. That Save is the net for the race the pre-check cannot see,
	// and it must surface as the same 409 code as every other collision.
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Equal(t, "usuario_ya_existe", problemCode(t, rec))
}

func TestCrearUsuario_EmailInvalido_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@muebleriamsp.mx", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		FirebaseUID: "fbuid-mal-correo",
		Email:       "no-es-correo",
		Nombre:      "Nombre Valido",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	_, findErr := rig.usuarios.FindByFirebaseUID(context.Background(), "fbuid-mal-correo")
	require.ErrorIs(t, findErr, domain.ErrUsuarioNotFound, "nothing may be persisted on a 422")
}

func TestCrearUsuario_MissingFirebaseUID_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@muebleriamsp.mx", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		Email:  "sin.uid@muebleriamsp.mx",
		Nombre: "Sin Uid",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

func TestCrearUsuario_MalformedJSON_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@muebleriamsp.mx", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodPost, "/usuarios/", strings.NewReader(`{"firebase_uid": `))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// decodeJSON maps a decode failure to apperror.NewValidation("invalid_json"),
	// which the response layer renders as 422.
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "invalid_json")
}

func TestCrearUsuario_NoPermission_Returns403(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-no-perm", "no.perm@muebleriamsp.mx", "Sin Permiso")

	// A CurrentUser without usuarios:crear must be rejected before the handler.
	r := mountWithCurrentUser(rig, auth.CurrentUser{ID: caller.ID()})
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		FirebaseUID: "fbuid-bloqueado",
		Email:       "bloqueado@muebleriamsp.mx",
		Nombre:      "Bloqueado",
	})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	_, findErr := rig.usuarios.FindByFirebaseUID(context.Background(), "fbuid-bloqueado")
	require.ErrorIs(t, findErr, domain.ErrUsuarioNotFound, "a 403 must not create the row")
}

func TestCrearUsuario_NoAuth_Returns401(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)

	// Router with NO CurrentUser planted: the handler itself must detect the
	// missing actor and answer 401 rather than persisting with a nil creator.
	r := chi.NewRouter()
	h := NewHandlers(rig.svc, rig.usuarios)
	r.Post("/usuarios/", h.CrearUsuario)

	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		FirebaseUID: "fbuid-anon",
		Email:       "anon@muebleriamsp.mx",
		Nombre:      "Anonimo",
	})
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	_, findErr := rig.usuarios.FindByFirebaseUID(context.Background(), "fbuid-anon")
	require.ErrorIs(t, findErr, domain.ErrUsuarioNotFound, "a 401 must not create the row")
}

// crearRoutingRig mounts the REAL MountRouter under /v2 with a usuario that
// holds usuarios:crear, so routing is exercised exactly as it is in
// production (authn middleware, idempotency middleware and all) instead of
// through the hand-rolled router the other tests use.
func crearRoutingRig(t *testing.T, perms ...domain.Permission) chi.Router {
	t.Helper()
	rig := newTestRig(t)
	u := rig.seedUsuario(t, "fbuid-routing", "routing@muebleriamsp.mx", "Routing")
	rig.firebase.Token = &outbound.FirebaseToken{
		UID: "fbuid-routing", Email: "routing@muebleriamsp.mx", Name: "Routing",
	}
	rig.usuarios.Permisos[u.ID()] = perms

	root := chi.NewRouter()
	root.Route("/v2", func(r chi.Router) {
		MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())
	})
	return root
}

// TestCrearUsuario_Routing_WithAndWithoutTrailingSlash pins the path the
// office desktop actually calls. chi mounts the /usuarios subrouter with
// Route(), and Post("/") inside it must answer BOTH /v2/usuarios and
// /v2/usuarios/ — a redirect or a 404 on the bare path would break every
// client that omits the slash.
func TestCrearUsuario_Routing_WithAndWithoutTrailingSlash(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"/v2/usuarios", "/v2/usuarios/"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			r := crearRoutingRig(t, domain.PermUsuariosCrear)
			body, err := json.Marshal(CrearUsuarioRequest{
				FirebaseUID: "fbuid-routing-nuevo",
				Email:       "routing.nuevo@muebleriamsp.mx",
				Nombre:      "Routing Nuevo",
			})
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer t")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			require.Equal(t, http.StatusCreated, rec.Code,
				"POST %s must reach CrearUsuario; got %d body=%s", target, rec.Code, rec.Body.String())
		})
	}
}

// TestCrearUsuario_Routing_DoesNotShadowEnsureVendedores guards the sibling
// POST on the same subrouter: adding Post("/") must not swallow
// POST /v2/usuarios/ensure-vendedores-by-email.
func TestCrearUsuario_Routing_DoesNotShadowEnsureVendedores(t *testing.T) {
	t.Parallel()
	r := crearRoutingRig(t)

	body, err := json.Marshal(EnsureVendedoresByEmailRequest{
		Emails: []string{"vendedor.ruta@muebleriamsp.mx"},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		"/v2/usuarios/ensure-vendedores-by-email", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer t")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// CrearUsuario would have rejected this body with 422 (unknown fields);
	// only the ensure handler answers 200 with the vendedores envelope.
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp EnsureVendedoresResponse
	decodeBody(t, rec, &resp)
	require.Len(t, resp.Vendedores, 1)
	assert.Equal(t, "vendedor.ruta@muebleriamsp.mx", resp.Vendedores[0].Email)
}

// ─── CrearUsuario: promoción de un VENDEDOR_ONLY ────────────────────────────

// seedVendedorOnly persists an active VENDEDOR_ONLY usuario — the row
// EnsureVendedoresByEmail mints from the phone when a cobrador names someone
// as vendedor of a venta: no firebase_uid, and a nombre that is just the
// email's local-part in lowercase.
func (r *testRig) seedVendedorOnly(t *testing.T, email, nombre string) *domain.Usuario {
	t.Helper()
	em, err := domain.NewEmail(email)
	require.NoError(t, err)
	nm, err := domain.NewNombre(nombre)
	require.NoError(t, err)
	u := domain.NewVendedorUsuario(uuid.New(), em, nm, uuid.New(), r.clockTime)
	require.NoError(t, r.usuarios.Save(context.Background(), u))
	return u
}

// TestCrearUsuario_PromueveVendedorOnly_Returns201 is the case that motivated
// the whole change: the office registers Humberto, whose row already exists
// because a cobrador named him as vendedor from the phone. The alta must
// answer 201 with the PRE-EXISTING id — not 409, and not a second row.
func TestCrearUsuario_PromueveVendedorOnly_Returns201(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@muebleriamsp.mx", "Admin")
	vendedor := rig.seedVendedorOnly(t, "humberto.quintana@muebleriamsp.mx", "humberto.quintana")
	// Snapshotted before the call: the fake hands the service the pointer it
	// stores, so reading vendedor.CreatedBy() afterwards would read the
	// mutated object.
	vendedorCreatedBy := vendedor.CreatedBy()

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	tel := "+52 449 987 6543"
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		FirebaseUID: "fbuid-humberto",
		Email:       "Humberto.Quintana@muebleriamsp.mx",
		Nombre:      "Humberto Quintana",
		Telefono:    &tel,
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp UsuarioResponse
	decodeBody(t, rec, &resp)
	assert.Equal(t, vendedor.ID().String(), resp.ID,
		"the response must carry the PRE-EXISTING row id, not a fresh one")
	assert.Equal(t, "fbuid-humberto", resp.FirebaseUID)
	assert.Equal(t, "humberto.quintana@muebleriamsp.mx", resp.Email)
	assert.Equal(t, "Humberto Quintana", resp.Nombre,
		"the real nombre must replace the one derived from the email")
	require.NotNil(t, resp.Telefono)
	assert.Equal(t, "4499876543", *resp.Telefono)
	assert.True(t, resp.Activo)

	// One row, promoted in place, reachable by both unique keys.
	stored, findErr := rig.usuarios.FindByFirebaseUID(context.Background(), "fbuid-humberto")
	require.NoError(t, findErr)
	assert.Equal(t, vendedor.ID(), stored.ID())
	assert.Equal(t, domain.EstatusFirebaseUser, stored.Estatus())
	byEmail, findErr := rig.usuarios.FindByEmail(context.Background(), "humberto.quintana@muebleriamsp.mx")
	require.NoError(t, findErr)
	assert.Equal(t, vendedor.ID(), byEmail.ID())
	assert.Equal(t, caller.ID(), stored.UpdatedBy(), "updated_by must be the actor of the alta")
	assert.Equal(t, vendedorCreatedBy, stored.CreatedBy(),
		"created_by belongs to whoever minted the vendedor row, not to the alta's actor")
	assert.NotEqual(t, caller.ID(), stored.CreatedBy())
}

// TestCrearUsuario_PromueveSinTelefono_ConservaElPrevio pins the rule that an
// omitted telefono preserves whatever the row already had.
func TestCrearUsuario_PromueveSinTelefono_ConservaElPrevio(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@muebleriamsp.mx", "Admin")
	vendedor := rig.seedVendedorOnly(t, "lucia.mendez@muebleriamsp.mx", "lucia.mendez")

	previo, err := platform.NewTelefono("4491112233")
	require.NoError(t, err)
	vendedor.Update(domain.UsuarioUpdate{
		Email:    vendedor.Email(),
		Nombre:   vendedor.Nombre(),
		Telefono: &previo,
	}, uuid.New(), rig.clockTime)
	require.NoError(t, rig.usuarios.Update(context.Background(), vendedor))

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		FirebaseUID: "fbuid-lucia",
		Email:       "lucia.mendez@muebleriamsp.mx",
		Nombre:      "Lucia Mendez",
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp UsuarioResponse
	decodeBody(t, rec, &resp)
	require.NotNil(t, resp.Telefono, "an omitted telefono must not erase the stored one")
	assert.Equal(t, "4491112233", *resp.Telefono)
	assert.Equal(t, "Lucia Mendez", resp.Nombre)
}

// problemCode decodes the Problem Details envelope and returns its machine
// readable "code". The office desktop keys its user-facing copy off that
// field, so the code is part of the contract, not an implementation detail.
func problemCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var problem struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
	}
	decodeBody(t, rec, &problem)
	return problem.Code
}

// TestCrearUsuario_VendedorOnlyInactivo_Returns409 guards the deliberate
// refusal to reactivate: somebody deactivated that row on purpose.
//
// The status AND the code are both pinned. A deactivated row is exactly the
// case where a plausible-but-wrong sentinel slips through the service layer
// unnoticed: domain.ErrUsuarioInactivo reads like a perfect fit, but it is a
// 403 the authn middleware already raises for the CALLER's own account, and
// the desktop renders it as "your account is disabled" — telling the operator
// her own session died when in fact somebody else's old row is in the way.
// Every collision here must be the same 409 "usuario_ya_existe" the desktop
// covers with one message.
func TestCrearUsuario_VendedorOnlyInactivo_Returns409(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@muebleriamsp.mx", "Admin")
	vendedor := rig.seedVendedorOnly(t, "de.baja@muebleriamsp.mx", "de.baja")
	vendedor.Desactivar(uuid.New(), rig.clockTime)
	require.NoError(t, rig.usuarios.Update(context.Background(), vendedor))

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		FirebaseUID: "fbuid-de-baja",
		Email:       "de.baja@muebleriamsp.mx",
		Nombre:      "De Baja",
	})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Equal(t, "usuario_ya_existe", problemCode(t, rec),
		"the deactivated row must collide like any other, NOT surface usuario_inactivo (403)")

	// Nothing moved: still deactivated, still without a uid, still the ugly name.
	stored, findErr := rig.usuarios.FindByEmail(context.Background(), "de.baja@muebleriamsp.mx")
	require.NoError(t, findErr)
	assert.Equal(t, vendedor.ID(), stored.ID())
	assert.False(t, stored.Activo(), "a 409 must leave the row deactivated")
	assert.True(t, stored.FirebaseUID().IsZero(), "no uid may be attached to a deactivated row")
	assert.Equal(t, domain.EstatusVendedorOnly, stored.Estatus())
	assert.Equal(t, "de.baja", stored.Nombre().Value(), "the nombre must not move either")
	_, findErr = rig.usuarios.FindByFirebaseUID(context.Background(), "fbuid-de-baja")
	require.ErrorIs(t, findErr, domain.ErrUsuarioNotFound, "no row may claim the alta uid")
}

// TestCrearUsuario_CorreoDeFirebaseUser_Returns409 pins the third collision
// cause on the same code: an email already held by a FIREBASE_USER row.
func TestCrearUsuario_CorreoDeFirebaseUser_Returns409(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@muebleriamsp.mx", "Admin")
	existing := rig.seedUsuario(t, "fbuid-ya-esta", "ya.esta@muebleriamsp.mx", "Ya Esta")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		FirebaseUID: "fbuid-segunda-cuenta",
		Email:       existing.Email().Value(),
		Nombre:      "Segunda Cuenta",
	})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Equal(t, "usuario_ya_existe", problemCode(t, rec))

	stored, findErr := rig.usuarios.FindByEmail(context.Background(), existing.Email().Value())
	require.NoError(t, findErr)
	assert.Equal(t, "fbuid-ya-esta", stored.FirebaseUID().Value(), "the uid must not be re-pointed")
}

// TestCrearUsuario_PromocionConUIDDeOtraFila_Returns409 is the trap of the
// promotion path: the UPDATE would hit UQ_MSP_USUARIOS_FIREBASE_UID. It must
// surface as a 409 conflict, never a 500.
func TestCrearUsuario_PromocionConUIDDeOtraFila_Returns409(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@muebleriamsp.mx", "Admin")
	otro := rig.seedUsuario(t, "fbuid-ocupado", "otro@muebleriamsp.mx", "Otro Usuario")
	vendedor := rig.seedVendedorOnly(t, "pendiente@muebleriamsp.mx", "pendiente")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/", CrearUsuarioRequest{
		FirebaseUID: otro.FirebaseUID().Value(),
		Email:       "pendiente@muebleriamsp.mx",
		Nombre:      "Nombre Real",
	})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Equal(t, "usuario_ya_existe", problemCode(t, rec), "must be a 409 conflict, never a 500")

	// Neither row moved: the uid stays with its owner, the vendedor stays raw.
	stillOtro, findErr := rig.usuarios.FindByFirebaseUID(context.Background(), otro.FirebaseUID().Value())
	require.NoError(t, findErr)
	assert.Equal(t, otro.ID(), stillOtro.ID())
	stored, findErr := rig.usuarios.FindByID(context.Background(), vendedor.ID())
	require.NoError(t, findErr)
	assert.Equal(t, domain.EstatusVendedorOnly, stored.Estatus())
	assert.Equal(t, "pendiente", stored.Nombre().Value())
}

// (var _ used to silence unused complaints if helpers are pulled lazily.)
var _ outbound.UsuarioRepo = (*fakeUsuarioRepo)(nil)
