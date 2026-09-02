package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/infra/authhttp"
	authoutbound "github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

// This test covers the OTHER half of the capture split, and it is here rather
// than in internal/platform/failedintent for one reason: the platform tests
// pin what StatusesOutsideAuth CONTAINS, not that the composition root uses
// it. Reverting captureAroundAuth to {401} left both packages green.
//
// It also cannot use the `planted` shortcut the 401 test uses: a planted
// CurrentUser makes authn.Handler skip the lookup entirely, which is exactly
// the step that produces the 403. The usuario has to come back from a repo,
// inactive.

// firebaseStub verifies any token and always returns the same uid. The token
// is not what this test is about — the usuario behind it is.
type firebaseStub struct {
	authoutbound.FirebaseClient // unimplemented methods panic if ever called
	uid                         string
}

func (f firebaseStub) VerifyIDToken(_ context.Context, _ string) (*authoutbound.FirebaseToken, error) {
	return &authoutbound.FirebaseToken{
		UID:       f.uid,
		Email:     "cobrador.baja@muebleriamsp.mx",
		Name:      "Cobrador Dado De Baja",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

// usuariosStub returns one usuario with activo=false — a cobrador dado de
// baja whose Firebase session is still perfectly valid.
type usuariosStub struct {
	authoutbound.UsuarioRepo // unimplemented methods panic if ever called
	usuario                  *authdomain.Usuario
}

func (u usuariosStub) FindByFirebaseUID(_ context.Context, _ string) (*authdomain.Usuario, error) {
	return u.usuario, nil
}

// usuarioInactivo builds the domain object the stub hands back.
func usuarioInactivo(t *testing.T, uid string) *authdomain.Usuario {
	t.Helper()
	fbUID, err := authdomain.NewFirebaseUID(uid)
	require.NoError(t, err)
	email, err := authdomain.NewEmail("cobrador.baja@muebleriamsp.mx")
	require.NoError(t, err)
	nombre, err := authdomain.NewNombre("Cobrador Dado De Baja")
	require.NoError(t, err)

	now := time.Now().UTC()
	return authdomain.HydrateUsuario(authdomain.HydrateUsuarioParams{
		ID:          uuid.New(),
		FirebaseUID: fbUID,
		Email:       email,
		Nombre:      nombre,
		// Activo=false is the whole point: a normal FIREBASE_USER whose row
		// was deactivated. Estatus is the creation discriminator, not the
		// on/off switch.
		Activo:    false,
		Estatus:   authdomain.EstatusFirebaseUser,
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: uuid.New(),
		UpdatedBy: uuid.New(),
	})
}

// assembleCobranzaChainConAuthnReal mirrors the cobranza mount exactly like
// assembleCobranzaChain, but wires a real authn middleware over stubs that
// resolve to an INACTIVE usuario, so the 403 is produced by production code.
func assembleCobranzaChainConAuthnReal(
	txCtx context.Context, t *testing.T, deps captureChainDeps, downstream http.HandlerFunc,
) http.Handler {
	t.Helper()
	const uid = "firebase-uid-cobrador-dado-de-baja"

	configs := provideFailedIntentCapturas(deps.store, deps.blobs, nil, &config.Config{})
	capture := captureAroundAuth(configs.Cobranza)
	authn := authhttp.NewAuthnMiddleware(
		firebaseStub{uid: uid},
		usuariosStub{usuario: usuarioInactivo(t, uid)},
		nil,
	)

	r := chi.NewRouter()
	r.Route("/v2", func(r chi.Router) {
		r.Route("/cobranza", func(r chi.Router) {
			r.Use(txInjectorFor(txCtx, nil), capture.OutsideAuth, authn.Handler, capture.InsideAuth)
			r.Post("/pagos", downstream)
		})
	})
	return r
}

// TestCapture_PagoDeCobradorDadoDeBaja_LeavesEvidence: the session is valid,
// the person is not. authn answers 403 user_inactive before anything further
// in the chain sees the request, so without StatusesOutsideAuth covering 403
// the pago — and the receipt photo that proves the money was collected —
// leaves no trace anywhere on the server.
func TestCapture_PagoDeCobradorDadoDeBaja_LeavesEvidence(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	committedBefore := countCommittedRows(t, pool.DB)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		deps := newCaptureChainDeps(t, pool)
		reached := false
		chain := assembleCobranzaChainConAuthnReal(ctx, t, deps, func(w http.ResponseWriter, _ *http.Request) {
			reached = true
			w.WriteHeader(http.StatusOK)
		})

		body, contentType := buildPagoMultipart(t)
		before := capturedIDs(ctx, t, pool)

		req := httptest.NewRequest(http.MethodPost, pagoCapturePath, bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Authorization", "Bearer token-valido-de-un-usuario-inactivo")
		rw := httptest.NewRecorder()
		chain.ServeHTTP(rw, req)

		require.Equal(t, http.StatusForbidden, rw.Code, "body: %s", rw.Body.String())
		require.False(t, reached, "authn must have cut the chain before the handler")

		fresh := newlyCaptured(before, capturedIDs(ctx, t, pool))
		require.Len(t, fresh, 1,
			"the rejected pago must leave exactly one row in MSP_FAILED_INTENTS")

		row := readCapturedRow(ctx, t, pool, fresh[0])
		assert.Equal(t, http.StatusForbidden, row.httpStatus)
		assert.Equal(t, "user_inactive", row.errorCode)
		assert.False(t, row.usuarioID.Valid,
			"the request never got past authn, so no usuario may be named")
		assert.True(t, row.blobPath.Valid, "the receipt photo is the evidence")
		assert.Equal(t, http.MethodPost, row.method)
		assert.Equal(t, pagoCapturePath, row.path)
	})

	assert.Equal(t, committedBefore, countCommittedRows(t, pool.DB),
		"nothing may survive the rollback")
}
