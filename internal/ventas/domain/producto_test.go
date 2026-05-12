package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestHydrateProducto(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	creator := uuid.New()
	comboID := uuid.New()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	montos, err := domain.NewMontoSnapshot(decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50))
	require.NoError(t, err)

	p := domain.HydrateProducto(domain.HydrateProductoParams{
		ID:         id,
		ArticuloID: 999,
		Articulo:   "Articulo",
		Cantidad:   decimal.NewFromInt(2),
		Precios:    montos,
		ComboID:    &comboID,
		CreatedAt:  now,
		UpdatedAt:  now,
		CreatedBy:  creator,
		UpdatedBy:  creator,
	})

	assert.Equal(t, id, p.ID())
	assert.Equal(t, 999, p.ArticuloID())
	assert.Equal(t, "Articulo", p.Articulo())
	assert.True(t, p.Cantidad().Equal(decimal.NewFromInt(2)))
	require.NotNil(t, p.ComboID())
	assert.Equal(t, comboID, *p.ComboID())
	assert.True(t, p.Precios().Equals(montos))
	a := p.Audit()
	assert.Equal(t, creator, a.CreatedBy())
}

func TestProducto_ViaCrearVenta_Validation(t *testing.T) {
	t.Parallel()
	mk := func(p domain.CrearVentaProductoInput) error {
		params := validCrearVentaParams(t)
		params.Productos = []domain.CrearVentaProductoInput{p}
		_, err := domain.CrearVenta(params)
		return err
	}
	montos, _ := domain.NewMontoSnapshot(decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50))

	// Valid producto.
	require.NoError(t, mk(domain.CrearVentaProductoInput{
		ID: uuid.New(), ArticuloID: 1, Articulo: "X", Cantidad: decimal.NewFromInt(1), Precios: montos,
	}))
	// Empty articulo.
	require.Error(t, mk(domain.CrearVentaProductoInput{
		ID: uuid.New(), ArticuloID: 1, Articulo: "   ", Cantidad: decimal.NewFromInt(1), Precios: montos,
	}))
	// Articulo too long.
	require.Error(t, mk(domain.CrearVentaProductoInput{
		ID: uuid.New(), ArticuloID: 1, Articulo: strings.Repeat("a", 201),
		Cantidad: decimal.NewFromInt(1), Precios: montos,
	}))
	// Cantidad zero.
	require.Error(t, mk(domain.CrearVentaProductoInput{
		ID: uuid.New(), ArticuloID: 1, Articulo: "X", Cantidad: decimal.Zero, Precios: montos,
	}))
	// Cantidad negative.
	require.Error(t, mk(domain.CrearVentaProductoInput{
		ID: uuid.New(), ArticuloID: 1, Articulo: "X", Cantidad: decimal.NewFromInt(-1), Precios: montos,
	}))
}
