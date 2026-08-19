package authhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// A valid Firebase user with no MSP_USUARIOS row yet is enrolled lazily on its
// first authenticated request (via the real SyncFromFirebase service), so the
// request proceeds with a planted CurrentUser instead of failing 404. This is
// the server-side root fix: clients never have to call POST /auth/login first.
func TestAuthnMiddleware_LazyProvisionsUnknownUser(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	rig.firebase.Token = &outbound.FirebaseToken{
		UID:   "firebase-uid-nuevo",
		Email: "nuevo.vendedor@msp.com",
		Name:  "Nuevo Vendedor",
	}

	mw := NewAuthnMiddleware(rig.firebase, rig.usuarios, rig.svc)

	var plantedEmail string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cu, ok := auth.CurrentUserFromContext(r.Context())
		assert.True(t, ok)
		plantedEmail = cu.Email
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/v2/zonas-cliente", nil)
	req.Header.Set("Authorization", "Bearer any-token")
	rec := httptest.NewRecorder()

	mw.Handler(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "nuevo.vendedor@msp.com", plantedEmail)
	_, err := rig.usuarios.FindByFirebaseUID(context.Background(), "firebase-uid-nuevo")
	assert.NoError(t, err, "el usuario debió quedar provisionado en el repo")
}

// With no provisioner wired (nil), the middleware keeps the original behavior:
// an unknown usuario surfaces usuario_not_found and the request is rejected.
func TestAuthnMiddleware_NilProvisioner_RejectsUnknownUser(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	rig.firebase.Token = &outbound.FirebaseToken{
		UID:   "firebase-uid-desconocido",
		Email: "desconocido@msp.com",
		Name:  "Desconocido",
	}

	mw := NewAuthnMiddleware(rig.firebase, rig.usuarios, nil)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/v2/zonas-cliente", nil)
	req.Header.Set("Authorization", "Bearer any-token")
	rec := httptest.NewRecorder()

	mw.Handler(next).ServeHTTP(rec, req)

	assert.False(t, called, "el handler no debió ejecutarse")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var problem map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	assert.Equal(t, "user_not_found", problem["code"])
}

// failingUsuarioRepo answers every lookup with an infrastructure failure. Only
// FindByFirebaseUID is exercised by the middleware; the embedded interface
// keeps the rest of the (large) port satisfied without dead code.
type failingUsuarioRepo struct {
	outbound.UsuarioRepo
	err error
}

func (f *failingUsuarioRepo) FindByFirebaseUID(_ context.Context, _ string) (*domain.Usuario, error) {
	return nil, f.err
}

// The Firebird pool being exhausted (or the statement timing out) must reach
// the client as 503, not as "usuario no encontrado". Answering 401 here is what
// made the 28h outage look like an auth problem to every client.
func TestAuthnMiddleware_InfraFailure_Returns503(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	rig.firebase.Token = &outbound.FirebaseToken{
		UID:   "firebase-uid-existente",
		Email: "existente@msp.com",
		Name:  "Usuario Existente",
	}
	repo := &failingUsuarioRepo{
		err: firebird.MapError(context.DeadlineExceeded),
	}

	mw := NewAuthnMiddleware(rig.firebase, repo, rig.svc)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/v2/zonas-cliente", nil)
	req.Header.Set("Authorization", "Bearer any-token")
	rec := httptest.NewRecorder()

	mw.Handler(next).ServeHTTP(rec, req)

	assert.False(t, called, "el handler no debió ejecutarse")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var problem map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	assert.Equal(t, "firebird_timeout", problem["code"])
}

// A non-apperror infra failure must not be laundered into 401 either: it keeps
// the generic 500 the response layer assigns to unclassified errors.
func TestAuthnMiddleware_UnclassifiedFailure_Returns500(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	rig.firebase.Token = &outbound.FirebaseToken{
		UID:   "firebase-uid-existente",
		Email: "existente@msp.com",
		Name:  "Usuario Existente",
	}
	repo := &failingUsuarioRepo{err: errors.New("driver: connection wedged")}

	mw := NewAuthnMiddleware(rig.firebase, repo, rig.svc)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/v2/zonas-cliente", nil)
	req.Header.Set("Authorization", "Bearer any-token")
	rec := httptest.NewRecorder()

	mw.Handler(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var problem map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	assert.Equal(t, "internal_error", problem["code"])
}

// An already-enrolled usuario proceeds without touching the provisioner.
func TestAuthnMiddleware_ExistingUser_Proceeds(t *testing.T) {
	t.Parallel()
	rig := newTestRig(t)
	u := rig.seedUsuario(t, "firebase-uid-existente", "existente@msp.com", "Usuario Existente")
	rig.firebase.Token = &outbound.FirebaseToken{
		UID:   u.FirebaseUID().Value(),
		Email: "existente@msp.com",
		Name:  "Usuario Existente",
	}

	// nil provisioner: a found user must never reach the provisioning branch.
	mw := NewAuthnMiddleware(rig.firebase, rig.usuarios, nil)

	var plantedEmail string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cu, ok := auth.CurrentUserFromContext(r.Context())
		assert.True(t, ok)
		plantedEmail = cu.Email
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/v2/zonas-cliente", nil)
	req.Header.Set("Authorization", "Bearer any-token")
	rec := httptest.NewRecorder()

	mw.Handler(next).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "existente@msp.com", plantedEmail)
}
