package app

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// TestAsignarPermisoARol covers happy + inmutable refusal + missing-rol.
func TestAsignarPermisoARol(t *testing.T) {
	t.Parallel()

	t.Run("happy_path", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, true)
		r := h.seedRol(t, "vendedor")
		by := uuid.New()

		require.NoError(t, h.svc.AsignarPermisoARol(t.Context(), r.ID(), domain.PermUsuariosListar, by))
		_, ok := h.roles.PermsByRol[r.ID()][domain.PermUsuariosListar]
		assert.True(t, ok)
		assert.Equal(t, []string{eventRolePermGranted}, h.outbox.EventTypes())
	})

	t.Run("inmutable_rol_refuses", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, true)
		r := h.seedInmutableRol(t, "super_admin")
		require.ErrorIs(
			t,
			h.svc.AsignarPermisoARol(t.Context(), r.ID(), domain.PermUsuariosListar, uuid.New()),
			domain.ErrRolInmutable,
		)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("rol_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, true)
		require.ErrorIs(
			t,
			h.svc.AsignarPermisoARol(t.Context(), uuid.New(), domain.PermUsuariosListar, uuid.New()),
			domain.ErrRolNotFound,
		)
	})

	t.Run("permiso_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false) // no permisos seeded
		r := h.seedRol(t, "vendedor")
		require.ErrorIs(
			t,
			h.svc.AsignarPermisoARol(t.Context(), r.ID(), domain.Permission("inexistente"), uuid.New()),
			domain.ErrPermisoNotFound,
		)
	})
}

// TestRevocarPermisoDeRol mirrors the assignment path.
func TestRevocarPermisoDeRol(t *testing.T) {
	t.Parallel()

	t.Run("happy_path", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, true)
		r := h.seedRol(t, "vendedor")
		require.NoError(t, h.svc.AsignarPermisoARol(t.Context(), r.ID(), domain.PermUsuariosListar, uuid.New()))
		h.outbox.Calls = nil

		require.NoError(t, h.svc.RevocarPermisoDeRol(t.Context(), r.ID(), domain.PermUsuariosListar))
		_, ok := h.roles.PermsByRol[r.ID()][domain.PermUsuariosListar]
		assert.False(t, ok)
		assert.Equal(t, []string{eventRolePermRevoked}, h.outbox.EventTypes())
	})

	t.Run("inmutable_refuses", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, true)
		r := h.seedInmutableRol(t, "super_admin")
		require.ErrorIs(
			t,
			h.svc.RevocarPermisoDeRol(t.Context(), r.ID(), domain.PermUsuariosListar),
			domain.ErrRolInmutable,
		)
	})

	t.Run("rol_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, true)
		require.ErrorIs(
			t,
			h.svc.RevocarPermisoDeRol(t.Context(), uuid.New(), domain.PermUsuariosListar),
			domain.ErrRolNotFound,
		)
	})
}
