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

func TestDesglosePorZona_NoCobrador(t *testing.T) {
	t.Parallel()

	rows := []rutasdomain.RutaResumen{
		{ZonaID: 99, CobradorID: nil, ZonaNombre: "Sin cobrador"},
	}
	svc := NewService(
		&fakeRutasRepo{rows: rows},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{}},
		&fakeCalendario{m: map[int]time.Time{}},
	)
	ventas, fi, err := svc.DesglosePorZona(context.Background(), 99)
	require.NoError(t, err)
	assert.Empty(t, ventas)
	assert.Nil(t, fi)
}

func TestDesglosePorZona_NoCalendario(t *testing.T) {
	t.Parallel()

	cobradorID := 7
	rows := []rutasdomain.RutaResumen{
		{ZonaID: 1, CobradorID: &cobradorID},
	}
	svc := NewService(
		&fakeRutasRepo{rows: rows},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{}},
		&fakeCalendario{m: map[int]time.Time{}}, // cobrador 7 not in calendar
	)
	ventas, fi, err := svc.DesglosePorZona(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, ventas)
	assert.Nil(t, fi)
}

func TestDesglosePorZona_EnrichAporte(t *testing.T) {
	t.Parallel()

	cobradorID := 5
	zonaID := 1
	rows := []rutasdomain.RutaResumen{
		{ZonaID: zonaID, CobradorID: &cobradorID},
	}
	// fechaInicio: 10 days ago; cadencia SEMANAL=7 → plazos≈1.43
	fechaInicio := time.Now().UTC().AddDate(0, 0, -10)
	// Venta: parcialidad=100, saldo=2900, total=4000, abono=100
	// pagado_antes = 4000 - (2900+100) = 1000
	// plazos ≈ 1.43, debia = MIN(100×1.43, 4000) = 143
	// vencidas = MAX(0, (143-1000)/100) = 0 (pagó más de lo debido)
	// aporte = MIN(100/100, 0+1) = 1.00
	ventas := []rutasdomain.VentaCobranza{
		{
			VentaID:      1,
			ClienteID:    100,
			ZonaID:       zonaID,
			Parcialidad:  decimal.NewFromInt(100),
			Frecuencia:   rutasdomain.Semanal,
			AbonoSemana:  decimal.NewFromInt(100),
			Saldo:        decimal.NewFromInt(2900),
			TotalImporte: decimal.NewFromInt(4000),
			FechaCargo:   fechaInicio.AddDate(0, 0, -30), // 40 days before now
		},
	}

	svc := NewService(
		&fakeRutasRepo{rows: rows},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{zonaID: ventas}},
		&fakeCalendario{m: map[int]time.Time{cobradorID: fechaInicio}},
	)
	got, fi, err := svc.DesglosePorZona(context.Background(), zonaID)
	require.NoError(t, err)
	require.NotNil(t, fi)
	require.Len(t, got, 1)
	// Aporte must be 1.00 (al corriente pays 1 cuota).
	assert.True(t, decimal.NewFromInt(1).Equal(got[0].Aporte),
		"aporte=%s", got[0].Aporte)
}
