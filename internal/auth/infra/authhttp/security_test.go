package authhttp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// secNewUUID returns a fresh uuid for synthetic CurrentUsers in the authz
// sweep — we never persist this user, so the value is irrelevant beyond
// being a valid uuid.UUID.
func secNewUUID() uuid.UUID { return uuid.New() }

// secProtectedRoute is one row of the authn/authz sweep table.
type secProtectedRoute struct {
	method       string
	path         string
	requiredPerm domain.Permission // perm required to reach the handler (used by authz sweep)
}

// secProtectedRoutes enumerates every route mounted by MountRouter except the
// anonymous POST /auth/login. Path UUID segments are static so the table can
// be reused across the authn and authz sweeps.
var secProtectedRoutes = []secProtectedRoute{
	{http.MethodGet, "/me", ""},
	{http.MethodGet, "/usuarios/", domain.PermUsuariosListar},
	{http.MethodGet, "/usuarios/00000000-0000-0000-0000-000000000001", domain.PermUsuariosVer},
	{http.MethodPatch, "/usuarios/00000000-0000-0000-0000-000000000001", domain.PermUsuariosActualizar},
	{http.MethodDelete, "/usuarios/00000000-0000-0000-0000-000000000001", domain.PermUsuariosDesactivar},
	{http.MethodPost, "/usuarios/00000000-0000-0000-0000-000000000001/roles", domain.PermUsuariosAsignarRol},
	{http.MethodDelete, "/usuarios/00000000-0000-0000-0000-000000000001/roles/00000000-0000-0000-0000-000000000002", domain.PermUsuariosAsignarRol},
	{http.MethodGet, "/roles/", domain.PermRolesListar},
	{http.MethodGet, "/roles/00000000-0000-0000-0000-000000000001", domain.PermRolesListar},
	{http.MethodPost, "/roles/", domain.PermRolesCrear},
	{http.MethodPatch, "/roles/00000000-0000-0000-0000-000000000001", domain.PermRolesActualizar},
	{http.MethodDelete, "/roles/00000000-0000-0000-0000-000000000001", domain.PermRolesActualizar},
	{http.MethodPost, "/roles/00000000-0000-0000-0000-000000000001/permisos", domain.PermRolesAsignarPermiso},
	{http.MethodDelete, "/roles/00000000-0000-0000-0000-000000000001/permisos/usuarios:listar", domain.PermRolesAsignarPermiso},
	{http.MethodGet, "/permisos", domain.PermPermisosListar},
}

// secAcceptedAuthnCodes is the SET of stable problem `code` values the authn
// middleware is allowed to surface for an un-authenticated request.
var secAcceptedAuthnCodes = map[string]struct{}{
	"missing_authorization":   {},
	"invalid_authorization":   {},
	"invalid_token":           {},
	"firebase_not_configured": {},
	"unauthenticated":         {},
}

// secProblemBody is the minimal projection of a Problem Details document we
// need for the assertions.
type secProblemBody struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
}

// TestSecurity_Authn_NoBearer_Returns401 verifies every protected endpoint
// rejects a request without an Authorization header with a 401 + stable code.
func TestSecurity_Authn_NoBearer_Returns401(t *testing.T) {
	t.Parallel()
	for _, rt := range secProtectedRoutes {
		rt := rt
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			t.Parallel()
			rig := newTestRig(t)
			r := chi.NewRouter()
			MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())

			req := httptest.NewRequest(rt.method, rt.path, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code, "want 401, got %d body=%s", rec.Code, rec.Body.String())
			assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")

			var body secProblemBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, http.StatusUnauthorized, body.Status)
			_, ok := secAcceptedAuthnCodes[body.Code]
			assert.Truef(t, ok, "unexpected code %q for %s %s", body.Code, rt.method, rt.path)
		})
	}
}

// TestSecurity_Authn_InvalidBearerScheme_Returns401 swaps Bearer for a wrong
// scheme on every protected endpoint and asserts the same 401 contract.
func TestSecurity_Authn_InvalidBearerScheme_Returns401(t *testing.T) {
	t.Parallel()
	for _, rt := range secProtectedRoutes {
		rt := rt
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			t.Parallel()
			rig := newTestRig(t)
			r := chi.NewRouter()
			MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())

			req := httptest.NewRequest(rt.method, rt.path, nil)
			req.Header.Set("Authorization", "Basic deadbeef")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
			var body secProblemBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			_, ok := secAcceptedAuthnCodes[body.Code]
			assert.Truef(t, ok, "unexpected code %q", body.Code)
		})
	}
}

// secMountWithLimitedPerms mounts the protected handlers behind a middleware
// that plants a CurrentUser carrying only the supplied permissions. Used by
// the authz sweep to probe every endpoint with an insufficient principal.
func secMountWithLimitedPerms(rig *testRig, held ...domain.Permission) chi.Router {
	codes := make([]string, len(held))
	for i, p := range held {
		codes[i] = string(p)
	}
	cu := auth.CurrentUser{
		ID:       secNewUUID(),
		Email:    "limited@example.com",
		Nombre:   "Limited",
		Permisos: codes,
	}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.PlantCurrentUser(req.Context(), cu)))
		})
	})
	h := NewHandlers(rig.svc, rig.usuarios)
	r.Get("/me", h.Me)
	r.Route("/usuarios", func(r chi.Router) {
		r.With(RequirePermission(domain.PermUsuariosListar)).Get("/", h.ListarUsuarios)
		r.With(RequirePermission(domain.PermUsuariosVer)).Get("/{id}", h.ObtenerUsuario)
		r.With(RequirePermission(domain.PermUsuariosActualizar)).Patch("/{id}", h.ActualizarUsuario)
		r.With(RequirePermission(domain.PermUsuariosDesactivar)).Delete("/{id}", h.DesactivarUsuario)
		r.With(RequirePermission(domain.PermUsuariosAsignarRol)).Post("/{id}/roles", h.AsignarRolAUsuario)
		r.With(RequirePermission(domain.PermUsuariosAsignarRol)).Delete("/{id}/roles/{rol_id}", h.RevocarRolDeUsuario)
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

// TestSecurity_Authz_InsufficientPermission_Returns403 plants a CurrentUser
// holding only PermPermisosListar and probes every other endpoint, expecting
// a 403 with the stable `permission_denied` code.
func TestSecurity_Authz_InsufficientPermission_Returns403(t *testing.T) {
	t.Parallel()
	for _, rt := range secProtectedRoutes {
		rt := rt
		if rt.requiredPerm == "" || rt.requiredPerm == domain.PermPermisosListar {
			// /me requires no perm; the perm we hold trivially passes for permisos.
			continue
		}
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			t.Parallel()
			rig := newTestRig(t)
			r := secMountWithLimitedPerms(rig, domain.PermPermisosListar)

			var body io.Reader
			switch rt.method {
			case http.MethodPost, http.MethodPatch:
				// Minimal JSON payload; the authz check runs before body parsing.
				body = bytes.NewReader([]byte(`{}`))
			}
			req := httptest.NewRequest(rt.method, rt.path, body)
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			require.Equal(t, http.StatusForbidden, rec.Code, "want 403, got %d body=%s", rec.Code, rec.Body.String())
			assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")
			var pb secProblemBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pb))
			assert.Equal(t, "permission_denied", pb.Code)
		})
	}
}

// TestSecurity_Authz_NoPermissions_Returns403 plants a CurrentUser with an
// empty permisos slice. Every endpoint guarded by RequirePermission must 403.
func TestSecurity_Authz_NoPermissions_Returns403(t *testing.T) {
	t.Parallel()
	for _, rt := range secProtectedRoutes {
		rt := rt
		if rt.requiredPerm == "" {
			continue
		}
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			t.Parallel()
			rig := newTestRig(t)
			r := secMountWithLimitedPerms(rig /* no perms */)

			var body io.Reader
			switch rt.method {
			case http.MethodPost, http.MethodPatch:
				body = bytes.NewReader([]byte(`{}`))
			}
			req := httptest.NewRequest(rt.method, rt.path, body)
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
			var pb secProblemBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pb))
			assert.Equal(t, "permission_denied", pb.Code)
		})
	}
}

// secSQLPayloads enumerates the classic SQL-injection probes we ship into
// the path / body of every endpoint to assert that the API rejects them with
// a stable validation error.
var secSQLPayloads = []string{
	"'; DROP TABLE MSP_USUARIOS; --",
	"' OR 1=1 --",
	"'; SELECT * FROM MSP_USUARIOS; --",
	"1' UNION SELECT NULL--",
	"\"; DROP TABLE MSP_ROLES; --",
}

// TestSecurity_SQLInjection_PathParamRejected sends each payload as a UUID
// path parameter on GET /v2/usuarios/{id}. With parameterized SQL upstream,
// the request fails parsing in the DTO layer and surfaces a 4xx.
func TestSecurity_SQLInjection_PathParamRejected(t *testing.T) {
	t.Parallel()
	for _, p := range secSQLPayloads {
		p := p
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			rig := newTestRig(t)
			caller := rig.seedUsuario(t, "fbuid-admin", "admin@example.com", "Admin")
			r := mountWithCurrentUser(rig, adminCurrentUser(caller))

			req := httptest.NewRequest(http.MethodGet, "/usuarios/"+url.PathEscape(p), nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			require.GreaterOrEqual(t, rec.Code, 400)
			require.Less(t, rec.Code, 500, "must not 5xx on SQL probe; got %d body=%s", rec.Code, rec.Body.String())
			assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")
			var pb secProblemBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pb))
			assert.NotEmpty(t, pb.Code, "every 4xx must carry a stable code")
		})
	}
}

// TestSecurity_SQLInjection_LoginBody_Rejected stuffs each payload into the
// id_token field of the login body. The expectation is a 4xx with a stable
// code; the field is opaque to the API (firebase rejects it) so we accept any
// 4xx response.
func TestSecurity_SQLInjection_LoginBody_Rejected(t *testing.T) {
	t.Parallel()
	for _, p := range secSQLPayloads {
		p := p
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			rig := newTestRig(t)
			// Force VerifyIDToken to error out so the payload is not silently
			// accepted; this mirrors what the real firebase SDK would do.
			rig.firebase.Err = secVerifyError{}

			r := chi.NewRouter()
			MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())

			payload := map[string]string{"id_token": p}
			buf := &bytes.Buffer{}
			require.NoError(t, json.NewEncoder(buf).Encode(payload))

			req := httptest.NewRequest(http.MethodPost, "/auth/login", buf)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			require.GreaterOrEqual(t, rec.Code, 400)
			require.Less(t, rec.Code, 500, "must not 5xx; got %d body=%s", rec.Code, rec.Body.String())
			assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")
			var pb secProblemBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pb))
			assert.NotEmpty(t, pb.Code, "every 4xx must carry a stable code")
			// The literal payload must not appear verbatim in the response
			// body — we want machine-readable codes, not echoed input.
			assert.NotContains(t, rec.Body.String(), "DROP TABLE")
			assert.NotContains(t, rec.Body.String(), "UNION SELECT")
		})
	}
}

// secVerifyError is a sentinel error wired into the fake firebase client
// so the SQL injection sweep on /auth/login produces a deterministic 4xx.
type secVerifyError struct{}

func (secVerifyError) Error() string { return "verify failed" }

// TestSecurity_StableCodes_AllRejectionsCarryCode is a property-style assertion
// over every 4xx response in the previously-defined sweeps: each response body
// must have a non-empty `code` field. Re-running the no-bearer sweep here makes
// the property explicit and isolates it from changes elsewhere.
func TestSecurity_StableCodes_AllRejectionsCarryCode(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	r := chi.NewRouter()
	MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())

	probes := []struct{ method, path string }{
		{http.MethodGet, "/me"},
		{http.MethodGet, "/usuarios/"},
		{http.MethodGet, "/roles/"},
		{http.MethodGet, "/permisos"},
		{http.MethodPost, "/auth/login"},
	}
	for _, p := range probes {
		p := p
		t.Run(p.method+" "+p.path, func(t *testing.T) {
			t.Parallel()
			var body io.Reader
			if p.method == http.MethodPost {
				body = strings.NewReader(`{"id_token":""}`)
			}
			req := httptest.NewRequest(p.method, p.path, body)
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.GreaterOrEqual(t, rec.Code, 400)
			require.Less(t, rec.Code, 500)
			var pb secProblemBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pb))
			assert.NotEmptyf(t, pb.Code, "missing stable code for %s %s body=%s", p.method, p.path, rec.Body.String())
		})
	}
}
