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
		ID:        id,
		Nombre:    "Combo basico",
		Precios:   montos,
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: creator,
		UpdatedBy: creator,
	})

	assert.Equal(t, id, c.ID())
	assert.Equal(t, "Combo basico", c.Nombre())
	assert.True(t, c.Precios().Equals(montos))
	auditRec := c.Audit()
	assert.Equal(t, now, auditRec.CreatedAt())
	assert.Equal(t, creator, auditRec.CreatedBy())
}

func TestCombo_ViaCrearVenta_Validation(t *testing.T) {
	t.Parallel()
	// Combos are only constructed via CrearVenta — exercise the indirect path.
	mk := func(nombre string) error {
		params := validCrearVentaParams(t)
		params.Combos = []domain.CrearVentaComboInput{{
			ID:      uuid.New(),
			Nombre:  nombre,
			Precios: params.Montos,
		}}
		_, err := domain.CrearVenta(params)
		return err
	}
	// Valid combo.
	require.NoError(t, mk("Combo X"))
	// Empty nombre.
	err := mk("   ")
	require.Error(t, err)
	// Too long nombre.
	err = mk(strings.Repeat("a", 201))
	require.Error(t, err)
}
