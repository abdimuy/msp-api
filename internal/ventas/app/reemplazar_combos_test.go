//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
)

func TestReemplazarCombos_Happy(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	out, err := h.svc.ReemplazarCombos(t.Context(), ventasapp.ReemplazarCombosInput{
		VentaID: *id,
		Combos: []ventasapp.CrearVentaComboInput{{
			ID: uuid.New(), Nombre: "Combo Demo",
			PrecioAnual: decimal.NewFromInt(500), PrecioCorto: decimal.NewFromInt(450), PrecioContado: decimal.NewFromInt(400),
			Cantidad: decimal.NewFromInt(2), AlmacenOrigen: 1, AlmacenDestino: 2,
		}},
	}, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 1, out.CombosCount())
}
