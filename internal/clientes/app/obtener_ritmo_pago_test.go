//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// TestObtenerRitmoPago_ClienteNoExiste_NotFound verifies that a missing client
// propagates the not-found apperror from the repo.
func TestObtenerRitmoPago_ClienteNoExiste_NotFound(t *testing.T) {
	t.Parallel()

	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{},
	}
	svc := clientesapp.NewService(repo, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	_, err := svc.ObtenerRitmoPago(context.Background(), 9999, outbound.RangoFechas{})
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected apperror.Error")
	assert.Equal(t, apperror.KindNotFound, appErr.Kind)
}

// TestObtenerRitmoPago_HappyPath_RetornaRitmo verifies that with a known client
// and preset raw data the service returns a populated RitmoPago.
func TestObtenerRitmoPago_HappyPath_RetornaRitmo(t *testing.T) {
	t.Parallel()

	pago := domain.PagoCrudo{
		Fecha:   time.Date(2026, 1, 13, 0, 0, 0, 0, time.UTC), // Monday
		Importe: decimal.NewFromInt(1000),
	}
	venta := domain.VentaCruda{
		Fecha:      time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		Total:      decimal.NewFromInt(5000),
		DoctoPvID:  100,
		Folio:      "PV-0100",
		EsCredito:  true,
		PlazoMeses: 6,
	}

	base := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{42: newCliente(42, "Test Cliente")},
	}
	repo := &ritmoPagoFakeRepo{
		fakeClientesRepo: base,
		data: outbound.RitmoPagoData{
			Pagos:       []domain.PagoCrudo{pago},
			Ventas:      []domain.VentaCruda{venta},
			SaldoActual: decimal.NewFromInt(4000),
		},
	}

	svc := clientesapp.NewService(repo, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	ritmo, err := svc.ObtenerRitmoPago(context.Background(), 42, outbound.RangoFechas{})
	require.NoError(t, err)

	assert.NotEmpty(t, ritmo.Semanas)
	assert.NotEmpty(t, ritmo.Eventos)
	assert.True(t, ritmo.Resumen.TotalAbonado.Equal(decimal.NewFromInt(1000)))
	assert.True(t, ritmo.Resumen.SaldoActual.Equal(decimal.NewFromInt(4000)))
}

// TestObtenerRitmoPago_RangoMapeadoCorrectamente verifies that the rango is
// forwarded unchanged to ObtenerRitmoPagoData.
func TestObtenerRitmoPago_RangoMapeadoCorrectamente(t *testing.T) {
	t.Parallel()

	base := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{42: newCliente(42, "Test")},
	}
	captured := &capturingRitmoPagoRepo{fakeClientesRepo: base}
	svc := clientesapp.NewService(captured, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	desde := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	hasta := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	rango := outbound.RangoFechas{Desde: &desde, Hasta: &hasta}

	_, err := svc.ObtenerRitmoPago(context.Background(), 42, rango)
	require.NoError(t, err)
	assert.Equal(t, rango.Desde, captured.capturedRango.Desde)
	assert.Equal(t, rango.Hasta, captured.capturedRango.Hasta)
}

// ─── Test-local repo wrappers ─────────────────────────────────────────────────

// ritmoPagoFakeRepo embeds fakeClientesRepo and overrides ObtenerRitmoPagoData
// to return preset data.
type ritmoPagoFakeRepo struct {
	*fakeClientesRepo
	data    outbound.RitmoPagoData
	dataErr error
}

func (f *ritmoPagoFakeRepo) ObtenerRitmoPagoData(_ context.Context, _ int, _ outbound.RangoFechas) (outbound.RitmoPagoData, error) {
	if f.dataErr != nil {
		return outbound.RitmoPagoData{}, f.dataErr
	}
	return f.data, nil
}

// capturingRitmoPagoRepo embeds fakeClientesRepo and records the rango passed
// to ObtenerRitmoPagoData for assertion.
type capturingRitmoPagoRepo struct {
	*fakeClientesRepo
	capturedRango outbound.RangoFechas
}

func (f *capturingRitmoPagoRepo) ObtenerRitmoPagoData(_ context.Context, _ int, rango outbound.RangoFechas) (outbound.RitmoPagoData, error) {
	f.capturedRango = rango
	return outbound.RitmoPagoData{}, nil
}
