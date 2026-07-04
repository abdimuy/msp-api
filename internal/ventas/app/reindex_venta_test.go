//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReindexVenta_NoIndexWired_NoOp(t *testing.T) {
	t.Parallel()
	h := newHarness(t) // searchIndex not wired
	err := h.svc.ReindexVenta(t.Context(), uuid.New())
	require.NoError(t, err)
}

func TestReindexVenta_ExistingVenta_IndexesMappedDoc(t *testing.T) {
	t.Parallel()
	h := newBuscarHarness(t)
	id := h.seedVenta(t)

	err := h.svc.ReindexVenta(t.Context(), *id)
	require.NoError(t, err)

	require.Len(t, h.index.IndexarUnoCalls, 1)
	assert.Equal(t, *id, h.index.IndexarUnoCalls[0].ID)
	assert.Empty(t, h.index.EliminarCalls)
}

func TestReindexVenta_NotFoundVenta_PurgesViaEliminar(t *testing.T) {
	t.Parallel()
	h := newBuscarHarness(t)
	missingID := uuid.New()

	err := h.svc.ReindexVenta(t.Context(), missingID)
	require.NoError(t, err)

	assert.Empty(t, h.index.IndexarUnoCalls)
	require.Len(t, h.index.EliminarCalls, 1)
	assert.Equal(t, missingID, h.index.EliminarCalls[0])
}

func TestReindexVenta_RepoErrorPropagates(t *testing.T) {
	t.Parallel()
	h := newBuscarHarness(t)
	boom := errors.New("firebird down")
	h.ventas.FindErr = boom

	err := h.svc.ReindexVenta(t.Context(), uuid.New())
	require.ErrorIs(t, err, boom)
}

func TestReindexVenta_IndexErrorPropagates(t *testing.T) {
	t.Parallel()
	h := newBuscarHarness(t)
	id := h.seedVenta(t)
	boom := errors.New("meili down")
	h.index.IndexarUnoErr = boom

	err := h.svc.ReindexVenta(t.Context(), *id)
	require.ErrorIs(t, err, boom)
}
