// Composition test for the /v2/auth chain.
//
// Mounts authhttp.MountRouter under /v2 the same way cmd/api/server.go
// provideRootHandler does and exercises end-to-end requests through the
// real chi router + AuthnMiddleware + idempotency middleware +
// RequirePermission stack. Only the outermost adapter boundary (Firebase,
// persistence) is stubbed. This test exists to catch integration bugs that
// per-handler unit tests miss — see commit 2086632 for the canonical
// example (chi RouteContext leak + planted-CurrentUser short-circuit +
// idempotency-key reuse) which only surfaced once the full chain was
// exercised together.
//
//nolint:revive // long test names are intentional for narrative.
package authhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authapp "github.com/abdimuy/msp-api/internal/auth/app"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/infra/authhttp"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/httptesting"
)

// TestE2E_Auth_LoginIdempotency drives POST /v2/auth/login through the
// idempotency middleware:
//   - first call with a key → 200, body returned and cached.
//   - second call with the same key + same body → 200, cached replay
//     (asserted via the Idempotent-Replay response header).
//   - second call with the same key + different body → 409
//     idempotency_key_mismatch (the chain detects request-hash divergence).
func TestE2E_Auth_LoginIdempotency(t *testing.T) {
	t.Parallel()

	const (
		fbUID   = "e2e-login-uid"
		idemKey = "auth-login-key-1"
	)
	fb := httptesting.NewFakeFirebase(fbUID)
	fb.Token.Email = "login@example.invalid"
	fb.Token.Name = "Login Tester"

	usuarios := httptesting.NewFakeUsuarioRepo()
	router := buildAuthE2ERouter(t, fb, usuarios)

	// ── 1. First login: 200 + caches under idemKey ─────────────────────────
	body1 := `{"id_token":"raw-token-A"}`
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, httptesting.NewE2ERequest(
		http.MethodPost, "/v2/auth/login", body1,
		httptesting.NoBearer(),
		httptesting.WithIdempotencyKey(idemKey),
	))
	require.Equal(t, http.StatusOK, rec1.Code, "first login must succeed; body=%s", rec1.Body.String())

	var resp1 authhttp.CurrentUserResponse
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	assert.Equal(t, fbUID, resp1.FirebaseUID)
	originalBytes := append([]byte(nil), rec1.Body.Bytes()...)

	// ── 2. Replay: same body + same key → cached 200, identical bytes ──────
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptesting.NewE2ERequest(
		http.MethodPost, "/v2/auth/login", body1,
		httptesting.NoBearer(),
		httptesting.WithIdempotencyKey(idemKey),
	))
	require.Equal(t, http.StatusOK, rec2.Code, "cached replay must respond 200; body=%s", rec2.Body.String())
	assert.Equal(t, "true", rec2.Header().Get("Idempotent-Replay"),
		"cache hit must set Idempotent-Replay: true")
	assert.Equal(t, originalBytes, rec2.Body.Bytes(),
		"replay must return identical bytes; mismatch implies a JSONB-style "+
			"renormalisation regression on idempotency_keys.response_body")

	// ── 3. Different body + same key → 422 idempotency_key_mismatch ───────
	// Per IETF Idempotency-Key draft §2.7, body-fingerprint mismatch is 422
	// (not 409). 409 is reserved for in-flight concurrent retries only.
	body3 := `{"id_token":"raw-token-DIFFERENT"}`
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, httptesting.NewE2ERequest(
		http.MethodPost, "/v2/auth/login", body3,
		httptesting.NoBearer(),
		httptesting.WithIdempotencyKey(idemKey),
	))
	assert.Equal(t, http.StatusUnprocessableEntity, rec3.Code,
		"key reuse with a different payload must be 422 idempotency_key_mismatch; body=%s",
		rec3.Body.String())
	assert.Contains(t, rec3.Body.String(), "idempotency_key_mismatch")
}

// TestE2E_Auth_MeReturnsCurrentUserViaAuthn drives GET /v2/me through the
// AuthnMiddleware. The middleware verifies the bearer token via Firebase,
// looks up the usuario, and plants CurrentUser on the request context. The
// Me handler then returns it. This is the regression guard for the
// "authn rejects planted CurrentUser" class of bug (commit 5ecc4db on the
// failed-intent chain).
func TestE2E_Auth_MeReturnsCurrentUserViaAuthn(t *testing.T) {
	t.Parallel()

	usuarioID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	const fbUID = "e2e-me-uid"
	fb := httptesting.NewFakeFirebase(fbUID)
	fb.Token.Email = "me@example.invalid"

	usuarios := httptesting.NewFakeUsuarioRepo()
	usuarios.AddUsuario(httptesting.AddUsuarioParams{
		ID:          usuarioID,
		FirebaseUID: fbUID,
		Email:       "me@example.invalid",
		Nombre:      "Me Tester",
		Activo:      true,
	})

	router := buildAuthE2ERouter(t, fb, usuarios)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptesting.NewE2ERequest(http.MethodGet, "/v2/me", ""))

	require.Equal(t, http.StatusOK, rec.Code, "/v2/me must respond 200 with valid bearer; body=%s", rec.Body.String())

	var resp authhttp.CurrentUserResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, usuarioID.String(), resp.ID)
	assert.Equal(t, fbUID, resp.FirebaseUID)
}

// TestE2E_Auth_RequirePermissionEnforced exercises PATCH /v2/usuarios/{id}
// under the RequirePermission(PermUsuariosActualizar) gate.
//
//   - Without the permission → 403 (RequirePermission rejects).
//   - With the permission → 200 (full chain passes: authn plants
//     CurrentUser, RequirePermission passes, idempotency middleware caches
//     the response, handler emits 200).
//   - Idem-key reuse with the same body → cached replay (idempotency
//     applies inside the protected chain).
func TestE2E_Auth_RequirePermissionEnforced(t *testing.T) {
	t.Parallel()

	usuarioID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	const fbUID = "e2e-rp-uid"
	fb := httptesting.NewFakeFirebase(fbUID)
	fb.Token.Email = "rp@example.invalid"

	usuarios := httptesting.NewFakeUsuarioRepo()
	usuarios.AddUsuario(httptesting.AddUsuarioParams{
		ID:          usuarioID,
		FirebaseUID: fbUID,
		Email:       "rp@example.invalid",
		Nombre:      "RP Tester",
		Activo:      true,
		// No permissions assigned yet.
	})

	router := buildAuthE2ERouter(t, fb, usuarios)

	body := `{"email":"rp@example.invalid","nombre":"RP Tester Updated"}`

	// ── 1. Without permission → 403 ───────────────────────────────────────
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, httptesting.NewE2ERequest(http.MethodPatch,
		"/v2/usuarios/"+usuarioID.String(), body,
		httptesting.WithIdempotencyKey("rp-idem-1")))
	assert.Equal(t, http.StatusForbidden, rec1.Code,
		"PATCH /v2/usuarios/{id} without PermUsuariosActualizar must be 403; body=%s",
		rec1.Body.String())

	// ── 2. Grant permission, retry with a fresh idem key → 200 ────────────
	usuarios.SetPermissions(usuarioID, []authdomain.Permission{authdomain.PermUsuariosActualizar})

	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptesting.NewE2ERequest(http.MethodPatch,
		"/v2/usuarios/"+usuarioID.String(), body,
		httptesting.WithIdempotencyKey("rp-idem-2")))
	require.Equal(t, http.StatusOK, rec2.Code,
		"PATCH /v2/usuarios/{id} with PermUsuariosActualizar must be 200; body=%s",
		rec2.Body.String())
	originalBytes := append([]byte(nil), rec2.Body.Bytes()...)

	// ── 3. Replay with same body + same key → cached, byte-equal ──────────
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, httptesting.NewE2ERequest(http.MethodPatch,
		"/v2/usuarios/"+usuarioID.String(), body,
		httptesting.WithIdempotencyKey("rp-idem-2")))
	require.Equal(t, http.StatusOK, rec3.Code,
		"idem-key replay in the protected chain must be 200; body=%s", rec3.Body.String())
	assert.Equal(t, "true", rec3.Header().Get("Idempotent-Replay"),
		"replay must be served from the idempotency cache")
	assert.Equal(t, originalBytes, rec3.Body.Bytes(),
		"replay must be byte-equal to the original response")
}

// ─── helpers ────────────────────────────────────────────────────────────────

// buildAuthE2ERouter assembles a chi router mounting authhttp.MountRouter
// under /v2, mirroring cmd/api/server.go::provideRootHandler. Only the
// auth surface is mounted — the goal is to isolate the /v2/auth chain.
func buildAuthE2ERouter(t *testing.T, fb outbound.FirebaseClient, usuarios outbound.UsuarioRepo) *chi.Mux {
	t.Helper()

	svc := authapp.NewService(
		usuarios,
		stubRolRepo{},
		stubPermisoRepo{},
		fixedClock{time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)},
		stubOutbox{},
		fb,
		nil, // no firebird tx manager — the in-memory fakes do not need one
	)

	idemStore := httptesting.NewInMemoryIdempotencyStore()

	r := chi.NewRouter()
	r.Route("/v2", func(r chi.Router) {
		authhttp.MountRouter(r, svc, fb, usuarios, idemStore)
	})
	return r
}

// ─── nop repos / outbox / clock ──────────────────────────────────────────────

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

type stubOutbox struct{}

func (stubOutbox) Enqueue(_ context.Context, _ string, _ uuid.UUID, _ string, _ any) error {
	return nil
}

type stubRolRepo struct{}

func (stubRolRepo) Save(context.Context, *authdomain.Rol) error   { return nil }
func (stubRolRepo) Update(context.Context, *authdomain.Rol) error { return nil }
func (stubRolRepo) FindByID(context.Context, uuid.UUID) (*authdomain.Rol, error) {
	return nil, authdomain.ErrRolNotFound
}

func (stubRolRepo) FindByNombre(context.Context, string) (*authdomain.Rol, error) {
	return nil, authdomain.ErrRolNotFound
}

func (stubRolRepo) List(context.Context, outbound.ListParams) (outbound.Page[*authdomain.Rol], error) {
	return outbound.Page[*authdomain.Rol]{}, nil
}
func (stubRolRepo) UpsertInmutableByName(context.Context, *authdomain.Rol) error { return nil }
func (stubRolRepo) AsignarPermiso(context.Context, uuid.UUID, authdomain.Permission, uuid.UUID, time.Time) error {
	return nil
}

func (stubRolRepo) RevocarPermiso(context.Context, uuid.UUID, authdomain.Permission) error {
	return nil
}

func (stubRolRepo) SyncPermisos(context.Context, uuid.UUID, []authdomain.Permission, uuid.UUID, time.Time) error {
	return nil
}

func (stubRolRepo) PermisosFor(context.Context, uuid.UUID) ([]authdomain.Permission, error) {
	return nil, nil
}

type stubPermisoRepo struct{}

func (stubPermisoRepo) UpsertCatalog(context.Context, []authdomain.PermissionMeta) error { return nil }

func (stubPermisoRepo) FindByCodigo(context.Context, authdomain.Permission) (*authdomain.Permiso, error) {
	return nil, authdomain.ErrPermisoNotFound
}
func (stubPermisoRepo) FindAll(context.Context) ([]*authdomain.Permiso, error) { return nil, nil }
func (stubPermisoRepo) FindOrphans(context.Context, []authdomain.Permission) ([]authdomain.Permission, error) {
	return nil, nil
}
