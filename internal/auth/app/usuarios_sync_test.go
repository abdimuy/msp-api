package app

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func tokenFor(uid, email, name string) *outbound.FirebaseToken {
	return &outbound.FirebaseToken{
		UID:       uid,
		Email:     email,
		Name:      name,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

// TestSyncFromFirebase covers token verification plus the create-or-update
// behavior of the login path.
func TestSyncFromFirebase(t *testing.T) {
	t.Parallel()

	t.Run("first_login_creates_usuario", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.firebase.Token = tokenFor("fuid-abc", "new@example.com", "Ana Garcia")

		u, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.NoError(t, err)
		assert.Equal(t, "fuid-abc", u.FirebaseUID().Value())
		assert.Equal(t, "new@example.com", u.Email().Value())
		assert.Equal(t, "Ana Garcia", u.Nombre().Value())
		assert.Equal(t, u.ID(), u.CreatedBy(), "self-created on first login")
		assert.Equal(t, 1, h.firebase.Verified)

		// Outbox event emitted with first_login=true.
		require.Len(t, h.outbox.Calls, 1)
		assert.Equal(t, eventUserSynced, h.outbox.Calls[0].EventType)
		payload, ok := h.outbox.Calls[0].Payload.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, payload["first_login"])
	})

	t.Run("derives_nombre_from_email_when_token_lacks_name", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.firebase.Token = tokenFor("fuid-xyz", "abc@example.com", "")

		u, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.NoError(t, err)
		assert.Equal(t, "abc", u.Nombre().Value())
	})

	t.Run("returning_login_returns_existing_usuario", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		existing := h.seedUsuario(t)
		h.firebase.Token = tokenFor(existing.FirebaseUID().Value(), existing.Email().Value(), existing.Nombre().Value())

		u, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.NoError(t, err)
		assert.Equal(t, existing.ID(), u.ID())
		require.Len(t, h.outbox.Calls, 1)
		payload, ok := h.outbox.Calls[0].Payload.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, false, payload["first_login"])
	})

	t.Run("inactive_usuario_is_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		existing := h.seedUsuario(t)
		existing.Desactivar(existing.ID(), h.clock.T)
		require.NoError(t, h.usuarios.Update(t.Context(), existing))
		h.firebase.Token = tokenFor(existing.FirebaseUID().Value(), existing.Email().Value(), existing.Nombre().Value())

		_, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.ErrorIs(t, err, domain.ErrUsuarioInactivo)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("token_verification_failure_propagates_typed_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		typed := apperror.NewUnauthorized("upstream", "upstream rechazó el token")
		h.firebase.Err = typed
		_, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.ErrorIs(t, err, typed)
	})

	t.Run("token_verification_failure_wraps_generic_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		raw := errors.New("network")
		h.firebase.Err = raw
		_, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.Error(t, err)
		require.ErrorIs(t, err, raw)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
	})

	t.Run("empty_email_in_token_is_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.firebase.Token = tokenFor("fuid-noemail", "", "Some Name")
		_, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.ErrorIs(t, err, domain.ErrEmailRequerido)
	})

	t.Run("repo_save_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.firebase.Token = tokenFor("fuid-fail", "fail@example.com", "Test")
		h.usuarios.SaveErr = errors.New("boom")
		_, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.Error(t, err)
	})

	t.Run("verifyidtoken_called_once", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.firebase.Token = tokenFor("fuid-once", "once@example.com", "Once")
		_, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.NoError(t, err)
		assert.Equal(t, 1, h.firebase.Verified)
	})
}

// TestSyncFromFirebase_PromotesVendedorOnly verifies that a VENDEDOR_ONLY row
// is promoted in place (same ID, new FUID) when the owner creates a Firebase
// account and logs in for the first time.
func TestSyncFromFirebase_PromotesVendedorOnly(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	// Seed a VENDEDOR_ONLY usuario via NewVendedorUsuario.
	vendEmail := "lucia.torres@muebleriamsp.mx"
	email, err := domain.NewEmail(vendEmail)
	require.NoError(t, err)
	nombre, err := domain.NewNombre("Lucía Torres")
	require.NoError(t, err)
	seededID := uuid.New()
	createdBy := uuid.New()
	vendedor := domain.NewVendedorUsuario(seededID, email, nombre, createdBy, h.clock.T)
	require.NoError(t, h.usuarios.Save(t.Context(), vendedor))

	// Firebase token arrives with a brand-new UID but the same email.
	h.firebase.Token = tokenFor("fb-new-uid", vendEmail, "Lucía Torres")

	u, syncErr := h.svc.SyncFromFirebase(t.Context(), "raw-token")
	require.NoError(t, syncErr)

	// Must reuse the same row — ID is preserved.
	assert.Equal(t, seededID, u.ID(), "promoted usuario must keep the original ID")

	// Firebase identity attached.
	assert.Equal(t, "fb-new-uid", u.FirebaseUID().Value())
	assert.Equal(t, domain.EstatusFirebaseUser, u.Estatus())

	// Audit bumped: UpdatedAt must be after CreatedAt.
	assert.True(t, u.UpdatedAt().After(u.CreatedAt()) || u.UpdatedAt().Equal(u.CreatedAt()),
		"UpdatedAt must be >= CreatedAt after promotion")

	// Outbox event emitted with first_login=true.
	require.Len(t, h.outbox.Calls, 1)
	assert.Equal(t, eventUserSynced, h.outbox.Calls[0].EventType)
	payload, ok := h.outbox.Calls[0].Payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, payload["first_login"])

	// Exactly one email row remains in the fake repo.
	found, findErr := h.usuarios.FindByEmail(t.Context(), vendEmail)
	require.NoError(t, findErr)
	assert.Equal(t, seededID, found.ID())
}

// TestSyncFromFirebase_InactiveVendedorOnly_Rejected verifies that an inactive
// VENDEDOR_ONLY user blocks the promotion and returns ErrUsuarioInactivo.
func TestSyncFromFirebase_InactiveVendedorOnly_Rejected(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)

	vendEmail := "pedro.soto@muebleriamsp.mx"
	email, err := domain.NewEmail(vendEmail)
	require.NoError(t, err)
	nombre, err := domain.NewNombre("Pedro Soto")
	require.NoError(t, err)
	seededID := uuid.New()
	vendedor := domain.NewVendedorUsuario(seededID, email, nombre, uuid.New(), h.clock.T)
	require.NoError(t, h.usuarios.Save(t.Context(), vendedor))

	// Deactivate the vendedor before the login attempt.
	vendedor.Desactivar(seededID, h.clock.T)
	require.NoError(t, h.usuarios.Update(t.Context(), vendedor))

	h.firebase.Token = tokenFor("fb-new-uid-2", vendEmail, "Pedro Soto")

	_, syncErr := h.svc.SyncFromFirebase(t.Context(), "raw-token")
	require.ErrorIs(t, syncErr, domain.ErrUsuarioInactivo)
	assert.Empty(t, h.outbox.Calls)
}
