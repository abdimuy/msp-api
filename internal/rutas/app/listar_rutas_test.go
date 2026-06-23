//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"testing"
	"time"

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

// fakeCobranzaRepo is a test double for outbound.CobranzaRepo.
type fakeCobranzaRepo struct {
	rows map[int][]rutasdomain.VentaCobranza
	err  error
}

func (f *fakeCobranzaRepo) VentasPorZona(_ context.Context, zonaID int, _, _ time.Time) ([]rutasdomain.VentaCobranza, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows[zonaID], nil
}

// fakeCalendario is a test double for outbound.CalendarioCobradorClient.
type fakeCalendario struct {
	m   map[int]time.Time
	err error
}

func (f *fakeCalendario) FechaInicioPorCobrador(_ context.Context) (map[int]time.Time, error) {
	return f.m, f.err
}

func intPtr(v int) *int { return &v }

func TestService_ListarRutas_NoMetrics(t *testing.T) {
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

	// Empty calendar → no metrics.
	svc := NewService(
		&fakeRutasRepo{rows: rows},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{}},
		&fakeCalendario{m: map[int]time.Time{}},
	)
	got, err := svc.ListarRutas(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, 1, got[0].ZonaID)
	assert.Equal(t, "Norte", got[0].ZonaNombre)
	assert.Equal(t, intPtr(5), got[0].CobradorID)
	assert.Nil(t, got[0].PctCoberturaSemanal, "no calendar → nil metrics")
	assert.Nil(t, got[0].PctPonderadoSemanal)

	assert.Nil(t, got[1].CobradorID)
	assert.Nil(t, got[1].PctCoberturaSemanal)
}

func TestService_ListarRutas_WithMetrics(t *testing.T) {
	t.Parallel()

	cobradorID := 5
	zonaID := 1
	rows := []rutasdomain.RutaResumen{
		{
			ZonaID:     zonaID,
			CobradorID: &cobradorID,
			ZonaNombre: "Norte",
			SaldoTotal: decimal.NewFromInt(1000),
		},
	}

	fechaInicio := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)

	ventas := []rutasdomain.VentaCobranza{
		{
			VentaID:      1,
			ClienteID:    100,
			ZonaID:       zonaID,
			Parcialidad:  decimal.NewFromInt(100),
			Frecuencia:   rutasdomain.Semanal,
			AbonoSemana:  decimal.NewFromInt(100),
			Vencidas:     decimal.NewFromInt(0),
			Aporte:       decimal.NewFromInt(1),
			Saldo:        decimal.NewFromInt(900),
			TotalImporte: decimal.NewFromInt(4000),
			FechaCargo:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			VentaID:      2,
			ClienteID:    101,
			ZonaID:       zonaID,
			Parcialidad:  decimal.NewFromInt(200),
			Frecuencia:   rutasdomain.Semanal,
			AbonoSemana:  decimal.Zero, // no pagó
			Vencidas:     decimal.NewFromInt(1),
			Aporte:       decimal.Zero,
			Saldo:        decimal.NewFromInt(2000),
			TotalImporte: decimal.NewFromInt(4000),
			FechaCargo:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	svc := NewService(
		&fakeRutasRepo{rows: rows},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{zonaID: ventas}},
		&fakeCalendario{m: map[int]time.Time{cobradorID: fechaInicio}},
	)
	got, err := svc.ListarRutas(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 1)

	require.NotNil(t, got[0].PctCoberturaSemanal, "coverage should be computed")
	// 1 of 2 paid → 50%
	assert.True(t,
		decimal.NewFromFloat(50.0).Equal(*got[0].PctCoberturaSemanal),
		"cobertura %s", got[0].PctCoberturaSemanal,
	)

	require.NotNil(t, got[0].PctPonderadoSemanal)
	// aporte sum=1, den=2 → 50%
	assert.True(t,
		decimal.NewFromFloat(50.0).Equal(*got[0].PctPonderadoSemanal),
		"ponderado %s", got[0].PctPonderadoSemanal,
	)
}
