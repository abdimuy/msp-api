//nolint:misspell // domain vocabulary is Spanish (cancelacion, ventas) per project convention.
package app_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestCancelarVenta(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_marks_canceled_and_emits_event", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		by := uuid.New()

		venta, err := h.svc.CancelarVenta(t.Context(), *ventaID, "cliente rechazo entrega", by)
		require.NoError(t, err)
		require.True(t, venta.IsCanceled())
		require.NotNil(t, venta.Cancelacion())
		assert.Equal(t, "cliente rechazo entrega", venta.Cancelacion().Reason())
		assert.Equal(t, 1, h.ventas.UpdateCalls)
		assert.Equal(t, []string{domain.EventTypeVentaCancelada}, h.outbox.eventTypes())
	})

	t.Run("not_found_returns_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)

		_, err := h.svc.CancelarVenta(t.Context(), uuid.New(), "motivo", uuid.New())
		require.ErrorIs(t, err, domain.ErrVentaNotFound)
		assert.Zero(t, h.ventas.UpdateCalls)
		assert.Empty(t, h.outbox.snapshot())
	})

	t.Run("double_cancel_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		_, err := h.svc.CancelarVenta(t.Context(), *ventaID, "primera", uuid.New())
		require.NoError(t, err)
		// Reset outbox after first cancel so the second-call assertions are clean.
		h.outbox.mu.Lock()
		h.outbox.calls = nil
		h.outbox.mu.Unlock()

		_, err = h.svc.CancelarVenta(t.Context(), *ventaID, "segunda", uuid.New())
		require.ErrorIs(t, err, domain.ErrVentaYaCancelada)
		assert.Empty(t, h.outbox.snapshot())
	})

	t.Run("blank_reason_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)

		_, err := h.svc.CancelarVenta(t.Context(), *ventaID, "   ", uuid.New())
		require.ErrorIs(t, err, domain.ErrReasonCancelacionRequerida)
		assert.Zero(t, h.ventas.UpdateCalls)
	})

	t.Run("repo_update_failure_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		boom := errors.New("update failed")
		h.ventas.UpdateErr = boom

		_, err := h.svc.CancelarVenta(t.Context(), *ventaID, "motivo", uuid.New())
		require.ErrorIs(t, err, boom)
		assert.Empty(t, h.outbox.snapshot())
	})
}
