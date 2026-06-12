package authhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
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
	assert.Equal(t, http.StatusNotFound, rec.Code)
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
