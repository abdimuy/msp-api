//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestObtenerVenta(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_returns_aggregate", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)

		got, err := h.svc.ObtenerVenta(t.Context(), *ventaID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, *ventaID, got.ID())
	})

	t.Run("not_found_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)

		_, err := h.svc.ObtenerVenta(t.Context(), uuid.New())
		require.ErrorIs(t, err, domain.ErrVentaNotFound)
	})

	t.Run("repo_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		boom := errors.New("repo down")
		h.ventas.FindErr = boom

		_, err := h.svc.ObtenerVenta(t.Context(), uuid.New())
		require.ErrorIs(t, err, boom)
	})
}
