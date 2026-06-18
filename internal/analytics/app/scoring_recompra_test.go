//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// ─── computeRecompraScore gating tests ───────────────────────────────────────

func TestComputeRecompraScore_VentasMesesDistintos_Zero_AplicaFalse(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadRecompraScorecard()
	require.NoError(t, err)
	btyd, err := app.LoadBTYD()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	// VentasMesesDistintos=0 → aplica=false even with a non-zero FechaPrimerVenta.
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            1,
		Nombre:               "Sin Ventas V",
		Zona:                 "Z1",
		FechaUltimaCompra:    now.AddDate(0, -6, 0),
		Frecuencia:           3,
		Monetary:             decimal.NewFromInt(15_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         now.AddDate(-1, 0, 0),
		Now:                  now,
		FechaPrimerVenta:     now.AddDate(-2, 0, 0), // non-zero, but...
		VentasMesesDistintos: 0,                     // ...zero months → aplica=false
	})

	score, banda, drivers, aplica := app.ExportComputeRecompraScore(c, now, sc, btyd)

	assert.False(t, aplica, "VentasMesesDistintos=0 must yield aplica=false")
	assert.Equal(t, 0, score.Int(), "score must be 0 when aplica=false")
	assert.Empty(t, banda.String(), "banda must be empty when aplica=false")
	assert.Nil(t, drivers, "drivers must be nil when aplica=false")
}

func TestComputeRecompraScore_ZeroFechaPrimerVenta_AplicaFalse(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadRecompraScorecard()
	require.NoError(t, err)
	btyd, err := app.LoadBTYD()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	// FechaPrimerVenta zero → aplica=false.
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:         2,
		Nombre:            "Sin Fecha Venta",
		Zona:              "Z1",
		FechaUltimaCompra: now.AddDate(0, -3, 0),
		Frecuencia:        5,
		Monetary:          decimal.NewFromInt(20_000),
		Saldo:             decimal.Zero,
		PorLiquidarPct:    decimal.Zero,
		CohorteFecha:      now.AddDate(-1, 0, 0),
		Now:               now,
		// FechaPrimerVenta intentionally zero
		VentasMesesDistintos: 3, // non-zero, but date is zero
	})

	score, banda, drivers, aplica := app.ExportComputeRecompraScore(c, now, sc, btyd)

	assert.False(t, aplica, "zero FechaPrimerVenta must yield aplica=false")
	assert.Equal(t, 0, score.Int())
	assert.Empty(t, banda.String())
	assert.Nil(t, drivers)
}

func TestComputeRecompraScore_ZeroScorecard_AplicaFalse(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            3,
		Nombre:               "Test",
		Zona:                 "Z1",
		FechaUltimaCompra:    now.AddDate(-1, 0, 0),
		Frecuencia:           5,
		Monetary:             decimal.NewFromInt(20_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         now.AddDate(-2, 0, 0),
		Now:                  now,
		FechaPrimerVenta:     now.AddDate(-2, 0, 0),
		FechaUltimaVenta:     now.AddDate(-1, 0, 0),
		VentasMesesDistintos: 5,
	})

	var zeroSc app.RecompraScorecard // Loaded() == false

	score, banda, drivers, aplica := app.ExportComputeRecompraScore(c, now, zeroSc, btyd)

	assert.False(t, aplica, "zero scorecard must return aplica=false")
	assert.Equal(t, 0, score.Int())
	assert.Empty(t, banda.String())
	assert.Nil(t, drivers)
}

func TestComputeRecompraScore_WithVHistory_AplicaTrue(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadRecompraScorecard()
	require.NoError(t, err)
	btyd, err := app.LoadBTYD()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	// Client with V purchase history → aplica=true, valid score and banda.
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            10,
		Nombre:               "Cliente Con Ventas",
		Zona:                 "Z1",
		FechaUltimaCompra:    now.AddDate(0, -2, 0),
		Frecuencia:           5,
		Monetary:             decimal.NewFromInt(25_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         now.AddDate(-3, 0, 0),
		Now:                  now,
		PctPagosATiempo:      decimal.NewFromFloat(80),
		FechaPrimerCargo:     now.AddDate(-3, 0, 0),
		FechaUltimoPago:      now.AddDate(0, -1, 0),
		FechaPrimerVenta:     now.AddDate(-3, 0, 0),
		FechaUltimaVenta:     now.AddDate(0, -2, 0),
		VentasMesesDistintos: 5,
		MonetaryVProm:        decimal.NewFromInt(10_000),
	})

	score, banda, drivers, aplica := app.ExportComputeRecompraScore(c, now, sc, btyd)

	assert.True(t, aplica, "client with V history must have aplica=true")
	assert.GreaterOrEqual(t, score.Int(), 0)
	assert.LessOrEqual(t, score.Int(), 100)
	assert.True(t, banda.IsValid(), "banda must be a valid BandaRecompra")
	assert.LessOrEqual(t, len(drivers), 3)
}

// ─── buildRecompraFeatures grid math test ────────────────────────────────────

// TestBuildRecompraFeatures_GridMath verifies the BG/BB monthly grid computation
// for a hand-checked example:
//
//	acq=2023-01, last=2024-01, VentasMesesDistintos=3, now=2024-07
//	→ acqMonth=2023*12+0=24276, lastMonth=2024*12+0=24288, nowMonth=2024*12+6=24294
//	→ n = 24294 - 24276 = 18
//	→ tx = clamp(24288 - 24276, 0, 18) = clamp(12, 0, 18) = 12
//	→ x = max(0, 3-1) = 2
//	→ RECENCIA_MESES = 24294 - 24288 = 6
//	→ FRECUENCIA_V = 2
//	→ ANTIGUEDAD_MESES = 18
func TestBuildRecompraFeatures_GridMath(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)

	now := time.Date(2024, 7, 15, 12, 0, 0, 0, time.UTC)

	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            99,
		Nombre:               "Grid Test",
		Zona:                 "Z1",
		FechaUltimaCompra:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		Frecuencia:           3,
		Monetary:             decimal.NewFromInt(15_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		Now:                  now,
		FechaPrimerVenta:     time.Date(2023, 1, 15, 0, 0, 0, 0, time.UTC), // acq=2023-01
		FechaUltimaVenta:     time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), // last=2024-01
		VentasMesesDistintos: 3,
		MonetaryVProm:        decimal.NewFromInt(5_000),
		PctPagosATiempo:      decimal.NewFromFloat(75),
	})

	got := app.ExportBuildRecompraFeatures(c, now, btyd)

	const delta = 1e-9

	// Grid assertions
	assert.InDelta(t, 6.0, got["RECENCIA_MESES"], delta, "RECENCIA_MESES")
	assert.InDelta(t, 2.0, got["FRECUENCIA_V"], delta, "FRECUENCIA_V")
	assert.InDelta(t, 18.0, got["ANTIGUEDAD_MESES"], delta, "ANTIGUEDAD_MESES")

	// Keys existence
	require.Contains(t, got, "BGBB_EXP_12M")
	require.Contains(t, got, "BGBB_P_ALIVE")
	require.Contains(t, got, "MONETARY_LOG")
	require.Contains(t, got, "PCT_PAGOS_A_TIEMPO")
	require.Contains(t, got, "DIAS_SIN_PAGAR")

	// MONETARY_LOG = log1p(5000) ≈ 8.517
	assert.InDelta(t, 8.517, got["MONETARY_LOG"], 0.001, "MONETARY_LOG")

	// PCT_PAGOS_A_TIEMPO = 75/100 = 0.75
	assert.InDelta(t, 0.75, got["PCT_PAGOS_A_TIEMPO"], delta, "PCT_PAGOS_A_TIEMPO")

	// BGBB values must be finite and non-negative
	assert.GreaterOrEqual(t, got["BGBB_EXP_12M"], 0.0, "BGBB_EXP_12M >= 0")
	assert.GreaterOrEqual(t, got["BGBB_P_ALIVE"], 0.0, "BGBB_P_ALIVE >= 0")
	assert.LessOrEqual(t, got["BGBB_P_ALIVE"], 1.0, "BGBB_P_ALIVE <= 1")
}

func TestBuildRecompraFeatures_DiaSinPagarFallback(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	// FechaUltimoPago zero → DIAS_SIN_PAGAR falls back to FechaPrimerCargo.
	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:         50,
		Nombre:            "Fallback Test",
		Zona:              "Z1",
		FechaUltimaCompra: now.AddDate(-1, 0, 0),
		Frecuencia:        2,
		Monetary:          decimal.NewFromInt(10_000),
		Saldo:             decimal.Zero,
		PorLiquidarPct:    decimal.Zero,
		CohorteFecha:      now.AddDate(-2, 0, 0),
		Now:               now,
		FechaPrimerCargo:  now.AddDate(0, 0, -60), // 60 days ago
		// FechaUltimoPago intentionally zero
		FechaPrimerVenta:     now.AddDate(-2, 0, 0),
		FechaUltimaVenta:     now.AddDate(-1, 0, 0),
		VentasMesesDistintos: 2,
	})

	got := app.ExportBuildRecompraFeatures(c, now, btyd)

	// When FechaUltimoPago is zero, DIAS_SIN_PAGAR should equal daysSince(FechaPrimerCargo)
	assert.InDelta(t, 60.0, got["DIAS_SIN_PAGAR"], 1.0, "DIAS_SIN_PAGAR fallback to FechaPrimerCargo ≈60d")
}

func TestBuildRecompraFeatures_AllEightKeysPresent(t *testing.T) {
	t.Parallel()

	btyd, err := app.LoadBTYD()
	require.NoError(t, err)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	c := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:            100,
		Nombre:               "Keys Test",
		Zona:                 "Z1",
		FechaUltimaCompra:    now.AddDate(-1, 0, 0),
		Frecuencia:           5,
		Monetary:             decimal.NewFromInt(20_000),
		Saldo:                decimal.Zero,
		PorLiquidarPct:       decimal.Zero,
		CohorteFecha:         now.AddDate(-3, 0, 0),
		Now:                  now,
		FechaPrimerVenta:     now.AddDate(-3, 0, 0),
		FechaUltimaVenta:     now.AddDate(-1, 0, 0),
		VentasMesesDistintos: 5,
		MonetaryVProm:        decimal.NewFromInt(8_000),
		PctPagosATiempo:      decimal.NewFromFloat(80),
		FechaUltimoPago:      now.AddDate(0, 0, -20),
	})

	got := app.ExportBuildRecompraFeatures(c, now, btyd)

	expectedKeys := []string{
		"BGBB_EXP_12M",
		"BGBB_P_ALIVE",
		"RECENCIA_MESES",
		"FRECUENCIA_V",
		"ANTIGUEDAD_MESES",
		"MONETARY_LOG",
		"PCT_PAGOS_A_TIEMPO",
		"DIAS_SIN_PAGAR",
	}

	for _, key := range expectedKeys {
		require.Contains(t, got, key, "expected key %q in features map", key)
	}
	assert.Len(t, got, 8, "feature map must have exactly 8 keys")
}
