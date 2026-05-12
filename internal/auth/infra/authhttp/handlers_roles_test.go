package authhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/domain"
)

func TestCrearRol_HappyPath(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	desc := "test rol"
	rec := doRequestRouter(t, r, http.MethodPost, "/roles/", CrearRolRequest{Nombre: "vendedor", Description: &desc})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp RolResponse
	decodeBody(t, rec, &resp)
	assert.Equal(t, "vendedor", resp.Nombre)
	assert.False(t, resp.Inmutable)
}

func TestCrearRol_MissingNombre_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/roles/", CrearRolRequest{Nombre: ""})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestActualizarRol_HappyPath(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	rol := rig.seedRol(t, "vendedor")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPatch, "/roles/"+rol.ID().String(), ActualizarRolRequest{Nombre: "supervisor"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp RolResponse
	decodeBody(t, rec, &resp)
	assert.Equal(t, "supervisor", resp.Nombre)
}

func TestListarRoles_HappyPath(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	rig.seedRol(t, "vendedor")
	rig.seedRol(t, "supervisor")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/roles/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp ListResponse[RolResponse]
	decodeBody(t, rec, &resp)
	assert.Len(t, resp.Items, 2)
}

func TestObtenerRol_NotFound_Returns404(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/roles/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDesactivarRol_Returns204(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	rol := rig.seedRol(t, "vendedor")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodDelete, "/roles/"+rol.ID().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestAsignarPermisoARol_Returns204(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	rol := rig.seedRol(t, "vendedor")
	rig.seedPermiso(t, domain.PermUsuariosListar)

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/roles/"+rol.ID().String()+"/permisos", AsignarPermisoRequest{Codigo: string(domain.PermUsuariosListar)})
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestAsignarPermisoARol_UnknownPermiso_Returns404(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	rol := rig.seedRol(t, "vendedor")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPost, "/roles/"+rol.ID().String()+"/permisos", AsignarPermisoRequest{Codigo: "does:not_exist"})
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestRevocarPermisoDeRol_Returns204(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	rol := rig.seedRol(t, "vendedor")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodDelete, "/roles/"+rol.ID().String()+"/permisos/"+string(domain.PermUsuariosListar), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestListarPermisos_HappyPath(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
	rig.seedPermiso(t, domain.PermUsuariosListar)
	rig.seedPermiso(t, domain.PermRolesListar)

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/permisos", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp ListResponse[PermisoResponse]
	decodeBody(t, rec, &resp)
	assert.Len(t, resp.Items, 2)
}

func TestListarPermisos_NoPermission_Returns403(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	caller := rig.seedUsuario(t, "fbuid-no", "no@example.com", "No Perms")

	// no permisos planted
	r := mountWithCurrentUser(rig, auth.CurrentUser{ID: caller.ID()})
	req := httptest.NewRequest(http.MethodGet, "/permisos", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}
