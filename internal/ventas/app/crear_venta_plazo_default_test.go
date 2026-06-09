//nolint:misspell // ventas vocabulary is Spanish (plazo, credito) per project convention.
package app_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestCrearVenta_PlazoMesesDefault documents the field-app contract: the
// Android app does not set plazo_meses (the office assigns the credit term),
// so a CREDITO venta arriving with plazo_meses = 0 must NOT fail — the API
// fills in the default term and the office overrides it later if needed.
//
// Before this behavior, such ventas were rejected with "el plazo en meses
// debe ser mayor a cero" and landed as failed intents the office had to edit
// and replay by hand.
func TestCrearVenta_PlazoMesesDefault(t *testing.T) {
	t.Parallel()

	t.Run("zero_plazo_defaults_to_12", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validCreditoInput()
		in.PlanCredito.PlazoMeses = 0 // la app no lo manda

		venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.NoError(t, err)
		require.NotNil(t, venta.PlanCredito())
		assert.Equal(t, domain.DefaultPlazoMeses, venta.PlanCredito().PlazoMeses())
		assert.Equal(t, 12, venta.PlanCredito().PlazoMeses())
	})

	t.Run("negative_plazo_also_defaults", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validCreditoInput()
		in.PlanCredito.PlazoMeses = -1

		venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.NoError(t, err)
		require.NotNil(t, venta.PlanCredito())
		assert.Equal(t, domain.DefaultPlazoMeses, venta.PlanCredito().PlazoMeses())
	})

	t.Run("explicit_plazo_is_preserved", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validCreditoInput()
		in.PlanCredito.PlazoMeses = 18

		venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.NoError(t, err)
		require.NotNil(t, venta.PlanCredito())
		assert.Equal(t, 18, venta.PlanCredito().PlazoMeses())
	})
}
