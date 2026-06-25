//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// TestService_ListarReporteUsuarios verifies the per-user report: two users on the
// same COBRADOR_ID/zona produce two independent rows (each with its own window),
// inactive users are skipped, the zona cartera is shared, and rows are sorted.
func TestService_ListarReporteUsuarios(t *testing.T) {
	t.Parallel()

	zonaID := 21563
	rutas := []rutasdomain.RutaResumen{{
		ZonaID:      zonaID,
		ZonaNombre:  "R/25",
		NumClientes: 848,
		SaldoTotal:  decimal.NewFromInt(1456200),
	}}

	// One credit venta in the zona that paid its cuota → cobertura 100%
	// regardless of window (the fake supplies AbonoSemana directly).
	ventas := []rutasdomain.VentaCobranza{{
		VentaID:      1,
		ZonaID:       zonaID,
		Parcialidad:  decimal.NewFromInt(100),
		Frecuencia:   rutasdomain.Semanal,
		AbonoSemana:  decimal.NewFromInt(100),
		Saldo:        decimal.NewFromInt(900),
		TotalImporte: decimal.NewFromInt(4000),
		FechaCargo:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}

	usuarios := []outbound.UsuarioCobrador{
		{
			UID: "noe", Nombre: "NOE CORTERO", Email: "noe@gmail.com", CobradorID: 11502, ZonaID: zonaID,
			FechaInicio: time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC),
		},
		{
			UID: "aldrich", Nombre: "ALDRICH CORTERO", Email: "abdimuy@gmail.com", CobradorID: 11502, ZonaID: zonaID,
			FechaInicio: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		},
		// inactive: no FechaInicio → skipped
		{UID: "sinfecha", Nombre: "SIN FECHA", CobradorID: 11502, ZonaID: zonaID},
		// not a cobrador: CobradorID 0 → skipped
		{
			UID: "office", Nombre: "OFICINA", CobradorID: 0, ZonaID: 0,
			FechaInicio: time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC),
		},
	}

	svc := NewService(
		&fakeRutasRepo{rows: rutas},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{zonaID: ventas}},
		&fakeCalendario{usuarios: usuarios},
	)

	got, err := svc.ListarReporteUsuarios(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2, "solo los 2 cobradores activos")

	// Sorted by zona then nombre → ALDRICH antes que NOE.
	assert.Equal(t, "ALDRICH CORTERO", got[0].Nombre)
	assert.Equal(t, "NOE CORTERO", got[1].Nombre)

	for _, r := range got {
		assert.Equal(t, zonaID, r.ZonaID)
		assert.Equal(t, "R/25", r.ZonaNombre, "cartera por zona, compartida")
		assert.Equal(t, 848, r.NumClientes)
		assert.True(t, decimal.NewFromInt(1456200).Equal(r.SaldoTotal))
		require.NotNil(t, r.PctCoberturaSemanal, "cobertura calculada por usuario")
		assert.True(t, decimal.NewFromInt(100).Equal(*r.PctCoberturaSemanal),
			"cobertura %s (1 de 1 pagó)", r.PctCoberturaSemanal)
		assert.Equal(t, 1, r.CoberturaNum, "1 venta pagó")
		assert.Equal(t, 1, r.CoberturaDen, "1 venta en cartera (divisor)")
		assert.False(t, r.FechaInicio.IsZero(), "ventana propia del usuario")
	}
	// Cada usuario conserva SU ventana.
	assert.Equal(t, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC), got[0].FechaInicio)
	assert.Equal(t, time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC), got[1].FechaInicio)
}

// TestService_ListarReporteUsuarios_CalendarioError degrades to an empty list
// (non-fatal) when Firestore is unavailable.
func TestService_ListarReporteUsuarios_CalendarioError(t *testing.T) {
	t.Parallel()

	svc := NewService(
		&fakeRutasRepo{rows: []rutasdomain.RutaResumen{}},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{}},
		&fakeCalendario{usuariosE: errors.New("firestore down")},
	)

	got, err := svc.ListarReporteUsuarios(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestService_DesglosePorUsuario uses the user's own window and returns the
// user's zona; an unknown/inactive uid yields an empty breakdown (no error).
func TestService_DesglosePorUsuario(t *testing.T) {
	t.Parallel()

	zonaID := 21563
	ventas := []rutasdomain.VentaCobranza{{
		VentaID:      1,
		ZonaID:       zonaID,
		Parcialidad:  decimal.NewFromInt(100),
		Frecuencia:   rutasdomain.Semanal,
		AbonoSemana:  decimal.NewFromInt(100),
		Saldo:        decimal.NewFromInt(900),
		TotalImporte: decimal.NewFromInt(4000),
		FechaCargo:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}
	fecha := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	svc := NewService(
		&fakeRutasRepo{rows: []rutasdomain.RutaResumen{{ZonaID: zonaID, ZonaNombre: "R/25"}}},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{zonaID: ventas}},
		&fakeCalendario{usuarios: []outbound.UsuarioCobrador{
			{UID: "noe", Nombre: "NOE CORTERO", CobradorID: 11502, ZonaID: zonaID, FechaInicio: fecha},
		}},
	)

	got, fi, zona, err := svc.DesglosePorUsuario(context.Background(), "noe")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, zonaID, zona)
	require.NotNil(t, fi)
	assert.Equal(t, fecha, *fi)

	// Unknown uid → empty, no error.
	empty, fi2, zona2, err2 := svc.DesglosePorUsuario(context.Background(), "desconocido")
	require.NoError(t, err2)
	assert.Empty(t, empty)
	assert.Nil(t, fi2)
	assert.Zero(t, zona2)
}

// TestService_ListarReporteUsuarios_VentasError keeps the row but leaves its
// percentages nil when the per-user venta fetch fails.
func TestService_ListarReporteUsuarios_VentasError(t *testing.T) {
	t.Parallel()

	zonaID := 21563
	svc := NewService(
		&fakeRutasRepo{rows: []rutasdomain.RutaResumen{{ZonaID: zonaID, ZonaNombre: "R/25"}}},
		&fakeCobranzaRepo{err: errors.New("fb down")},
		&fakeCalendario{usuarios: []outbound.UsuarioCobrador{
			{
				UID: "noe", Nombre: "NOE CORTERO", CobradorID: 11502, ZonaID: zonaID,
				FechaInicio: time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC),
			},
		}},
	)

	got, err := svc.ListarReporteUsuarios(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Nil(t, got[0].PctCoberturaSemanal)
	assert.Nil(t, got[0].PctPonderadoSemanal)
}
