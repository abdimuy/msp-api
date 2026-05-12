//nolint:misspell // domain vocabulary is Spanish (imagen, ventas) per project convention.
package app_test

import (
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestObtenerImagen(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_returns_metadata_and_blob", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		in := adjuntarInput(*ventaID)
		img, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
		require.NoError(t, err)

		result, err := h.svc.ObtenerImagen(t.Context(), *ventaID, img.ID())
		require.NoError(t, err)
		require.NotNil(t, result.Imagen)
		assert.Equal(t, img.ID(), result.Imagen.ID())
		require.NotNil(t, result.Object.Body)
		defer func() { _ = result.Object.Body.Close() }()
		body, err := io.ReadAll(result.Object.Body)
		require.NoError(t, err)
		assert.Equal(t, "fake jpeg bytes", string(body))
	})

	t.Run("venta_not_found", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		_, err := h.svc.ObtenerImagen(t.Context(), uuid.New(), uuid.New())
		require.ErrorIs(t, err, domain.ErrVentaNotFound)
	})

	t.Run("imagen_not_found_in_venta", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)

		_, err := h.svc.ObtenerImagen(t.Context(), *ventaID, uuid.New())
		require.ErrorIs(t, err, domain.ErrImagenNotFound)
	})

	t.Run("storage_get_failure_returns_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		in := adjuntarInput(*ventaID)
		img, err := h.svc.AdjuntarImagen(t.Context(), in, uuid.New())
		require.NoError(t, err)

		boom := errors.New("disk read failed")
		h.storage.GetErr = boom
		_, err = h.svc.ObtenerImagen(t.Context(), *ventaID, img.ID())
		require.ErrorIs(t, err, boom)
	})

	t.Run("cancelled_venta_still_readable", func(t *testing.T) {
		t.Parallel()
		// Cancellation freezes the aggregate but does not hide its history;
		// supervisors must still review evidence on cancelled ventas.
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		img, err := h.svc.AdjuntarImagen(t.Context(), adjuntarInput(*ventaID), uuid.New())
		require.NoError(t, err)
		_, err = h.svc.CancelarVenta(t.Context(), *ventaID, "motivo", uuid.New())
		require.NoError(t, err)

		result, err := h.svc.ObtenerImagen(t.Context(), *ventaID, img.ID())
		require.NoError(t, err)
		require.NotNil(t, result.Imagen)
		_ = result.Object.Body.Close()
	})
}
