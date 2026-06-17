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

	const delta = 1e-9 // float64 tolerance

	type tc struct {
		name              string
		porLiquidarPct    float64
		pctPagosATiempo   float64
		diasAtrasoProm    int
		wantSaldoFrac     float64
		wantCoberturaPlan float64
		wantPctPagos      float64
		wantDiasAtraso    float64
	}

	tests := []tc{
		{
			name:              "PorLiquidarPct=120 clamps SALDO_FRAC=1.0, COBERTURA_PLAN=0.0",
			porLiquidarPct:    120,
			pctPagosATiempo:   0,
			diasAtrasoProm:    0,
			wantSaldoFrac:     1.0,
			wantCoberturaPlan: 0.0,
			wantPctPagos:      0.0,
			wantDiasAtraso:    0.0,
		},
		{
			name:              "PorLiquidarPct=0 → SALDO_FRAC=0.0, COBERTURA_PLAN=1.0",
			porLiquidarPct:    0,
			pctPagosATiempo:   0,
			diasAtrasoProm:    0,
			wantSaldoFrac:     0.0,
			wantCoberturaPlan: 1.0,
			wantPctPagos:      0.0,
			wantDiasAtraso:    0.0,
		},
		{
			name:              "PorLiquidarPct=40, PctPagosATiempo=70, DiasAtrasoProm=8",
			porLiquidarPct:    40,
			pctPagosATiempo:   70,
			diasAtrasoProm:    8,
			wantSaldoFrac:     0.4,
			wantCoberturaPlan: 0.6,
			wantPctPagos:      0.7,
			wantDiasAtraso:    8.0,
		},
		{
			name:              "negative PorLiquidarPct clamps SALDO_FRAC=0.0",
			porLiquidarPct:    -10,
			pctPagosATiempo:   150,
			diasAtrasoProm:    5,
			wantSaldoFrac:     0.0,
			wantCoberturaPlan: 1.0,
			wantPctPagos:      1.0,
			wantDiasAtraso:    5.0,
		},
		{
			name:              "midpoint PorLiquidarPct=50 → SALDO_FRAC=0.5, COBERTURA_PLAN=0.5",
			porLiquidarPct:    50,
			pctPagosATiempo:   50,
			diasAtrasoProm:    15,
			wantSaldoFrac:     0.5,
			wantCoberturaPlan: 0.5,
			wantPctPagos:      0.5,
			wantDiasAtraso:    15.0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := mustCandidato(domain.CrearWinbackCandidatoParams{
				ClienteID:         1,
				Nombre:            "Test Credito",
				Zona:              "Z1",
				FechaUltimaCompra: testNow.AddDate(0, 0, -200),
				Frecuencia:        5,
				Monetary:          decimal.NewFromInt(20_000),
				Saldo:             decimal.NewFromInt(5_000),
				PorLiquidarPct:    decimal.NewFromFloat(tt.porLiquidarPct),
				PctPagosATiempo:   decimal.NewFromFloat(tt.pctPagosATiempo),
				DiasAtrasoProm:    tt.diasAtrasoProm,
				CohorteFecha:      testNow.AddDate(-1, 0, 0),
				Now:               testNow,
			})

			got := app.ExportBuildCreditoFeatures(c)

			require.Contains(t, got, "SALDO_FRAC")
			require.Contains(t, got, "COBERTURA_PLAN")
			require.Contains(t, got, "PCT_PAGOS_A_TIEMPO_6M")
			require.Contains(t, got, "DIAS_ATRASO_PROM")

			assert.InDelta(t, tt.wantSaldoFrac, got["SALDO_FRAC"], delta, "SALDO_FRAC")
			assert.InDelta(t, tt.wantCoberturaPlan, got["COBERTURA_PLAN"], delta, "COBERTURA_PLAN")
			assert.InDelta(t, tt.wantPctPagos, got["PCT_PAGOS_A_TIEMPO_6M"], delta, "PCT_PAGOS_A_TIEMPO_6M")
			assert.InDelta(t, tt.wantDiasAtraso, got["DIAS_ATRASO_PROM"], delta, "DIAS_ATRASO_PROM")
		})
	}
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
			Frecuencia:        8,
			Monetary:          decimal.NewFromInt(30_000),
			Saldo:             decimal.NewFromInt(8_000),
			PorLiquidarPct:    decimal.NewFromFloat(40.0),
			PctPagosATiempo:   decimal.NewFromFloat(80.0),
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

		// Low-risk: low saldo fraction, high punctuality, low atraso
		lowRisk := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         10,
			Nombre:            "Buen Pagador",
			Zona:              "Z1",
			FechaUltimaCompra: now.AddDate(0, 0, -200),
			FechaUltimoPago:   now.AddDate(0, 0, -10),
			Frecuencia:        10,
			Monetary:          decimal.NewFromInt(40_000),
			Saldo:             decimal.NewFromInt(2_000),
			PorLiquidarPct:    decimal.NewFromFloat(5.0),
			PctPagosATiempo:   decimal.NewFromFloat(95.0),
			DiasAtrasoProm:    1,
			CohorteFecha:      now.AddDate(-1, 0, 0),
			Now:               now,
		})

		// High-risk: high saldo fraction, low punctuality, high atraso
		highRisk := mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         11,
			Nombre:            "Mal Pagador",
			Zona:              "Z1",
			FechaUltimaCompra: now.AddDate(0, 0, -200),
			FechaUltimoPago:   now.AddDate(0, 0, -10),
			Frecuencia:        10,
			Monetary:          decimal.NewFromInt(40_000),
			Saldo:             decimal.NewFromInt(10_000),
			PorLiquidarPct:    decimal.NewFromFloat(90.0),
			PctPagosATiempo:   decimal.NewFromFloat(20.0),
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
