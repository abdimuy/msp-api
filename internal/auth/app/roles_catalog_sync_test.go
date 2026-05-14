package app

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// TestSyncRolesCatalog exercises the boot-time rol bootstrap: skip when no
// usuario, upsert + permission sync when usuarios exist, and inmutable
// rol creation with full permission set.
func TestSyncRolesCatalog(t *testing.T) {
	t.Parallel()

	t.Run("skips_when_no_usuario_exists", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		require.NoError(t, h.svc.SyncRolesCatalog(t.Context(), uuid.New()))
		assert.Equal(t, 0, h.roles.UpsertCalls)
		assert.Equal(t, 0, h.roles.SyncCalls)
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("creates_super_admin_with_full_permission_set", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, true)
		h.seedUsuario(t)
		bootUser := uuid.New()

		require.NoError(t, h.svc.SyncRolesCatalog(t.Context(), bootUser))
		assert.Equal(t, 1, h.roles.UpsertCalls)
		assert.Equal(t, 1, h.roles.SyncCalls)

		rol, err := h.roles.FindByNombre(t.Context(), superAdminNombre)
		require.NoError(t, err)
		assert.True(t, rol.Inmutable())

		perms, err := h.roles.PermisosFor(t.Context(), rol.ID())
		require.NoError(t, err)
		assert.Len(t, perms, len(domain.AllPermissions()))
		assert.Equal(t, []string{eventRolesCatalogSynced}, h.outbox.EventTypes())
	})

	t.Run("idempotent_second_call", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, true)
		h.seedUsuario(t)
		bootUser := uuid.New()

		require.NoError(t, h.svc.SyncRolesCatalog(t.Context(), bootUser))
		require.NoError(t, h.svc.SyncRolesCatalog(t.Context(), bootUser))
		assert.Equal(t, 2, h.roles.UpsertCalls)
		assert.Equal(t, 2, h.roles.SyncCalls)

		// Only one rol persisted.
		_, err := h.roles.FindByNombre(t.Context(), superAdminNombre)
		require.NoError(t, err)
	})

	t.Run("usuario_list_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, true)
		h.usuarios.ListErr = errors.New("boom")
		require.Error(t, h.svc.SyncRolesCatalog(t.Context(), uuid.New()))
	})

	t.Run("upsert_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, true)
		h.seedUsuario(t)
		h.roles.UpsertErr = errors.New("boom")
		require.Error(t, h.svc.SyncRolesCatalog(t.Context(), uuid.New()))
	})

	t.Run("sync_permisos_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, true)
		h.seedUsuario(t)
		h.roles.SyncErr = errors.New("boom")
		require.Error(t, h.svc.SyncRolesCatalog(t.Context(), uuid.New()))
	})

	// Boot-time caller (cmd/api/auth_wiring.go) passes uuid.Nil because
	// it has no concrete usuario id in hand at startup. The routine must
	// auto-derive a valid id from the first usuario in the system rather
	// than persisting CREATED_BY=00000000-... and tripping the FK on
	// MSP_ROLES_PERMISOS.CREATED_BY → MSP_USUARIOS.ID. Regression guard.
	t.Run("derives_bootuserid_from_first_usuario_when_nil", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, true)
		h.seedUsuario(t)

		require.NoError(t, h.svc.SyncRolesCatalog(t.Context(), uuid.Nil))
		assert.Equal(t, 1, h.roles.UpsertCalls)
		assert.Equal(t, 1, h.roles.SyncCalls)

		rol, err := h.roles.FindByNombre(t.Context(), superAdminNombre)
		require.NoError(t, err)
		assert.True(t, rol.Inmutable())
	})
}
