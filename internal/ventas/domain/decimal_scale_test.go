//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestNewMontoSnapshot_RejectsScaleOver2(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"100.999", "0.001", "1.123456"} {
		val := decimal.RequireFromString(s)
		_, err := domain.NewMontoSnapshot(val, decimal.Zero, decimal.Zero)
		require.Error(t, err, "input=%s", s)
		ae, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "monto_demasiados_decimales", ae.Code, "input=%s", s)
	}
	// Trigger error on second field.
	good := decimal.RequireFromString("0.00")
	bad := decimal.RequireFromString("0.001")
	_, err := domain.NewMontoSnapshot(good, bad, good)
	require.Error(t, err)
	_, err = domain.NewMontoSnapshot(good, good, bad)
	require.Error(t, err)
}

func TestNewMontoSnapshot_RejectsBeyondMaxMonto(t *testing.T) {
	t.Parallel()
	overflow := decimal.RequireFromString("1000000000000.00")
	_, err := domain.NewMontoSnapshot(overflow, decimal.Zero, decimal.Zero)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "monto_demasiado_grande", ae.Code)

	// At the boundary: exactly MaxMontoVenta must succeed.
	atCap := domain.MaxMontoVenta
	_, err = domain.NewMontoSnapshot(atCap, atCap, atCap)
	require.NoError(t, err)
}

func TestCrearVenta_RejectsCantidadScaleOver4(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Cantidad = decimal.RequireFromString("1.99999") // 5 decimals
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "cantidad_demasiados_decimales", ae.Code)
}

func TestCrearVenta_RejectsComboCantidadScaleOver4(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Combos = []domain.CrearVentaComboInput{{
		ID:             p.ID,
		Nombre:         "C",
		Precios:        domain.HydrateMontoSnapshot(decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50)),
		Cantidad:       decimal.RequireFromString("1.99999"),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
	}}
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "cantidad_demasiados_decimales", ae.Code)
}

func TestCrearVenta_RejectsProductoPreciosScaleOver2(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	// Inject a 3-decimal value via Hydrate (bypasses NewMontoSnapshot).
	p.Productos[0].Precios = domain.HydrateMontoSnapshot(
		decimal.RequireFromString("100.999"),
		decimal.RequireFromString("90.00"),
		decimal.RequireFromString("80.00"),
	)
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "monto_demasiados_decimales", ae.Code)
}

func TestNewPlanCredito_RejectsEngancheScaleOver2(t *testing.T) {
	t.Parallel()
	_, err := domain.NewPlanCredito(12,
		decimal.RequireFromString("100.999"),
		decimal.NewFromInt(50),
		domain.FrecPagoSemanal,
	)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "monto_demasiados_decimales", ae.Code)
}

func TestNewPlanCredito_RejectsEngancheBeyondCap(t *testing.T) {
	t.Parallel()
	overflow := decimal.RequireFromString("1000000000000.00")
	_, err := domain.NewPlanCredito(12, overflow, decimal.NewFromInt(50), domain.FrecPagoSemanal)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "monto_demasiado_grande", ae.Code)
}
