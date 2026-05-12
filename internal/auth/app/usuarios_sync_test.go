package app

import (
	"errors"
	"testing"
	"time"

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
