package authhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
)

// updateGolden is the -update flag for the golden-file pattern. Run
// `go test ./internal/auth/infra/authhttp -run TestGolden -update` to
// regenerate the snapshots after intentional changes.
var updateGolden = flag.Bool("update", false, "rewrite golden files from live responses")

// ─── golden helpers ─────────────────────────────────────────────────────────

// goldenAssert compares the live JSON response body to the snapshot stored at
// testdata/golden/<name>.json. With -update, the snapshot is rewritten from
// the live body instead of compared. Both sides are pretty-printed before
// disk I/O so committed snapshots are human-readable; the comparison itself
// uses assert.JSONEq so key order does not matter.
func goldenAssert(t *testing.T, name string, gotBody []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".json")

	// Pretty-print the live body before writing/comparing so diffs are
	// human-friendly. JSONEq ignores formatting so this only matters when
	// the snapshot is read by humans.
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, gotBody, "", "  "); err != nil {
		// Body is not JSON — store and compare verbatim. None of the cases
		// in this file exercise that path (we never call goldenAssert on a
		// 204 No Content body), so this is a defensive fallback only.
		pretty.Reset()
		_, _ = pretty.Write(gotBody)
	}

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, pretty.Bytes(), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s — rerun with -update to create it", path)
	}
	assert.JSONEq(t, string(want), string(gotBody), "golden mismatch for %s", name)
}

// scrubFields removes volatile keys from a JSON object body before snapshot
// comparison. Used for paths whose response intrinsically contains a value
// that cannot be pinned by a fixed clock or fixed UUID — currently:
//
//   - request_id: empty in tests today but kept in the helper so future
//     middleware additions do not break snapshots.
//   - updated_at: domain.audit.MarkUpdated calls time.Now() directly, so any
//     mutation handler produces a volatile UpdatedAt regardless of clock.
//   - id, firebase_uid: created by uuid.New() inside SyncFromFirebase's
//     first-login path; scrubbed for the login_success_first_login snapshot.
//
// Unknown keys in `keys` are silently ignored.
func scrubFields(body []byte, keys ...string) []byte {
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		return body
	}
	for _, k := range keys {
		delete(v, k)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return body
	}
	return out
}

// sortItemsByID re-serializes a list response body with its `items` array
// sorted by the `id` field. Required because the fake repos iterate over a
// Go map (unordered) when materializing List() results, which would make
// list snapshots flake. Items without an `id` field are left in place.
func sortItemsByID(body []byte) []byte {
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		return body
	}
	raw, ok := v["items"].([]any)
	if !ok {
		return body
	}
	sort.SliceStable(raw, func(i, j int) bool {
		ai, _ := raw[i].(map[string]any)
		aj, _ := raw[j].(map[string]any)
		si, _ := ai["id"].(string)
		sj, _ := aj["id"].(string)
		return si < sj
	})
	v["items"] = raw
	out, err := json.Marshal(v)
	if err != nil {
		return body
	}
	return out
}

// ─── deterministic builders ─────────────────────────────────────────────────

// fixedUUID returns a UUID whose last hex digit equals n (mod 16). Lets each
// test pin entity IDs without sharing a global counter.
func fixedUUID(n int) uuid.UUID {
	return uuid.MustParse("00000000-0000-0000-0000-00000000000" + string(rune('0'+(n%10))))
}

// goldenRig builds a fresh testRig with predictable seed data. Use this in
// every TestGolden_* — it keeps callers terse.
func goldenRig(t *testing.T) *testRig {
	t.Helper()
	return newTestRig(t)
}

// seedUsuarioWithID adds a usuario with a caller-supplied UUID so snapshots
// can lock the `id` field. Mirrors testRig.seedUsuario otherwise.
func seedUsuarioWithID(t *testing.T, rig *testRig, id uuid.UUID, fuid, email, nombre string) *domain.Usuario {
	t.Helper()
	fid, err := domain.NewFirebaseUID(fuid)
	require.NoError(t, err)
	em, err := domain.NewEmail(email)
	require.NoError(t, err)
	nm, err := domain.NewNombre(nombre)
	require.NoError(t, err)
	u := domain.NewUsuario(id, fid, em, nm, nil, nil, id, rig.clockTime)
	require.NoError(t, rig.usuarios.Save(context.Background(), u))
	return u
}

// seedRolWithID adds a rol with a caller-supplied UUID for snapshot stability.
func seedRolWithID(t *testing.T, rig *testRig, id uuid.UUID, nombre string, inmutable bool) *domain.Rol {
	t.Helper()
	rol, err := domain.NewRol(id, nombre, nil, inmutable, fixedUUID(9), rig.clockTime)
	require.NoError(t, err)
	require.NoError(t, rig.roles.Save(context.Background(), rol))
	return rol
}

// ─── Session ────────────────────────────────────────────────────────────────

func TestGolden_Login_SuccessFirstLogin(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	rig.firebase.Token = &outbound.FirebaseToken{
		UID:   "fbuid-new",
		Email: "new@example.com",
		Name:  "New User",
	}
	rec := doRequest(context.Background(), t, rig, http.MethodPost, "/auth/login", map[string]string{"id_token": "tok"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	// `id` and `firebase_uid` come from SyncFromFirebase. UID is supplied
	// (deterministic) but the persisted usuario gets a fresh uuid.New() id;
	// scrub that one volatile field.
	body := scrubFields(rec.Body.Bytes(), "id")
	goldenAssert(t, "login_success_first_login", body)
}

func TestGolden_Me_SuccessWithPermissions(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	u := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-1", "u1@example.com", "User One")

	// Mount the full router so authn runs end-to-end with planted firebase.
	rig.firebase.Token = &outbound.FirebaseToken{UID: u.FirebaseUID().Value(), Email: u.Email().Value()}
	// Plant a permission so the response Permisos list is non-empty and
	// stable.
	rig.usuarios.Permisos[u.ID()] = []domain.Permission{domain.PermPermisosListar}

	r := chi.NewRouter()
	MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	goldenAssert(t, "me_success_with_permissions", rec.Body.Bytes())
}

// ─── Usuarios ───────────────────────────────────────────────────────────────

func TestGolden_ListarUsuarios_Empty(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	caller := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-admin", "admin@example.com", "Admin")
	// Hard-delete the caller from the fake so the listing is genuinely
	// empty (the listing endpoint would otherwise show the admin itself).
	delete(rig.usuarios.ByID, caller.ID())
	delete(rig.usuarios.ByFUID, caller.FirebaseUID().Value())
	delete(rig.usuarios.ByEmail, caller.Email().Value())

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/usuarios/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	goldenAssert(t, "listar_usuarios_empty", rec.Body.Bytes())
}

func TestGolden_ListarUsuarios_WithData(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	caller := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-admin", "admin@example.com", "Admin")
	seedUsuarioWithID(t, rig, fixedUUID(2), "fbuid-2", "u2@example.com", "User Two")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/usuarios/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	// Map-iteration order is unstable; pre-sort items by id before snapshot.
	goldenAssert(t, "listar_usuarios_with_data", sortItemsByID(rec.Body.Bytes()))
}

func TestGolden_ObtenerUsuario_Success(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	caller := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-admin", "admin@example.com", "Admin")
	target := seedUsuarioWithID(t, rig, fixedUUID(2), "fbuid-t", "t@example.com", "Target User")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/usuarios/"+target.ID().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	goldenAssert(t, "obtener_usuario_success", rec.Body.Bytes())
}

func TestGolden_ObtenerUsuario_NotFound(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	caller := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-admin", "admin@example.com", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	// Use a fixed (non-existent) target ID so `instance` stays deterministic.
	target := fixedUUID(7)
	req := httptest.NewRequest(http.MethodGet, "/usuarios/"+target.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	goldenAssert(t, "obtener_usuario_not_found", rec.Body.Bytes())
}

func TestGolden_ActualizarUsuario_Success(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	caller := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-admin", "admin@example.com", "Admin")
	target := seedUsuarioWithID(t, rig, fixedUUID(2), "fbuid-t", "t@example.com", "Target User")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPatch, "/usuarios/"+target.ID().String(), ActualizarUsuarioRequest{
		Email:  "newemail@example.com",
		Nombre: "New Name",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	// MarkUpdated() inside the audit subrecord calls time.Now() directly,
	// so updated_at is intrinsically volatile on any mutation path. Scrub it.
	body := scrubFields(rec.Body.Bytes(), "updated_at")
	goldenAssert(t, "actualizar_usuario_success", body)
}

func TestGolden_ActualizarUsuario_ValidationError(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	caller := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-admin", "admin@example.com", "Admin")
	target := seedUsuarioWithID(t, rig, fixedUUID(2), "fbuid-t", "t@example.com", "Target User")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPatch, "/usuarios/"+target.ID().String(), ActualizarUsuarioRequest{
		Email:  "not-an-email",
		Nombre: "Some Name",
	})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	goldenAssert(t, "actualizar_usuario_validation_error", rec.Body.Bytes())
}

func TestGolden_DesactivarUsuario_Unauthorized(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	// Use the full router so the authn middleware runs. No Authorization
	// header → 401 "missing_authorization".
	r := chi.NewRouter()
	MountRouter(r, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())
	target := fixedUUID(2)
	req := httptest.NewRequest(http.MethodDelete, "/usuarios/"+target.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	goldenAssert(t, "desactivar_usuario_unauthorized", rec.Body.Bytes())
}

func TestGolden_DesactivarUsuario_Forbidden(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	caller := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-no", "no@example.com", "No Perms")
	// Caller has no permissions at all.
	cu := auth.CurrentUser{
		ID:          caller.ID(),
		FirebaseUID: caller.FirebaseUID().Value(),
		Email:       caller.Email().Value(),
		Nombre:      caller.Nombre().Value(),
	}
	r := mountWithCurrentUser(rig, cu)
	target := fixedUUID(2)
	req := httptest.NewRequest(http.MethodDelete, "/usuarios/"+target.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	goldenAssert(t, "desactivar_usuario_forbidden", rec.Body.Bytes())
}

// ─── Roles ──────────────────────────────────────────────────────────────────

func TestGolden_ListarRoles_WithData(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	caller := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-admin", "admin@example.com", "Admin")
	seedRolWithID(t, rig, fixedUUID(2), "supervisor", false)
	seedRolWithID(t, rig, fixedUUID(3), "vendedor", false)

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/roles/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	goldenAssert(t, "listar_roles_with_data", sortItemsByID(rec.Body.Bytes()))
}

func TestGolden_CrearRol_Success(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	caller := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-admin", "admin@example.com", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	desc := "rol de prueba"
	rec := doRequestRouter(t, r, http.MethodPost, "/roles/", CrearRolRequest{Nombre: "vendedor", Description: &desc})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	// CrearRol uses uuid.New() internally for the rol's id; scrub it so the
	// snapshot stays deterministic.
	body := scrubFields(rec.Body.Bytes(), "id")
	goldenAssert(t, "crear_rol_success", body)
}

func TestGolden_ActualizarRol_InmutableError(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	caller := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-admin", "admin@example.com", "Admin")
	rol := seedRolWithID(t, rig, fixedUUID(2), "super_admin", true)

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	rec := doRequestRouter(t, r, http.MethodPatch, "/roles/"+rol.ID().String(), ActualizarRolRequest{Nombre: "new_name"})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	goldenAssert(t, "actualizar_rol_inmutable_error", rec.Body.Bytes())
}

func TestGolden_ObtenerRol_NotFound(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	caller := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-admin", "admin@example.com", "Admin")

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	target := fixedUUID(7)
	req := httptest.NewRequest(http.MethodGet, "/roles/"+target.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	goldenAssert(t, "obtener_rol_not_found", rec.Body.Bytes())
}

// ─── Permisos ───────────────────────────────────────────────────────────────

func TestGolden_ListarPermisos(t *testing.T) {
	t.Parallel()
	rig := goldenRig(t)
	caller := seedUsuarioWithID(t, rig, fixedUUID(1), "fbuid-admin", "admin@example.com", "Admin")
	rig.seedPermiso(t, domain.PermUsuariosListar)
	rig.seedPermiso(t, domain.PermRolesListar)

	r := mountWithCurrentUser(rig, adminCurrentUser(caller))
	req := httptest.NewRequest(http.MethodGet, "/permisos", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	// FindAll iterates over a map; sort items for determinism.
	var v map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v))
	items, _ := v["items"].([]any)
	sort.SliceStable(items, func(i, j int) bool {
		ai, _ := items[i].(map[string]any)
		aj, _ := items[j].(map[string]any)
		si, _ := ai["codigo"].(string)
		sj, _ := aj["codigo"].(string)
		return si < sj
	})
	v["items"] = items
	out, err := json.Marshal(v)
	require.NoError(t, err)
	goldenAssert(t, "listar_permisos", out)
}
