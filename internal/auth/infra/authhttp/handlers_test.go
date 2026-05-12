package authhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
	"github.com/abdimuy/msp-api/internal/auth/app"
	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
)

// testRig bundles the moving parts a single test case needs. Built fresh per
// test so cases stay isolated.
type testRig struct {
	svc       *app.Service
	usuarios  *fakeUsuarioRepo
	roles     *fakeRolRepo
	permisos  *fakePermisoRepo
	firebase  *fakeFirebase
	clockTime time.Time
}

func newTestRig(t *testing.T) *testRig {
	t.Helper()
	usuarios := newFakeUsuarioRepo()
	roles := newFakeRolRepo()
	permisos := newFakePermisoRepo()
	fb := &fakeFirebase{}
	clk := fixedClock{T: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	svc := app.NewService(usuarios, roles, permisos, clk, &fakeOutbox{}, fb, nil)
	return &testRig{
		svc:       svc,
		usuarios:  usuarios,
		roles:     roles,
		permisos:  permisos,
		firebase:  fb,
		clockTime: clk.T,
	}
}

// seedUsuario adds a usuario to the fake repo and returns it.
func (r *testRig) seedUsuario(t *testing.T, fuid, email, nombre string) *domain.Usuario {
	t.Helper()
	id := uuid.New()
	fid, err := domain.NewFirebaseUID(fuid)
	require.NoError(t, err)
	em, err := domain.NewEmail(email)
	require.NoError(t, err)
	nm, err := domain.NewNombre(nombre)
	require.NoError(t, err)
	u := domain.NewUsuario(id, fid, em, nm, nil, nil, id, r.clockTime)
	require.NoError(t, r.usuarios.Save(context.Background(), u))
	return u
}

// seedRol creates a mutable rol in the fake repo and returns it.
func (r *testRig) seedRol(t *testing.T, nombre string) *domain.Rol {
	t.Helper()
	rol, err := domain.NewRol(uuid.New(), nombre, nil, false, uuid.New(), r.clockTime)
	require.NoError(t, err)
	require.NoError(t, r.roles.Save(context.Background(), rol))
	return rol
}

// seedPermiso adds a permiso to the catalog so AsignarPermisoARol can succeed.
func (r *testRig) seedPermiso(t *testing.T, code domain.Permission) {
	t.Helper()
	require.NoError(t, r.permisos.UpsertCatalog(context.Background(), []domain.PermissionMeta{{
		Code:        code,
		Description: "test permission",
		Categoria:   "test",
	}}))
}

// asUser builds a context with the given usuario as CurrentUser, holding the
// supplied permission codes.
func asUser(u *domain.Usuario, perms ...domain.Permission) context.Context {
	codes := make([]string, len(perms))
	for i, p := range perms {
		codes[i] = string(p)
	}
	return auth.PlantCurrentUser(context.Background(), auth.CurrentUser{
		ID:          u.ID(),
		FirebaseUID: u.FirebaseUID().Value(),
		Email:       u.Email().Value(),
		Nombre:      u.Nombre().Value(),
		Permisos:    codes,
	})
}

// allPermissions returns every permission code declared in the domain — used
// to build a "god mode" CurrentUser for handlers that exercise the entire
// authorization chain in passing-cases.
func allPermissions() []domain.Permission {
	all := domain.AllPermissions()
	codes := make([]domain.Permission, len(all))
	for i, p := range all {
		codes[i] = p.Code
	}
	return codes
}

func doRequest(ctx context.Context, t *testing.T, rig *testRig, method, target string, body any) *httptest.ResponseRecorder { //nolint:unparam // body kept generic for future use
	t.Helper()
	r := chi.NewRouter()
	MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())
	var reader io.Reader
	if body != nil {
		buf := &bytes.Buffer{}
		require.NoError(t, json.NewEncoder(buf).Encode(body))
		reader = buf
	}
	req := httptest.NewRequestWithContext(ctx, method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// decodeBody decodes the response body into v.
func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(rec.Body).Decode(v))
}

// doJSONRequest issues a JSON request through an arbitrary router. The body
// may be nil for GET/DELETE.
func doJSONRequest(r chi.Router, method, target string, body any) (*httptest.ResponseRecorder, error) {
	var reader io.Reader
	if body != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			return nil, err
		}
		reader = buf
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec, nil
}

// ─── Login ──────────────────────────────────────────────────────────────────

func TestLogin_HappyPath_CreatesUsuario(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	rig.firebase.Token = &outbound.FirebaseToken{
		UID:   "fbuid-new",
		Email: "new@example.com",
		Name:  "New User",
	}

	rec := doRequest(context.Background(), t, rig, http.MethodPost, "/auth/login", map[string]string{"id_token": "tok"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp CurrentUserResponse
	decodeBody(t, rec, &resp)
	assert.Equal(t, "new@example.com", resp.Email)
	assert.Equal(t, "fbuid-new", resp.FirebaseUID)
}

func TestLogin_MissingIDToken_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	rec := doRequest(context.Background(), t, rig, http.MethodPost, "/auth/login", map[string]string{})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

func TestLogin_FirebaseError_Surfaces401(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	rig.firebase.Err = errors.New("verify failed")
	rec := doRequest(context.Background(), t, rig, http.MethodPost, "/auth/login", map[string]string{"id_token": "tok"})
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func TestLogin_BadJSON_Returns422(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	r := chi.NewRouter()
	MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader("{not-json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

// ─── Me / Authn ─────────────────────────────────────────────────────────────

func TestMe_NoAuthHeader_Returns401(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	rec := doRequest(context.Background(), t, rig, http.MethodGet, "/me", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func TestMe_WithBearer_VerifiesAndReturnsUser(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	u := rig.seedUsuario(t, "fbuid-1", "u1@example.com", "User One")
	rig.firebase.Token = &outbound.FirebaseToken{UID: u.FirebaseUID().Value(), Email: u.Email().Value()}

	r := chi.NewRouter()
	MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp CurrentUserResponse
	decodeBody(t, rec, &resp)
	assert.Equal(t, u.Email().Value(), resp.Email)
}

func TestMe_InvalidAuthorizationScheme_Returns401(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)

	r := chi.NewRouter()
	MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Basic abcd")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMe_DeactivatedUsuario_Returns403(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	u := rig.seedUsuario(t, "fbuid-x", "x@example.com", "X User")
	u.Desactivar(u.ID(), rig.clockTime)
	require.NoError(t, rig.usuarios.Update(context.Background(), u))
	rig.firebase.Token = &outbound.FirebaseToken{UID: u.FirebaseUID().Value()}

	r := chi.NewRouter()
	MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}
