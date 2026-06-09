package app

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// fakeNombreResolver returns a canned NOMBRE keyed by uid.
type fakeNombreResolver struct {
	nombres map[string]string
	err     error
	calls   int
}

func (f *fakeNombreResolver) ResolveNombre(_ context.Context, uid string) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return f.nombres[uid], nil
}

// TestSyncFromFirebase_NombreResolver covers the root-cause fix: first-login
// user creation prefers the canonical NOMBRE from Firestore over the token's
// name claim (which is frequently empty or the email).
func TestSyncFromFirebase_NombreResolver(t *testing.T) {
	t.Parallel()

	t.Run("firestore_nombre_wins_over_token_name", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.firebase.Token = tokenFor("fuid-1", "abdimuy@gmail.com", "abdimuy@gmail.com")
		resolver := &fakeNombreResolver{nombres: map[string]string{"fuid-1": "Aldrich Cortero"}}
		h.svc.WithNombreResolver(resolver)

		u, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.NoError(t, err)
		assert.Equal(t, "Aldrich Cortero", u.Nombre().Value(),
			"the Firestore NOMBRE must win over the email-as-name token claim")
		assert.Equal(t, 1, resolver.calls)
	})

	t.Run("falls_back_to_token_name_when_firestore_empty", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.firebase.Token = tokenFor("fuid-2", "x@example.com", "Token Name")
		resolver := &fakeNombreResolver{nombres: map[string]string{}} // no entry
		h.svc.WithNombreResolver(resolver)

		u, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.NoError(t, err)
		assert.Equal(t, "Token Name", u.Nombre().Value())
	})

	t.Run("falls_back_to_email_when_firestore_empty_and_no_token_name", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.firebase.Token = tokenFor("fuid-3", "israel.soto@msp.com", "")
		resolver := &fakeNombreResolver{nombres: map[string]string{}}
		h.svc.WithNombreResolver(resolver)

		u, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.NoError(t, err)
		assert.Equal(t, "israel.soto", u.Nombre().Value())
	})

	t.Run("resolver_error_degrades_to_token_name", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.firebase.Token = tokenFor("fuid-4", "y@example.com", "Token Name")
		resolver := &fakeNombreResolver{err: errors.New("firestore down")}
		h.svc.WithNombreResolver(resolver)

		u, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.NoError(t, err)
		assert.Equal(t, "Token Name", u.Nombre().Value(),
			"a Firestore failure must never block login; fall back to the token name")
	})

	t.Run("nil_resolver_keeps_legacy_behavior", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.firebase.Token = tokenFor("fuid-5", "z@example.com", "Token Name")
		// No WithNombreResolver — resolver stays nil.

		u, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.NoError(t, err)
		assert.Equal(t, "Token Name", u.Nombre().Value())
	})

	t.Run("promote_vendedor_only_adopts_firestore_nombre", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)

		// Seed a VENDEDOR_ONLY row with an email-derived placeholder name.
		vendEmail := "israel.soto@msp.com"
		email, err := domain.NewEmail(vendEmail)
		require.NoError(t, err)
		nombre, err := domain.NewNombre("israel.soto")
		require.NoError(t, err)
		vend := domain.NewVendedorUsuario(uuid.New(), email, nombre, uuid.New(), h.clock.T)
		require.NoError(t, h.usuarios.Save(t.Context(), vend))

		h.firebase.Token = tokenFor("fb-uid-israel", vendEmail, "")
		resolver := &fakeNombreResolver{nombres: map[string]string{"fb-uid-israel": "Israel Soto Carpintero"}}
		h.svc.WithNombreResolver(resolver)

		u, err := h.svc.SyncFromFirebase(t.Context(), "raw-token")
		require.NoError(t, err)
		assert.Equal(t, "Israel Soto Carpintero", u.Nombre().Value(),
			"promoting a vendedor-only row on first login must adopt the canonical Firestore name")
		assert.Equal(t, "fb-uid-israel", u.FirebaseUID().Value())
	})
}
