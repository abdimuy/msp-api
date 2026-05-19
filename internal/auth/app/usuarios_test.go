package app

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
)

// TestActualizar covers the happy path, validation failures, and missing
// usuario branch of Service.Actualizar.
func TestActualizar(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_updates_fields_and_emits_event", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		by := uuid.New()

		updated, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:     u.ID(),
			Email:  "nuevo@example.com",
			Nombre: "Pedro Lopez",
		}, by)
		require.NoError(t, err)
		assert.Equal(t, "nuevo@example.com", updated.Email().Value())
		assert.Equal(t, "Pedro Lopez", updated.Nombre().Value())
		assert.Equal(t, by, updated.UpdatedBy())
		assert.Equal(t, []string{eventUserUpdated}, h.outbox.EventTypes())
	})

	t.Run("with_optional_telefono", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		tel := "+15512345678"
		updated, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:       u.ID(),
			Email:    u.Email().Value(),
			Nombre:   "Maria",
			Telefono: &tel,
		}, uuid.New())
		require.NoError(t, err)
		require.NotNil(t, updated.Telefono())
		assert.Equal(t, "+15512345678", updated.Telefono().Value())
	})

	t.Run("blank_telefono_clears_field", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		empty := "  "
		updated, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:       u.ID(),
			Email:    u.Email().Value(),
			Nombre:   "Maria",
			Telefono: &empty,
		}, uuid.New())
		require.NoError(t, err)
		assert.Nil(t, updated.Telefono())
	})

	t.Run("invalid_telefono_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		bad := "abc"
		_, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:       u.ID(),
			Email:    u.Email().Value(),
			Nombre:   "Maria",
			Telefono: &bad,
		}, uuid.New())
		require.Error(t, err)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("usuario_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		_, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:     uuid.New(),
			Email:  "x@example.com",
			Nombre: "X",
		}, uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioNotFound)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("invalid_email_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		_, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:     u.ID(),
			Email:  "not-an-email",
			Nombre: "Pedro",
		}, uuid.New())
		require.ErrorIs(t, err, domain.ErrEmailInvalido)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("invalid_nombre_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		_, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:     u.ID(),
			Email:  u.Email().Value(),
			Nombre: "   ",
		}, uuid.New())
		require.ErrorIs(t, err, domain.ErrNombreRequerido)
	})

	t.Run("repo_update_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		boom := errors.New("boom")
		h.usuarios.UpdateErr = boom
		_, err := h.svc.Actualizar(t.Context(), ActualizarParams{
			ID:     u.ID(),
			Email:  u.Email().Value(),
			Nombre: "Pedro Lopez",
		}, uuid.New())
		require.ErrorIs(t, err, boom)
		assert.Empty(t, h.outbox.Calls)
	})
}

// TestDesactivar covers the happy soft-delete path plus the not-found branch.
func TestDesactivar(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_mangles_email_and_emits_event", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		oldEmail := u.Email().Value()
		originalFUID := u.FirebaseUID().Value()
		by := uuid.New()

		require.NoError(t, h.svc.Desactivar(t.Context(), u.ID(), by))
		assert.False(t, u.Activo())
		assert.Contains(t, u.Email().Value(), "deleted-")
		assert.Contains(t, u.Email().Value(), oldEmail)
		assert.Contains(t, u.FirebaseUID().Value(), "deleted-")
		assert.Equal(t, []string{eventUserDeactivated}, h.outbox.EventTypes())

		// The event payload must carry the ORIGINAL firebase_uid (captured
		// before the rename) so the downstream handler can find the
		// account on Firebase.
		require.Len(t, h.outbox.Calls, 1)
		payload, ok := h.outbox.Calls[0].Payload.(map[string]any)
		require.True(t, ok, "payload must be a map")
		assert.Equal(t, originalFUID, payload["firebase_uid"])
		assert.NotContains(t, payload["firebase_uid"], "deleted-",
			"event must carry the pre-rename uid, not the placeholder")
		assert.Equal(t, u.ID(), payload["usuario_id"])
		assert.Equal(t, by, payload["deactivated_by"])
	})

	t.Run("usuario_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		err := h.svc.Desactivar(t.Context(), uuid.New(), uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioNotFound)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("repo_update_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		h.usuarios.UpdateErr = errors.New("boom")
		require.Error(t, h.svc.Desactivar(t.Context(), u.ID(), uuid.New()))
		assert.Empty(t, h.outbox.Calls)
	})
}

// TestObtenerListar exercises the trivial read-through methods.
func TestObtenerListar(t *testing.T) {
	t.Parallel()

	t.Run("obtener_returns_usuario", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		got, err := h.svc.Obtener(t.Context(), u.ID())
		require.NoError(t, err)
		assert.Equal(t, u.ID(), got.ID())
	})

	t.Run("obtener_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		_, err := h.svc.Obtener(t.Context(), uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioNotFound)
	})

	t.Run("listar_returns_page", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.seedUsuario(t)
		h.seedUsuario(t)
		page, err := h.svc.Listar(t.Context(), outbound.ListParams{PageSize: 10})
		require.NoError(t, err)
		assert.Len(t, page.Items, 2)
	})
}

// TestAsignarRevocarRol covers role assignment lifecycle.
func TestAsignarRevocarRol(t *testing.T) {
	t.Parallel()

	t.Run("asignar_happy_path", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		r := h.seedRol(t, "vendedor")
		by := uuid.New()

		require.NoError(t, h.svc.AsignarRolAUsuario(t.Context(), u.ID(), r.ID(), by))
		_, ok := h.usuarios.RoleLinks[u.ID()][r.ID()]
		assert.True(t, ok)
		assert.Equal(t, []string{eventRoleAssigned}, h.outbox.EventTypes())
	})

	t.Run("asignar_usuario_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		r := h.seedRol(t, "vendedor")
		err := h.svc.AsignarRolAUsuario(t.Context(), uuid.New(), r.ID(), uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioNotFound)
	})

	t.Run("asignar_rol_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		err := h.svc.AsignarRolAUsuario(t.Context(), u.ID(), uuid.New(), uuid.New())
		require.ErrorIs(t, err, domain.ErrRolNotFound)
	})

	t.Run("revocar_happy_path", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		u := h.seedUsuario(t)
		r := h.seedRol(t, "vendedor")
		require.NoError(t, h.svc.AsignarRolAUsuario(t.Context(), u.ID(), r.ID(), uuid.New()))
		h.outbox.Calls = nil

		require.NoError(t, h.svc.RevocarRolDeUsuario(t.Context(), u.ID(), r.ID()))
		_, ok := h.usuarios.RoleLinks[u.ID()][r.ID()]
		assert.False(t, ok)
		assert.Equal(t, []string{eventRoleRevoked}, h.outbox.EventTypes())
	})

	t.Run("revocar_usuario_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		err := h.svc.RevocarRolDeUsuario(t.Context(), uuid.New(), uuid.New())
		require.ErrorIs(t, err, domain.ErrUsuarioNotFound)
	})
}
