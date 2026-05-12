package authhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
)

// ─── helpers ────────────────────────────────────────────────────────────────

// contractSpecPath is the location of the hand-written OpenAPI document,
// resolved relative to this test file's directory.
const contractSpecPath = "../../../../api/openapi.yaml"

// loadSpec loads and validates the OpenAPI 3.1 document.
func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	spec, err := loader.LoadFromFile(contractSpecPath)
	require.NoError(t, err, "spec must load")
	require.NoError(t, spec.Validate(context.Background()), "spec must be valid OpenAPI 3.1")
	return spec
}

// contractRig pairs a test rig with a router pre-mounted under the /v2 prefix
// declared as the server in the spec. The spec's server URL is `/v2`, so the
// router must mount the auth module under that same prefix for path matching
// to line up.
type contractRig struct {
	rig    *testRig
	router chi.Router
}

func newContractRig(t *testing.T) *contractRig {
	t.Helper()
	rig := newTestRig(t)
	root := chi.NewRouter()
	root.Route("/v2", func(r chi.Router) {
		MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())
	})
	return &contractRig{rig: rig, router: root}
}

// contractRequest is a small helper for building JSON requests that go through
// the spec validation path. It returns the populated request (caller may add
// headers afterwards).
func contractRequest(t *testing.T, method, target string, body any) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != nil {
		buf := &bytes.Buffer{}
		require.NoError(t, json.NewEncoder(buf).Encode(body))
		reader = buf
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

// contractAuthFunc accepts any bearer token a test supplies — the contract
// suite trusts the handler-level authn middleware to do the real verification.
// Without this, openapi3filter rejects the request with "missing
// AuthenticationFunc" because the spec applies a bearer security requirement.
func contractAuthFunc(_ context.Context, _ *openapi3filter.AuthenticationInput) error {
	return nil
}

// contractValidateExchange runs both request and response validation around
// a single HTTP exchange. The request must be validated BEFORE serving so the
// body is still readable; the response is validated AFTER serving against the
// captured recorder.
//
// Returns the recorder so callers can make additional assertions on the
// handler output.
func contractValidateExchange(t *testing.T, c *contractRig, router routers.Router, req *http.Request, expectStatus int) *httptest.ResponseRecorder {
	t.Helper()

	route, pathParams, err := router.FindRoute(req)
	require.NoError(t, err, "spec must declare route %s %s", req.Method, req.URL.Path)

	opts := &openapi3filter.Options{
		IncludeResponseStatus: true,
		AuthenticationFunc:    contractAuthFunc,
	}

	// Snapshot the body so the handler sees the same bytes the validator did.
	var bodyBytes []byte
	if req.Body != nil {
		buf, readErr := io.ReadAll(req.Body)
		require.NoError(t, readErr)
		bodyBytes = buf
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Only validate request shape on positive paths — negative tests
	// intentionally send malformed input.
	if expectStatus >= 200 && expectStatus < 300 {
		err = openapi3filter.ValidateRequest(req.Context(), &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
			Options:    opts,
		})
		require.NoError(t, err, "request must be valid per spec")
		// Restore the body for the handler after the validator consumed it.
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	rec := c.serve(req)
	require.Equal(t, expectStatus, rec.Code, rec.Body.String())

	err = openapi3filter.ValidateResponse(req.Context(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
			Options:    opts,
		},
		Status:  rec.Code,
		Header:  rec.Header(),
		Body:    io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		Options: opts,
	})
	require.NoError(t, err, "response must conform to spec: status=%d body=%s", rec.Code, rec.Body.String())
	return rec
}

// newContractRouter loads + validates the spec and builds the gorillamux router.
func newContractRouter(t *testing.T) routers.Router {
	t.Helper()
	spec := loadSpec(t)
	router, err := gorillamux.NewRouter(spec)
	require.NoError(t, err)
	return router
}

// serve runs req against the rig's mounted router and returns the recorder.
func (c *contractRig) serve(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	c.router.ServeHTTP(rec, req)
	return rec
}

// seedAuthedUser plants the firebase token + usuario row needed for an
// authenticated request to succeed past the authn middleware. Returns the
// seeded usuario for further setup (eg role assignment).
func (c *contractRig) seedAuthedUser(t *testing.T, fuid, email, nombre string, perms ...domain.Permission) *domain.Usuario {
	t.Helper()
	u := c.rig.seedUsuario(t, fuid, email, nombre)
	c.rig.firebase.Token = &outbound.FirebaseToken{UID: fuid, Email: email, Name: nombre}
	codes := make([]domain.Permission, len(perms))
	copy(codes, perms)
	c.rig.usuarios.Permisos[u.ID()] = codes
	return u
}

// ─── tests ──────────────────────────────────────────────────────────────────

// TestContract_SpecIsValid ensures the YAML parses and is internally consistent.
func TestContract_SpecIsValid(t *testing.T) {
	t.Parallel()
	_ = loadSpec(t)
}

// TestContract_Login_OK validates POST /v2/auth/login happy path.
func TestContract_Login_OK(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.rig.firebase.Token = &outbound.FirebaseToken{UID: "fbuid-c-login", Email: "c-login@example.com", Name: "C Login"}

	req := contractRequest(t, http.MethodPost, "/v2/auth/login", map[string]string{"id_token": "tok"})
	contractValidateExchange(t, c, router, req, http.StatusOK)
}

// TestContract_Login_BadRequest validates POST /v2/auth/login 422.
func TestContract_Login_BadRequest(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	req := contractRequest(t, http.MethodPost, "/v2/auth/login", map[string]string{})
	contractValidateExchange(t, c, router, req, http.StatusUnprocessableEntity)
}

// TestContract_Me_OK validates GET /v2/me happy path.
func TestContract_Me_OK(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-me", "me@example.com", "Me User")

	req := contractRequest(t, http.MethodGet, "/v2/me", nil)
	req.Header.Set("Authorization", "Bearer dev:me")
	contractValidateExchange(t, c, router, req, http.StatusOK)
}

// TestContract_Me_Unauthorized validates GET /v2/me with no Authorization header.
func TestContract_Me_Unauthorized(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	req := contractRequest(t, http.MethodGet, "/v2/me", nil)
	contractValidateExchange(t, c, router, req, http.StatusUnauthorized)
}

// TestContract_ListarUsuarios_OK validates GET /v2/usuarios/ happy path.
func TestContract_ListarUsuarios_OK(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-lu", "lu@example.com", "List User", domain.PermUsuariosListar)

	req := contractRequest(t, http.MethodGet, "/v2/usuarios/", nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusOK)
}

// TestContract_ListarUsuarios_Forbidden validates GET /v2/usuarios/ 403.
func TestContract_ListarUsuarios_Forbidden(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-lu-f", "lu-f@example.com", "No Perm")

	req := contractRequest(t, http.MethodGet, "/v2/usuarios/", nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusForbidden)
}

// TestContract_ObtenerUsuario_OK validates GET /v2/usuarios/{id} happy path.
func TestContract_ObtenerUsuario_OK(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	u := c.seedAuthedUser(t, "fbuid-ou", "ou@example.com", "Obtener User", domain.PermUsuariosVer)

	req := contractRequest(t, http.MethodGet, "/v2/usuarios/"+u.ID().String(), nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusOK)
}

// TestContract_ObtenerUsuario_NotFound validates GET /v2/usuarios/{id} 404.
func TestContract_ObtenerUsuario_NotFound(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-ou-nf", "ou-nf@example.com", "Not Found", domain.PermUsuariosVer)

	req := contractRequest(t, http.MethodGet, "/v2/usuarios/"+uuid.New().String(), nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusNotFound)
}

// TestContract_ObtenerUsuario_InvalidUUID validates GET /v2/usuarios/{id} 422.
func TestContract_ObtenerUsuario_InvalidUUID(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-ou-iu", "ou-iu@example.com", "Invalid UUID", domain.PermUsuariosVer)

	req := contractRequest(t, http.MethodGet, "/v2/usuarios/not-a-uuid", nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusUnprocessableEntity)
}

// TestContract_ActualizarUsuario_OK validates PATCH /v2/usuarios/{id} happy path.
func TestContract_ActualizarUsuario_OK(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	u := c.seedAuthedUser(t, "fbuid-au", "au@example.com", "Actualizar User", domain.PermUsuariosActualizar)

	body := ActualizarUsuarioRequest{Email: "au-new@example.com", Nombre: "New Name"}
	req := contractRequest(t, http.MethodPatch, "/v2/usuarios/"+u.ID().String(), body)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusOK)
}

// TestContract_ActualizarUsuario_Validation validates PATCH /v2/usuarios/{id} 422.
func TestContract_ActualizarUsuario_Validation(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	u := c.seedAuthedUser(t, "fbuid-au-v", "au-v@example.com", "Au Validation", domain.PermUsuariosActualizar)

	// Empty body fails validator (email required).
	body := ActualizarUsuarioRequest{}
	req := contractRequest(t, http.MethodPatch, "/v2/usuarios/"+u.ID().String(), body)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusUnprocessableEntity)
}

// TestContract_DesactivarUsuario_NoContent validates DELETE /v2/usuarios/{id} 204.
func TestContract_DesactivarUsuario_NoContent(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	u := c.seedAuthedUser(t, "fbuid-du", "du@example.com", "Desactivar User", domain.PermUsuariosDesactivar)

	req := contractRequest(t, http.MethodDelete, "/v2/usuarios/"+u.ID().String(), nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusNoContent)
}

// TestContract_AsignarRolAUsuario_NoContent validates POST /v2/usuarios/{id}/roles 204.
func TestContract_AsignarRolAUsuario_NoContent(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	u := c.seedAuthedUser(t, "fbuid-ar", "ar@example.com", "Asignar Rol", domain.PermUsuariosAsignarRol)
	rol := c.rig.seedRol(t, "rol-x")

	body := AsignarRolRequest{RolID: rol.ID().String()}
	req := contractRequest(t, http.MethodPost, "/v2/usuarios/"+u.ID().String()+"/roles", body)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusNoContent)
}

// TestContract_AsignarRolAUsuario_Validation validates POST /v2/usuarios/{id}/roles 422.
func TestContract_AsignarRolAUsuario_Validation(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	u := c.seedAuthedUser(t, "fbuid-ar-v", "ar-v@example.com", "Asignar Rol Bad", domain.PermUsuariosAsignarRol)

	body := AsignarRolRequest{RolID: "not-a-uuid"}
	req := contractRequest(t, http.MethodPost, "/v2/usuarios/"+u.ID().String()+"/roles", body)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusUnprocessableEntity)
}

// TestContract_RevocarRolDeUsuario_NoContent validates DELETE /v2/usuarios/{id}/roles/{rol_id}.
func TestContract_RevocarRolDeUsuario_NoContent(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	u := c.seedAuthedUser(t, "fbuid-rr", "rr@example.com", "Revocar Rol", domain.PermUsuariosAsignarRol)
	rol := c.rig.seedRol(t, "rol-y")

	req := contractRequest(t, http.MethodDelete, "/v2/usuarios/"+u.ID().String()+"/roles/"+rol.ID().String(), nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusNoContent)
}

// TestContract_ListarRoles_OK validates GET /v2/roles/ happy path.
func TestContract_ListarRoles_OK(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-lr", "lr@example.com", "Listar Roles", domain.PermRolesListar)

	req := contractRequest(t, http.MethodGet, "/v2/roles/", nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusOK)
}

// TestContract_CrearRol_Created validates POST /v2/roles/ 201.
func TestContract_CrearRol_Created(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-cr", "cr@example.com", "Crear Rol", domain.PermRolesCrear)

	desc := "Vendedor de campo"
	body := CrearRolRequest{Nombre: "vendedor-ct", Description: &desc}
	req := contractRequest(t, http.MethodPost, "/v2/roles/", body)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusCreated)
}

// TestContract_CrearRol_Validation validates POST /v2/roles/ 422.
func TestContract_CrearRol_Validation(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-cr-v", "cr-v@example.com", "Crear Rol Bad", domain.PermRolesCrear)

	body := CrearRolRequest{Nombre: ""}
	req := contractRequest(t, http.MethodPost, "/v2/roles/", body)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusUnprocessableEntity)
}

// TestContract_ObtenerRol_OK validates GET /v2/roles/{id} happy path.
func TestContract_ObtenerRol_OK(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-or", "or@example.com", "Obtener Rol", domain.PermRolesListar)
	rol := c.rig.seedRol(t, "rol-or")

	req := contractRequest(t, http.MethodGet, "/v2/roles/"+rol.ID().String(), nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusOK)
}

// TestContract_ObtenerRol_NotFound validates GET /v2/roles/{id} 404.
func TestContract_ObtenerRol_NotFound(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-or-nf", "or-nf@example.com", "Obtener Rol NF", domain.PermRolesListar)

	req := contractRequest(t, http.MethodGet, "/v2/roles/"+uuid.New().String(), nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusNotFound)
}

// TestContract_ActualizarRol_OK validates PATCH /v2/roles/{id} happy path.
func TestContract_ActualizarRol_OK(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-aru", "aru@example.com", "Actualizar Rol", domain.PermRolesActualizar)
	rol := c.rig.seedRol(t, "rol-aru")

	body := ActualizarRolRequest{Nombre: "supervisor-ct"}
	req := contractRequest(t, http.MethodPatch, "/v2/roles/"+rol.ID().String(), body)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusOK)
}

// TestContract_DesactivarRol_NoContent validates DELETE /v2/roles/{id} 204.
func TestContract_DesactivarRol_NoContent(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-dr", "dr@example.com", "Desactivar Rol", domain.PermRolesActualizar)
	rol := c.rig.seedRol(t, "rol-dr")

	req := contractRequest(t, http.MethodDelete, "/v2/roles/"+rol.ID().String(), nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusNoContent)
}

// TestContract_AsignarPermisoARol_NoContent validates POST /v2/roles/{id}/permisos.
func TestContract_AsignarPermisoARol_NoContent(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-ap", "ap@example.com", "Asignar Permiso", domain.PermRolesAsignarPermiso)
	rol := c.rig.seedRol(t, "rol-ap")
	c.rig.seedPermiso(t, domain.PermUsuariosListar)

	body := AsignarPermisoRequest{Codigo: string(domain.PermUsuariosListar)}
	req := contractRequest(t, http.MethodPost, "/v2/roles/"+rol.ID().String()+"/permisos", body)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusNoContent)
}

// TestContract_AsignarPermisoARol_NotFound validates POST /v2/roles/{id}/permisos 404.
func TestContract_AsignarPermisoARol_NotFound(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-ap-nf", "ap-nf@example.com", "Asignar Permiso NF", domain.PermRolesAsignarPermiso)
	rol := c.rig.seedRol(t, "rol-ap-nf")

	body := AsignarPermisoRequest{Codigo: "does:not_exist"}
	req := contractRequest(t, http.MethodPost, "/v2/roles/"+rol.ID().String()+"/permisos", body)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusNotFound)
}

// TestContract_RevocarPermisoDeRol_NoContent validates DELETE /v2/roles/{id}/permisos/{codigo}.
func TestContract_RevocarPermisoDeRol_NoContent(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-rp", "rp@example.com", "Revocar Permiso", domain.PermRolesAsignarPermiso)
	rol := c.rig.seedRol(t, "rol-rp")

	req := contractRequest(t, http.MethodDelete, "/v2/roles/"+rol.ID().String()+"/permisos/"+string(domain.PermUsuariosListar), nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusNoContent)
}

// TestContract_ListarPermisos_OK validates GET /v2/permisos happy path.
func TestContract_ListarPermisos_OK(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-lp", "lp@example.com", "Listar Permisos", domain.PermPermisosListar)
	c.rig.seedPermiso(t, domain.PermUsuariosListar)

	req := contractRequest(t, http.MethodGet, "/v2/permisos", nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusOK)
}

// TestContract_ListarPermisos_Forbidden validates GET /v2/permisos 403.
func TestContract_ListarPermisos_Forbidden(t *testing.T) {
	t.Parallel()
	router := newContractRouter(t)
	c := newContractRig(t)
	c.seedAuthedUser(t, "fbuid-lp-f", "lp-f@example.com", "Listar Permisos No")

	req := contractRequest(t, http.MethodGet, "/v2/permisos", nil)
	req.Header.Set("Authorization", "Bearer t")
	contractValidateExchange(t, c, router, req, http.StatusForbidden)
}
