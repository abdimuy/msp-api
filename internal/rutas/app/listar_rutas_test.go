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

	// Raw values as the repo returns them — Aporte and Vencidas are zero
	// (unset). enrichVentas computes them from Parcialidad, Saldo,
	// TotalImporte, AbonoSemana and fechaInicio.
	ventas := []rutasdomain.VentaCobranza{
		{
			VentaID:      1,
			ClienteID:    100,
			ZonaID:       zonaID,
			Parcialidad:  decimal.NewFromInt(100),
			Frecuencia:   rutasdomain.Semanal,
			AbonoSemana:  decimal.NewFromInt(100),
			Vencidas:     decimal.Zero,
			Aporte:       decimal.Zero,
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
			Vencidas:     decimal.Zero,
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

// TestService_ListarRutas_PonderadoDenominador verifies that the ponderado
// denominator respects AplicaPonderado (calendar-based) rather than frecuencia alone.
// The zone has 3 ventas: one SEMANAL that applies (cargo far in the past), one
// QUINCENAL whose next due-date is in the window, and one MENSUAL whose next
// due-date is outside the window. Only the first two count in the denominator.
func TestService_ListarRutas_PonderadoDenominador(t *testing.T) {
	t.Parallel()

	zonaID := 3

	// Window: [fechaInicio=2026-01-12, now≈2026-01-18].
	// For testing AplicaPonderado via enrichVentas, we set FechaCargo on each
	// venta so that AplicaEnVentana gives the expected result:
	//   v1 SEMANAL  cargo=2025-12-29 → k=1 lands on 2026-01-05 (antes de ventana),
	//               k=2 lands on 2026-01-12 → dentro de [01-12..01-18] → aplica.
	//   v2 QUINCENAL cargo=2025-12-20 → 2026-01-15 en ventana → aplica.
	//   v3 MENSUAL   cargo=2026-01-12 → 2026-02-01 fuera de ventana (hasta=01-18) → NO aplica.
	fechaInicio := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 1, 18, 0, 0, 0, 0, time.UTC)

	ventas := []rutasdomain.VentaCobranza{
		{ // v1: SEMANAL, aplica (k=2 → 2026-01-12 en ventana); pagó
			VentaID:      10,
			ClienteID:    200,
			ZonaID:       zonaID,
			Parcialidad:  decimal.NewFromInt(100),
			Frecuencia:   rutasdomain.Semanal,
			AbonoSemana:  decimal.NewFromInt(100),
			Saldo:        decimal.NewFromInt(900),
			TotalImporte: decimal.NewFromInt(4000),
			FechaCargo:   time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC),
		},
		{ // v2: QUINCENAL, aplica (01-15 en ventana); no pagó
			VentaID:      11,
			ClienteID:    201,
			ZonaID:       zonaID,
			Parcialidad:  decimal.NewFromInt(200),
			Frecuencia:   rutasdomain.Quincenal,
			AbonoSemana:  decimal.Zero,
			Saldo:        decimal.NewFromInt(2000),
			TotalImporte: decimal.NewFromInt(4000),
			FechaCargo:   time.Date(2025, 12, 20, 0, 0, 0, 0, time.UTC),
		},
		{ // v3: MENSUAL, NO aplica (02-01 fuera de ventana); no pagó
			VentaID:      12,
			ClienteID:    202,
			ZonaID:       zonaID,
			Parcialidad:  decimal.NewFromInt(150),
			Frecuencia:   rutasdomain.Mensual,
			AbonoSemana:  decimal.Zero,
			Saldo:        decimal.NewFromInt(3000),
			TotalImporte: decimal.NewFromInt(6000),
			FechaCargo:   time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC),
		},
	}

	// We need a controlled now for the calendar window. Since the service always
	// calls time.Now() internally, we drive enrichVentas directly in a unit test
	// for calcReporteZona and trust the integration path via ListarRutas for the
	// cobertura metric. Here we verify enrichVentas + calcReporteZona directly.
	enrichVentas(ventas, fechaInicio, now)

	assert.True(t, ventas[0].AplicaPonderado, "v1 SEMANAL should apply")
	assert.True(t, ventas[1].AplicaPonderado, "v2 QUINCENAL 01-15 in window")
	assert.False(t, ventas[2].AplicaPonderado, "v3 MENSUAL 02-01 outside window")

	reporte := calcReporteZona(zonaID, ventas)

	// Cobertura: 3 ventas, 1 pagó → 33.33…%
	require.NotNil(t, reporte.PctCoberturaSemanal)

	// Ponderado denominator: 2 (v1 + v2); v1 paid 1 cuota, v2 paid 0 cuotas.
	// aporte(v1)=1, aporte(v2)=0 → sum=1, den=2 → 50%.
	require.NotNil(t, reporte.PctPonderadoSemanal)
	assert.True(t,
		decimal.NewFromFloat(50.0).Equal(*reporte.PctPonderadoSemanal),
		"ponderado %s (expected 50%%)", reporte.PctPonderadoSemanal,
	)
}
