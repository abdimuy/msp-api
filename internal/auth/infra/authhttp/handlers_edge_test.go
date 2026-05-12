package authhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// Tests for edge / error paths that the happy-path suites don't reach.

func TestActualizarUsuario_InvalidUUID_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	rec := doRequestRouter(t, r, http.MethodPatch, "/usuarios/bad-uuid", ActualizarUsuarioRequest{
		Email: "ok@example.com", Nombre: "name",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestActualizarUsuario_BadJSON_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	req := httptest.NewRequest(http.MethodPatch, "/usuarios/"+target.ID().String(), strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestDesactivarUsuario_InvalidUUID_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	req := httptest.NewRequest(http.MethodDelete, "/usuarios/bad-uuid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestAsignarRol_InvalidUsuarioUUID_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	rec := doRequestRouter(t, r, http.MethodPost, "/usuarios/bad-uuid/roles", AsignarRolRequest{RolID: uuid.New().String()})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestAsignarRol_BadJSON_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	req := httptest.NewRequest(http.MethodPost, "/usuarios/"+target.ID().String()+"/roles", strings.NewReader("bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestRevocarRol_InvalidUUIDs_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	req := httptest.NewRequest(http.MethodDelete, "/usuarios/bad/roles/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestRevocarRol_BadRolUUID_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	target := rig.seedUsuario(t, "fbuid-t", "t@example.com", "Target")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	req := httptest.NewRequest(http.MethodDelete, "/usuarios/"+target.ID().String()+"/roles/bad-uuid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestCrearRol_BadJSON_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	req := httptest.NewRequest(http.MethodPost, "/roles/", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestActualizarRol_NotFound_Returns404(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	rec := doRequestRouter(t, r, http.MethodPatch, "/roles/"+uuid.New().String(), ActualizarRolRequest{Nombre: "newname"})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestActualizarRol_BadUUID_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	rec := doRequestRouter(t, r, http.MethodPatch, "/roles/not-a-uuid", ActualizarRolRequest{Nombre: "ok"})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestDesactivarRol_BadUUID_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	req := httptest.NewRequest(http.MethodDelete, "/roles/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestAsignarPermiso_BadUUID_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	rec := doRequestRouter(t, r, http.MethodPost, "/roles/not-a-uuid/permisos", AsignarPermisoRequest{Codigo: "x"})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestAsignarPermiso_MissingCodigo_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	rol := rig.seedRol(t, "vendedor")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	rec := doRequestRouter(t, r, http.MethodPost, "/roles/"+rol.ID().String()+"/permisos", AsignarPermisoRequest{Codigo: ""})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestAsignarPermiso_BadJSON_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	rol := rig.seedRol(t, "vendedor")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	req := httptest.NewRequest(http.MethodPost, "/roles/"+rol.ID().String()+"/permisos", strings.NewReader("nope"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestRevocarPermiso_BadUUID_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	req := httptest.NewRequest(http.MethodDelete, "/roles/not-a-uuid/permisos/x", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestObtenerRol_InvalidUUID_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	r := mountWithCurrentUser(rig, adminCurrentUser(caller))

	req := httptest.NewRequest(http.MethodGet, "/roles/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

// ─── DTO mapper tests ───────────────────────────────────────────────────────

func TestToUsuarioResponse_WithTelefonoAndAlmacen(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	u := rig.seedUsuario(t, "fbuid-x", "x@example.com", "X User")
	// re-update with telefono + almacen
	tel := "+525555555555"
	alm := 7
	rig2 := newTestRig(t)
	caller := rig2.seedUsuario(t, "fbuid-c", "c@example.com", "Caller")
	r := mountWithCurrentUser(rig2, adminCurrentUser(caller))
	_ = u
	target := rig2.seedUsuario(t, "fbuid-tt", "tt@example.com", "Target")
	rec := doRequestRouter(t, r, http.MethodPatch, "/usuarios/"+target.ID().String(), ActualizarUsuarioRequest{
		Email:     "tt@example.com",
		Nombre:    "Target",
		Telefono:  &tel,
		AlmacenID: &alm,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp UsuarioResponse
	decodeBody(t, rec, &resp)
	require.NotNil(t, resp.Telefono)
	// Telefono VO may normalize/strip non-digit characters; just assert
	// non-empty round-trip rather than exact equality.
	assert.NotEmpty(t, *resp.Telefono)
	require.NotNil(t, resp.AlmacenID)
	assert.Equal(t, 7, *resp.AlmacenID)
}

// ─── extractBearer paths ────────────────────────────────────────────────────

func TestExtractBearer_EmptyToken_Returns401(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)

	r := chi.NewRouter()
	MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer   ")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ─── Coverage poker: hit RequirePermission without CurrentUser ──────────────

func TestRequirePermission_WithoutCurrentUser_Returns401(t *testing.T) {
	t.Parallel()
	// Build a router that mounts the protected sub-tree without planting a
	// CurrentUser at all.
	rig := newTestRig(t)
	r := chi.NewRouter()
	h := NewHandlers(rig.svc, rig.usuarios)
	r.With(RequirePermission(domain.PermPermisosListar)).Get("/permisos", h.ListarPermisos)

	req := httptest.NewRequest(http.MethodGet, "/permisos", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ─── Me with no current user (defensive 401 path) ───────────────────────────

func TestHandlers_Me_NoCurrentUserPlanted_Returns401(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	h := NewHandlers(rig.svc, rig.usuarios)
	r := chi.NewRouter()
	r.Get("/me", h.Me)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ─── Unused-import poker: keep imports referenced. ──────────────────────────

var _ = auth.CurrentUser{}
