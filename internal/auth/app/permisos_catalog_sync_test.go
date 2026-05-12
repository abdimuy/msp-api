package app

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// TestSyncPermissionCatalog covers the happy upsert, orphan warning, and
// repo-error propagation paths.
func TestSyncPermissionCatalog(t *testing.T) {
	t.Parallel()

	t.Run("upserts_full_catalog", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		require.NoError(t, h.svc.SyncPermissionCatalog(t.Context()))
		assert.Equal(t, 1, h.permisos.UpsertCalls)
		assert.Len(t, h.permisos.LastUpsert, len(domain.AllPermissions()))
		assert.Equal(t, []string{eventPermCatalogSynced}, h.outbox.EventTypes())
	})

	t.Run("orphans_logged_but_no_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.permisos.Orphans = []domain.Permission{"legacy:gone"}
		require.NoError(t, h.svc.SyncPermissionCatalog(t.Context()))
		assert.Equal(t, []string{eventPermCatalogSynced}, h.outbox.EventTypes())
	})

	t.Run("upsert_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.permisos.UpsertErr = errors.New("boom")
		require.Error(t, h.svc.SyncPermissionCatalog(t.Context()))
		assert.Empty(t, h.outbox.Calls)
	})

	t.Run("find_orphans_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, false)
		h.permisos.FindOrphansErr = errors.New("boom")
		require.Error(t, h.svc.SyncPermissionCatalog(t.Context()))
	})
}
