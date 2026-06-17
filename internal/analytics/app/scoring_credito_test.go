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

// ─── TestBuildCreditoFeatures ─────────────────────────────────────────────────

func TestBuildCreditoFeatures(t *testing.T) {
	t.Parallel()

	const delta = 1e-6 // float64 tolerance

	testNowLocal := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)

	t.Run("FechaUltimoPago 10 days ago, FechaPrimerCargo 100 days ago", func(t *testing.T) {
		t.Parallel()

		c := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         1,
			Nombre:            "Test Credito",
			Zona:              "Z1",
			FechaUltimaCompra: testNowLocal.AddDate(0, 0, -200),
			FechaUltimoPago:   testNowLocal.AddDate(0, 0, -10),
			FechaPrimerCargo:  testNowLocal.AddDate(0, 0, -100),
			Frecuencia:        5,
			Monetary:          decimal.NewFromInt(20_000),
			Saldo:             decimal.NewFromInt(5_000),
			PorLiquidarPct:    decimal.NewFromFloat(40),
			PctPagosATiempo:   decimal.NewFromFloat(85),
			NumPagos:          20,
			Pagos90D:          7,
			CadenciaDias:      28,
			DiasAtrasoProm:    3,
			CohorteFecha:      testNowLocal.AddDate(-1, 0, 0),
			Now:               testNowLocal,
		})

		got := app.ExportBuildCreditoFeatures(c, testNowLocal)

		require.Contains(t, got, "DIAS_SIN_PAGAR")
		require.Contains(t, got, "PAGOS_90D")
		require.Contains(t, got, "PCT_PAGOS_A_TIEMPO_6M")
		require.Contains(t, got, "CADENCIA_DIAS")
		require.Contains(t, got, "NUM_PAGOS_TOTAL")
		require.Contains(t, got, "ANTIGUEDAD_DIAS")

		assert.InDelta(t, 10.0, got["DIAS_SIN_PAGAR"], delta, "DIAS_SIN_PAGAR")
		assert.InDelta(t, 7.0, got["PAGOS_90D"], delta, "PAGOS_90D")
		assert.InDelta(t, 0.85, got["PCT_PAGOS_A_TIEMPO_6M"], delta, "PCT_PAGOS_A_TIEMPO_6M")
		assert.InDelta(t, 28.0, got["CADENCIA_DIAS"], delta, "CADENCIA_DIAS")
		assert.InDelta(t, 20.0, got["NUM_PAGOS_TOTAL"], delta, "NUM_PAGOS_TOTAL")
		assert.InDelta(t, 100.0, got["ANTIGUEDAD_DIAS"], delta, "ANTIGUEDAD_DIAS")
	})

	t.Run("FechaUltimoPago zero falls back to FechaPrimerCargo for DIAS_SIN_PAGAR", func(t *testing.T) {
		t.Parallel()

		c := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         2,
			Nombre:            "Sin Pagos",
			Zona:              "Z1",
			FechaUltimaCompra: testNowLocal.AddDate(0, 0, -300),
			// FechaUltimoPago intentionally zero
			FechaPrimerCargo: testNowLocal.AddDate(0, 0, -50),
			Frecuencia:       3,
			Monetary:         decimal.NewFromInt(10_000),
			Saldo:            decimal.NewFromInt(2_000),
			PorLiquidarPct:   decimal.NewFromFloat(20),
			PctPagosATiempo:  decimal.Zero,
			NumPagos:         0,
			Pagos90D:         0,
			CohorteFecha:     testNowLocal.AddDate(-1, 0, 0),
			Now:              testNowLocal,
		})

		got := app.ExportBuildCreditoFeatures(c, testNowLocal)

		// When FechaUltimoPago is zero, DIAS_SIN_PAGAR == ANTIGUEDAD_DIAS
		assert.InDelta(t, got["ANTIGUEDAD_DIAS"], got["DIAS_SIN_PAGAR"], delta, "DIAS_SIN_PAGAR should equal ANTIGUEDAD_DIAS when FechaUltimoPago is zero")
		assert.InDelta(t, 50.0, got["DIAS_SIN_PAGAR"], delta, "DIAS_SIN_PAGAR fallback to FechaPrimerCargo")
		assert.InDelta(t, 50.0, got["ANTIGUEDAD_DIAS"], delta, "ANTIGUEDAD_DIAS")
	})

	t.Run("Both FechaUltimoPago and FechaPrimerCargo zero → both features zero", func(t *testing.T) {
		t.Parallel()

		c := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         3,
			Nombre:            "Cliente Nuevo",
			Zona:              "Z1",
			FechaUltimaCompra: testNowLocal.AddDate(0, 0, -100),
			// FechaUltimoPago zero, FechaPrimerCargo zero
			Frecuencia:     1,
			Monetary:       decimal.NewFromInt(5_000),
			Saldo:          decimal.Zero,
			PorLiquidarPct: decimal.Zero,
			CohorteFecha:   testNowLocal.AddDate(-1, 0, 0),
			Now:            testNowLocal,
		})

		got := app.ExportBuildCreditoFeatures(c, testNowLocal)

		assert.InDelta(t, 0.0, got["DIAS_SIN_PAGAR"], delta, "DIAS_SIN_PAGAR zero when both dates are zero")
		assert.InDelta(t, 0.0, got["ANTIGUEDAD_DIAS"], delta, "ANTIGUEDAD_DIAS zero when FechaPrimerCargo is zero")
	})

	t.Run("PctPagosATiempo=80 → PCT_PAGOS_A_TIEMPO_6M=0.8", func(t *testing.T) {
		t.Parallel()

		c := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         4,
			Nombre:            "Test Pct",
			Zona:              "Z1",
			FechaUltimaCompra: testNowLocal.AddDate(0, 0, -200),
			Frecuencia:        5,
			Monetary:          decimal.NewFromInt(15_000),
			Saldo:             decimal.NewFromInt(3_000),
			PorLiquidarPct:    decimal.NewFromFloat(20),
			PctPagosATiempo:   decimal.NewFromFloat(80),
			CohorteFecha:      testNowLocal.AddDate(-1, 0, 0),
			Now:               testNowLocal,
		})

		got := app.ExportBuildCreditoFeatures(c, testNowLocal)

		assert.InDelta(t, 0.8, got["PCT_PAGOS_A_TIEMPO_6M"], delta, "PCT_PAGOS_A_TIEMPO_6M=0.8")
	})
}

// ─── TestComputeCreditoScore ──────────────────────────────────────────────────

func TestComputeCreditoScore(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadScorecard()
	require.NoError(t, err, "LoadScorecard must succeed with embedded JSON")
	require.True(t, sc.Loaded(), "loaded scorecard must report Loaded()==true")

	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

	t.Run("credit client → aplica=true, valid score and banda", func(t *testing.T) {
		t.Parallel()

		c := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         1,
			Nombre:            "Cliente Credito",
			Zona:              "Z1",
			FechaUltimaCompra: now.AddDate(0, 0, -200),
			FechaUltimoPago:   now.AddDate(0, 0, -15),
			FechaPrimerCargo:  now.AddDate(-2, 0, 0),
			Frecuencia:        8,
			Monetary:          decimal.NewFromInt(30_000),
			Saldo:             decimal.NewFromInt(8_000),
			PorLiquidarPct:    decimal.NewFromFloat(40.0),
			PctPagosATiempo:   decimal.NewFromFloat(80.0),
			NumPagos:          30,
			Pagos90D:          5,
			CadenciaDias:      25,
			DiasAtrasoProm:    5,
			CohorteFecha:      now.AddDate(-1, 0, 0),
			Now:               now,
		})

		score, banda, drivers, aplica := app.ExportComputeCreditoScore(c, now, sc)

		assert.True(t, aplica, "credit client must have aplica=true")
		assert.GreaterOrEqual(t, score.Int(), 0, "score must be >= 0")
		assert.LessOrEqual(t, score.Int(), 100, "score must be <= 100")
		assert.True(t, banda.IsValid(), "banda must be a valid BandaCredito")
		assert.LessOrEqual(t, len(drivers), 3, "at most 3 drivers")
	})

	t.Run("SIN_CREDITO client → aplica=false, zero score, empty banda, nil drivers", func(t *testing.T) {
		t.Parallel()

		// saldo=0 and fechaUltimoPago=zero → SIN_CREDITO
		c := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         2,
			Nombre:            "Cliente Contado",
			Zona:              "Z1",
			FechaUltimaCompra: now.AddDate(0, 0, -300),
			Frecuencia:        4,
			Monetary:          decimal.NewFromInt(15_000),
			Saldo:             decimal.Zero,
			PorLiquidarPct:    decimal.Zero,
			CohorteFecha:      now.AddDate(-1, 0, 0),
			Now:               now,
			// FechaUltimoPago intentionally zero — contado client
		})

		score, banda, drivers, aplica := app.ExportComputeCreditoScore(c, now, sc)

		assert.False(t, aplica, "SIN_CREDITO client must have aplica=false")
		assert.Equal(t, 0, score.Int(), "score must be 0 when no aplica")
		assert.Empty(t, banda.String(), "banda must be empty when no aplica")
		assert.Nil(t, drivers, "drivers must be nil when no aplica")
	})

	t.Run("zero Scorecard → aplica=false for credit client", func(t *testing.T) {
		t.Parallel()

		// A credit client with saldo > 0 and payment history
		c := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         3,
			Nombre:            "Cliente Con Saldo",
			Zona:              "Z1",
			FechaUltimaCompra: now.AddDate(0, 0, -150),
			FechaUltimoPago:   now.AddDate(0, 0, -10),
			Frecuencia:        6,
			Monetary:          decimal.NewFromInt(25_000),
			Saldo:             decimal.NewFromInt(5_000),
			PorLiquidarPct:    decimal.NewFromFloat(30.0),
			CohorteFecha:      now.AddDate(-1, 0, 0),
			Now:               now,
		})

		// Zero Scorecard{} — Loaded() == false
		var zeroSc app.Scorecard

		score, banda, drivers, aplica := app.ExportComputeCreditoScore(c, now, zeroSc)

		assert.False(t, aplica, "zero scorecard must return aplica=false")
		assert.Equal(t, 0, score.Int())
		assert.Empty(t, banda.String())
		assert.Nil(t, drivers)
	})

	t.Run("high-risk client scores lower than low-risk client", func(t *testing.T) {
		t.Parallel()

		// Low-risk: many recent payments, high punctuality, low days since payment,
		// long history (established client). These differentiate clearly under v1.
		lowRisk := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         10,
			Nombre:            "Buen Pagador",
			Zona:              "Z1",
			FechaUltimaCompra: now.AddDate(0, 0, -200),
			FechaUltimoPago:   now.AddDate(0, 0, -5), // paid 5 days ago
			FechaPrimerCargo:  now.AddDate(-3, 0, 0), // 3 years history
			Frecuencia:        10,
			Monetary:          decimal.NewFromInt(40_000),
			Saldo:             decimal.NewFromInt(2_000),
			PorLiquidarPct:    decimal.NewFromFloat(5.0),
			PctPagosATiempo:   decimal.NewFromFloat(95.0),
			NumPagos:          120,
			Pagos90D:          10, // many recent payments
			CadenciaDias:      25,
			DiasAtrasoProm:    1,
			CohorteFecha:      now.AddDate(-1, 0, 0),
			Now:               now,
		})

		// High-risk: few recent payments, low punctuality, many days since payment,
		// short history (new/unstable client).
		highRisk := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         11,
			Nombre:            "Mal Pagador",
			Zona:              "Z1",
			FechaUltimaCompra: now.AddDate(0, 0, -200),
			FechaUltimoPago:   now.AddDate(0, 0, -90), // paid 90 days ago (very late)
			FechaPrimerCargo:  now.AddDate(0, -6, 0),  // 6 months history (new)
			Frecuencia:        10,
			Monetary:          decimal.NewFromInt(40_000),
			Saldo:             decimal.NewFromInt(10_000),
			PorLiquidarPct:    decimal.NewFromFloat(90.0),
			PctPagosATiempo:   decimal.NewFromFloat(20.0),
			NumPagos:          8,
			Pagos90D:          1, // barely any recent payments
			CadenciaDias:      25,
			DiasAtrasoProm:    45,
			CohorteFecha:      now.AddDate(-1, 0, 0),
			Now:               now,
		})

		lowScore, _, _, lowAplica := app.ExportComputeCreditoScore(lowRisk, now, sc)
		highScore, _, highDrivers, highAplica := app.ExportComputeCreditoScore(highRisk, now, sc)

		require.True(t, lowAplica, "low-risk client must have aplica=true")
		require.True(t, highAplica, "high-risk client must have aplica=true")

		assert.Greater(t, lowScore.Int(), highScore.Int(),
			"low-risk client (%d) should score higher than high-risk client (%d)",
			lowScore.Int(), highScore.Int())

		assert.NotEmpty(t, highDrivers, "high-risk client should have at least one driver")
	})
}
