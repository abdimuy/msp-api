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

func TestHydrateCombo(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	creator := uuid.New()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	montos, err := domain.NewMontoSnapshot(decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50))
	require.NoError(t, err)

	c := domain.HydrateCombo(domain.HydrateComboParams{
		ID:             id,
		Nombre:         "Combo basico",
		Precios:        montos,
		Cantidad:       decimal.NewFromInt(2),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		CreatedAt:      now,
		UpdatedAt:      now,
		CreatedBy:      creator,
		UpdatedBy:      creator,
	})

	assert.Equal(t, id, c.ID())
	assert.Equal(t, "Combo basico", c.Nombre())
	assert.True(t, c.Precios().Equals(montos))
	assert.True(t, c.Cantidad().Equal(decimal.NewFromInt(2)))
	assert.Equal(t, 1, c.AlmacenOrigen())
	assert.Equal(t, 2, c.AlmacenDestino())
	auditRec := c.Audit()
	assert.Equal(t, now, auditRec.CreatedAt())
	assert.Equal(t, creator, auditRec.CreatedBy())
}

func TestCombo_ViaCrearVenta_Validation(t *testing.T) {
	t.Parallel()
	// Combos are only constructed via CrearVenta — exercise the indirect path.
	mk := func(mut func(*domain.CrearVentaComboInput)) error {
		params := validCrearVentaParams(t)
		input := domain.CrearVentaComboInput{
			ID: uuid.New(), Nombre: "Combo X", Precios: params.Montos,
			Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2,
		}
		mut(&input)
		params.Combos = []domain.CrearVentaComboInput{input}
		_, err := domain.CrearVenta(params)
		return err
	}
	// Valid combo.
	require.NoError(t, mk(func(c *domain.CrearVentaComboInput) {}))
	// Empty nombre.
	require.Error(t, mk(func(c *domain.CrearVentaComboInput) { c.Nombre = "   " }))
	// Too long nombre.
	require.Error(t, mk(func(c *domain.CrearVentaComboInput) { c.Nombre = strings.Repeat("a", 201) }))
	// Cantidad zero.
	require.Error(t, mk(func(c *domain.CrearVentaComboInput) { c.Cantidad = decimal.Zero }))
	// Almacen origen missing.
	require.Error(t, mk(func(c *domain.CrearVentaComboInput) { c.AlmacenOrigen = 0 }))
	// Almacen destino missing.
	require.Error(t, mk(func(c *domain.CrearVentaComboInput) { c.AlmacenDestino = 0 }))
	// Almacenes iguales.
	require.Error(t, mk(func(c *domain.CrearVentaComboInput) { c.AlmacenOrigen = 5; c.AlmacenDestino = 5 }))
}
