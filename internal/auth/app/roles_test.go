package app

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
)

// TestCrearRol covers happy and validation-failure cases of CrearRol.
func TestCrearRol(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_persists_and_emits_event", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		by := uuid.New()
		desc := "rol para vendedores"

		rol, err := h.svc.CrearRol(t.Context(), CrearRolParams{Nombre: "vendedor", Description: &desc}, by)
		require.NoError(t, err)
		assert.Equal(t, "vendedor", rol.Nombre())
		assert.False(t, rol.Inmutable())
		assert.True(t, rol.Activo())
		_, ok := h.roles.ByID[rol.ID()]
		assert.True(t, ok)
		assert.Equal(t, []string{eventRoleCreated}, h.outbox.EventTypes())
	})

	t.Run("invalid_nombre_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		_, err := h.svc.CrearRol(t.Context(), CrearRolParams{Nombre: ""}, uuid.New())
		require.ErrorIs(t, err, domain.ErrRolNombreRequerido)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("nombre_collision_returns_yaexiste", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.seedRol(t, "vendedor")
		_, err := h.svc.CrearRol(t.Context(), CrearRolParams{Nombre: "vendedor"}, uuid.New())
		require.ErrorIs(t, err, domain.ErrRolYaExiste)
	})
}

// TestActualizarRol covers Update on mutable and inmutable roles plus the
// not-found branch.
func TestActualizarRol(t *testing.T) {
	t.Parallel()

	t.Run("happy_path", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		r := h.seedRol(t, "vendedor")
		newDesc := "actualizado"

		updated, err := h.svc.ActualizarRol(t.Context(), ActualizarRolParams{
			ID: r.ID(), Nombre: "vendedor_senior", Description: &newDesc,
		}, uuid.New())
		require.NoError(t, err)
		assert.Equal(t, "vendedor_senior", updated.Nombre())
		require.NotNil(t, updated.Description())
		assert.Equal(t, "actualizado", *updated.Description())
		assert.Equal(t, []string{eventRoleUpdated}, h.outbox.EventTypes())
	})

	t.Run("inmutable_rol_refuses_update", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		r := h.seedInmutableRol(t, "super_admin")
		_, err := h.svc.ActualizarRol(t.Context(), ActualizarRolParams{
			ID: r.ID(), Nombre: "hacked",
		}, uuid.New())
		require.ErrorIs(t, err, domain.ErrRolInmutable)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("rol_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		_, err := h.svc.ActualizarRol(t.Context(), ActualizarRolParams{
			ID: uuid.New(), Nombre: "x",
		}, uuid.New())
		require.ErrorIs(t, err, domain.ErrRolNotFound)
	})
}

// TestDesactivarRol mirrors ActualizarRol for the deactivation path.
func TestDesactivarRol(t *testing.T) {
	t.Parallel()

	t.Run("happy_path", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		r := h.seedRol(t, "temp")
		require.NoError(t, h.svc.DesactivarRol(t.Context(), r.ID(), uuid.New()))
		assert.False(t, r.Activo())
		assert.Equal(t, []string{eventRoleDeactivated}, h.outbox.EventTypes())
	})

	t.Run("inmutable_refuses", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		r := h.seedInmutableRol(t, "super_admin")
		require.ErrorIs(t, h.svc.DesactivarRol(t.Context(), r.ID(), uuid.New()), domain.ErrRolInmutable)
	})

	t.Run("rol_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		require.ErrorIs(t, h.svc.DesactivarRol(t.Context(), uuid.New(), uuid.New()), domain.ErrRolNotFound)
	})
}

// TestObtenerListarRoles exercises the read-through methods.
func TestObtenerListarRoles(t *testing.T) {
	t.Parallel()

	t.Run("obtener_returns_rol", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		r := h.seedRol(t, "vendedor")
		got, err := h.svc.ObtenerRol(t.Context(), r.ID())
		require.NoError(t, err)
		assert.Equal(t, r.ID(), got.ID())
	})

	t.Run("obtener_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		_, err := h.svc.ObtenerRol(t.Context(), uuid.New())
		require.ErrorIs(t, err, domain.ErrRolNotFound)
	})

	t.Run("listar_returns_page", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.seedRol(t, "r1")
		h.seedRol(t, "r2")
		page, err := h.svc.ListarRoles(t.Context(), outbound.ListParams{PageSize: 10})
		require.NoError(t, err)
		assert.Len(t, page.Items, 2)
	})
}

// TestListarPermisos verifies the read-through to PermisoRepo.FindAll.
func TestListarPermisos(t *testing.T) {
	t.Parallel()

	h := newHarness(t, true)
	got, err := h.svc.ListarPermisos(t.Context())
	require.NoError(t, err)
	assert.Len(t, got, len(domain.AllPermissions()))
}
