//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestReemplazarProductos_Happy(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	three, four := 3, 4
	out, err := h.svc.ReemplazarProductos(t.Context(), ventasapp.ReemplazarProductosInput{
		VentaID: *id,
		Productos: []ventasapp.CrearVentaProductoInput{{
			ID: uuid.New(), ArticuloID: 99, Articulo: "Refacción", Cantidad: decimal.NewFromInt(2),
			PrecioAnual: decimal.NewFromInt(100), PrecioCorto: decimal.NewFromInt(90), PrecioContado: decimal.NewFromInt(80),
			AlmacenOrigen: &three, AlmacenDestino: &four,
		}},
	}, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 1, out.ProductosCount())
}

func TestReemplazarProductos_RejectsCancelada(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	_, _ = h.svc.CancelarVenta(t.Context(), *id, "razon", uuid.New())
	one, two := 1, 2
	_, err := h.svc.ReemplazarProductos(t.Context(), ventasapp.ReemplazarProductosInput{
		VentaID: *id,
		Productos: []ventasapp.CrearVentaProductoInput{{
			ID: uuid.New(), ArticuloID: 1, Articulo: "X", Cantidad: decimal.NewFromInt(1),
			PrecioAnual: decimal.NewFromInt(1), PrecioCorto: decimal.NewFromInt(1), PrecioContado: decimal.NewFromInt(1),
			AlmacenOrigen: &one, AlmacenDestino: &two,
		}},
	}, uuid.New())
	require.ErrorIs(t, err, domain.ErrVentaNoEditable)
}
