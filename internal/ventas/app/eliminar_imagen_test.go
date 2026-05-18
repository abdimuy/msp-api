//nolint:misspell // domain vocabulary is Spanish (imagen, ventas) per project convention.
package app_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// seedVentaWithImagen creates a venta and attaches one imagen so deletion
// tests have something to work with. Returns ventaID and imagenID.
func seedVentaWithImagen(t *testing.T, h *testHarness) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ventaID := h.seedVenta(t)
	in := adjuntarInput(*ventaID)
	img, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
	require.NoError(t, err)
	h.outbox.mu.Lock()
	h.outbox.calls = nil
	h.outbox.mu.Unlock()
	h.storage.mu.Lock()
	h.storage.DeleteCalls = 0
	h.storage.DeletedKeys = nil
	h.storage.mu.Unlock()
	return *ventaID, img.ID()
}

func TestEliminarImagen(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_removes_row_and_blob", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID, imagenID := seedVentaWithImagen(t, h)

		err := h.svc.EliminarImagen(t.Context(), ventaID, imagenID, uuid.New())
		require.NoError(t, err)
		assert.Equal(t, 1, h.ventas.DeleteImagenCalls)
		assert.Equal(t, 1, h.storage.DeleteCalls)
		assert.Equal(t, []string{domain.EventTypeImagenEliminada}, h.outbox.eventTypes())
	})

	t.Run("venta_not_found_returns_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)

		err := h.svc.EliminarImagen(t.Context(), uuid.New(), uuid.New(), uuid.New())
		require.ErrorIs(t, err, domain.ErrVentaNotFound)
		assert.Zero(t, h.ventas.DeleteImagenCalls)
		assert.Zero(t, h.storage.DeleteCalls)
	})

	t.Run("imagen_not_found_returns_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)

		err := h.svc.EliminarImagen(t.Context(), *ventaID, uuid.New(), uuid.New())
		require.ErrorIs(t, err, domain.ErrImagenNotFound)
		assert.Zero(t, h.ventas.DeleteImagenCalls)
		assert.Zero(t, h.storage.DeleteCalls)
	})

	t.Run("repo_delete_failure_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID, imagenID := seedVentaWithImagen(t, h)
		boom := errors.New("delete failed")
		h.ventas.DeleteImagenErr = boom

		err := h.svc.EliminarImagen(t.Context(), ventaID, imagenID, uuid.New())
		require.ErrorIs(t, err, boom)
		assert.Zero(t, h.storage.DeleteCalls, "blob is not removed when the row delete failed")
		assert.Empty(t, h.outbox.snapshot())
	})

	t.Run("storage_delete_failure_is_logged_not_returned", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID, imagenID := seedVentaWithImagen(t, h)
		h.storage.DeleteErr = errors.New("storage flaky")

		err := h.svc.EliminarImagen(t.Context(), ventaID, imagenID, uuid.New())
		require.NoError(t, err, "DB is source of truth; storage failure must not bubble up")
		assert.Equal(t, 1, h.ventas.DeleteImagenCalls)
		assert.Equal(t, 1, h.storage.DeleteCalls)
		assert.Equal(t, []string{domain.EventTypeImagenEliminada}, h.outbox.eventTypes())
	})

	t.Run("canceled_venta_rejects_deletion", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID, imagenID := seedVentaWithImagen(t, h)
		_, err := h.svc.CancelarVenta(t.Context(), ventaID, "motivo", uuid.New())
		require.NoError(t, err)
		h.outbox.mu.Lock()
		h.outbox.calls = nil
		h.outbox.mu.Unlock()

		err = h.svc.EliminarImagen(t.Context(), ventaID, imagenID, uuid.New())
		require.ErrorIs(t, err, domain.ErrVentaCanceladaInmutable)
		assert.Zero(t, h.ventas.DeleteImagenCalls)
	})
}

// TestEliminarImagen_StorageDeleteFailure_LogsStorageKey lives at the top
// level (not inside the parallel TestEliminarImagen subtest matrix) because
// it mutates slog.Default and must run sequentially.
//
// Killed mutation: eliminar_imagen.go:35 `if err := s.storage.Delete; err
// != nil` — the existing storage_delete_failure_is_logged_not_returned only
// asserts the call returns nil, which lets a CONDITIONALS_NEGATION mutation
// (== nil instead of != nil) survive: the storage error path becomes a
// silent no-op AND the test still passes. Pinning the log emission closes
// that gap.
//
//nolint:paralleltest,tparallel // mutates global slog.Default; sequential by necessity.
func TestEliminarImagen_StorageDeleteFailure_LogsStorageKey(t *testing.T) {
	logs := captureLogs(t)
	h := newHarness(t)
	ventaID, imagenID := seedVentaWithImagen(t, h)
	h.storage.DeleteErr = errors.New("storage flaky")

	err := h.svc.EliminarImagen(t.Context(), ventaID, imagenID, uuid.New())
	require.NoError(t, err, "DB is source of truth; storage failure must not bubble up")

	out := logs()
	assert.Contains(t, out, "ventas.storage_delete_failed",
		"storage delete failure must produce a structured log entry")
	assert.Contains(t, out, ventaID.String(),
		"log entry must carry venta_id for traceability")
	assert.Contains(t, out, imagenID.String(),
		"log entry must carry imagen_id for traceability")
}
