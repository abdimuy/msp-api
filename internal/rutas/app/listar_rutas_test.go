//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// fakeRutasRepo is a test double for outbound.RutasRepo.
type fakeRutasRepo struct {
	rows []rutasdomain.RutaResumen
	err  error
}

func (f *fakeRutasRepo) ListarRutas(_ context.Context) ([]rutasdomain.RutaResumen, error) {
	return f.rows, f.err
}

func intPtr(v int) *int { return &v }

func TestService_ListarRutas(t *testing.T) {
	t.Parallel()

	cobradorID := 5
	rows := []rutasdomain.RutaResumen{
		{
			ZonaID:         1,
			ZonaNombre:     "Norte",
			CobradorID:     &cobradorID,
			CobradorNombre: "Juan Pérez",
			NumClientes:    42,
			SaldoTotal:     decimal.NewFromFloat(15000.50),
		},
		{
			ZonaID:         2,
			ZonaNombre:     "Sur",
			CobradorID:     nil,
			CobradorNombre: "",
			NumClientes:    0,
			SaldoTotal:     decimal.Zero,
		},
	}

	svc := NewService(&fakeRutasRepo{rows: rows})
	got, err := svc.ListarRutas(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, 1, got[0].ZonaID)
	assert.Equal(t, "Norte", got[0].ZonaNombre)
	assert.Equal(t, intPtr(5), got[0].CobradorID)
	assert.Equal(t, "Juan Pérez", got[0].CobradorNombre)
	assert.Equal(t, 42, got[0].NumClientes)
	assert.True(t, decimal.NewFromFloat(15000.50).Equal(got[0].SaldoTotal))

	assert.Equal(t, 2, got[1].ZonaID)
	assert.Nil(t, got[1].CobradorID)
	assert.Empty(t, got[1].CobradorNombre)
	assert.Equal(t, 0, got[1].NumClientes)
	assert.True(t, decimal.Zero.Equal(got[1].SaldoTotal))
}
