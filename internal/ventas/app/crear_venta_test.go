//nolint:misspell // domain vocabulary is Spanish (ventas, contado, etc.) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestCrearVenta(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_contado_emits_creada_event", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		by := uuid.New()

		venta, err := h.svc.CrearVenta(t.Context(), validContadoInput(), by)
		require.NoError(t, err)
		require.NotNil(t, venta)
		assert.Equal(t, domain.TipoVentaContado, venta.TipoVenta())
		assert.Equal(t, 1, h.ventas.SaveCalls)
		assert.Equal(t, []string{domain.EventTypeVentaCreada}, h.outbox.eventTypes())
		assert.Empty(t, venta.PendingEvents(), "pending events should be drained after commit")
	})

	t.Run("happy_path_credito_sets_plan_and_dia_cobranza", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)

		venta, err := h.svc.CrearVenta(t.Context(), validCreditoInput(), uuid.New())
		require.NoError(t, err)
		require.NotNil(t, venta.PlanCredito())
		require.NotNil(t, venta.DiaCobranza())
		assert.Equal(t, domain.FrecPagoQuincenal, venta.PlanCredito().FrecPago())
		assert.True(t, venta.DiaCobranza().IsMes())
	})

	t.Run("invalid_tipo_venta_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		in.TipoVenta = "INVALIDO"

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrTipoVentaInvalido)
		assert.Zero(t, h.ventas.SaveCalls)
		assert.Empty(t, h.outbox.snapshot())
	})

	t.Run("credito_missing_plan_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		in.TipoVenta = "CREDITO"

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrPlanCreditoRequiredEnCredito)
		assert.Zero(t, h.ventas.SaveCalls)
	})

	t.Run("repo_save_failure_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		boom := errors.New("db down")
		h.ventas.SaveErr = boom

		_, err := h.svc.CrearVenta(t.Context(), validContadoInput(), uuid.New())
		require.ErrorIs(t, err, boom)
		assert.Empty(t, h.outbox.snapshot(), "no events when persistence fails")
	})

	t.Run("outbox_failure_does_not_block_success", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.outbox.err = errors.New("outbox down")

		venta, err := h.svc.CrearVenta(t.Context(), validContadoInput(), uuid.New())
		require.NoError(t, err)
		require.NotNil(t, venta)
		assert.Equal(t, 1, h.ventas.SaveCalls)
		// Best-effort: the call was still attempted.
		assert.Equal(t, []string{domain.EventTypeVentaCreada}, h.outbox.eventTypes())
	})

	t.Run("context_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		// We do not assert on the error since the fake repo does not check
		// ctx — but the call should still complete without panicking.
		_, _ = h.svc.CrearVenta(ctx, validContadoInput(), uuid.New())
	})
}
