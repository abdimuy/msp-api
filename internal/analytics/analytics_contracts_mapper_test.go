package analytics_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestToWinbackCandidatoContract_AllFieldsRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	fechaUltimaCompra := time.Date(2025, 11, 15, 0, 0, 0, 0, time.UTC)
	cohorteFecha := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	c := domain.HydrateWinbackCandidato(domain.HydrateWinbackCandidatoParams{
		ClienteID:         42,
		Nombre:            "Muebles García S.A.",
		Zona:              "Zona Norte",
		Telefono:          "3312345678",
		FechaUltimaCompra: fechaUltimaCompra,
		Frecuencia:        7,
		Monetary:          decimal.NewFromInt(85000),
		Saldo:             decimal.NewFromInt(12000),
		PorLiquidarPct:    decimal.NewFromFloat(14.12),
		NextBestProduct:   "Colchón King Premium",
		EnControl:         true,
		CohorteFecha:      cohorteFecha,
		CreatedAt:         now,
		UpdatedAt:         now,
	})

	got := analytics.ToWinbackCandidatoContract(c)

	assert.Equal(t, 42, got.ClienteID)
	assert.Equal(t, "Muebles García S.A.", got.Nombre)
	assert.Equal(t, "Zona Norte", got.Zona)
	assert.Equal(t, "3312345678", got.Telefono)
	assert.Equal(t, fechaUltimaCompra, got.FechaUltimaCompra)
	assert.Equal(t, 7, got.Frecuencia)
	assert.True(t, decimal.NewFromInt(85000).Equal(got.Monetary))
	assert.True(t, decimal.NewFromInt(12000).Equal(got.Saldo))
	assert.True(t, decimal.NewFromFloat(14.12).Equal(got.PorLiquidarPct))
	assert.Equal(t, "Colchón King Premium", got.NextBestProduct)
	assert.True(t, got.EnControl)
}

func TestToWinbackCandidatoContract_SegmentoAndScoreAreZeroValued(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cohorteFecha := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	c := domain.HydrateWinbackCandidato(domain.HydrateWinbackCandidatoParams{
		ClienteID:    1,
		CohorteFecha: cohorteFecha,
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	got := analytics.ToWinbackCandidatoContract(c)

	// Segmento and Score are enrichment fields populated by the app/HTTP layer,
	// not by the entity-only projection mapper.
	assert.Empty(t, got.Segmento, "Segmento must be zero-valued from entity-only mapper")
	assert.Equal(t, 0, got.Score, "Score must be zero-valued from entity-only mapper")
}

func TestToWinbackCandidatoContracts_SliceHelper(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cohorteFecha := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	candidates := []*domain.WinbackCandidato{
		domain.HydrateWinbackCandidato(domain.HydrateWinbackCandidatoParams{
			ClienteID:    10,
			Nombre:       "Cliente Alfa",
			CohorteFecha: cohorteFecha,
			CreatedAt:    now,
			UpdatedAt:    now,
		}),
		domain.HydrateWinbackCandidato(domain.HydrateWinbackCandidatoParams{
			ClienteID:    20,
			Nombre:       "Cliente Beta",
			CohorteFecha: cohorteFecha,
			CreatedAt:    now,
			UpdatedAt:    now,
		}),
	}

	got := analytics.ToWinbackCandidatoContracts(candidates)

	assert.Len(t, got, 2)
	assert.Equal(t, 10, got[0].ClienteID)
	assert.Equal(t, "Cliente Alfa", got[0].Nombre)
	assert.Equal(t, 20, got[1].ClienteID)
	assert.Equal(t, "Cliente Beta", got[1].Nombre)
}

func TestToWinbackCandidatoContracts_EmptySlice(t *testing.T) {
	t.Parallel()

	got := analytics.ToWinbackCandidatoContracts(nil)
	assert.Empty(t, got)
}

// ─── ToClientePulsoContract ───────────────────────────────────────────────────

func TestToClientePulsoContract_CreditoAplica(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cohorteFecha := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	c := domain.HydrateWinbackCandidato(domain.HydrateWinbackCandidatoParams{
		ClienteID:    55,
		Nombre:       "García Reyes",
		CohorteFecha: cohorteFecha,
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	comp := analytics.PulsoComputado{
		Segmento:        "DORMIDO_VALIOSO",
		Score:           72,
		RecenciaDias:    300,
		EstadoPago:      "AL_CORRIENTE",
		TierRiesgo:      "AL_DIA",
		ScoreCredito:    42,
		BandaCredito:    "ALTO",
		CreditoDrivers:  []string{"saldo alto pendiente"},
		ScoreRecompra:   65,
		BandaRecompra:   "ALTA",
		RecompraDrivers: []string{"frecuencia alta"},
	}

	got := analytics.ToClientePulsoContract(c, comp)

	assert.Equal(t, 55, got.ClienteID)
	assert.Equal(t, 42, got.ScoreCredito)
	assert.Equal(t, "ALTO", got.BandaCredito)
	assert.Equal(t, []string{"saldo alto pendiente"}, got.CreditoDrivers)
	// Verify recompra fields pass through correctly.
	assert.Equal(t, 65, got.ScoreRecompra)
	assert.Equal(t, "ALTA", got.BandaRecompra)
	assert.Equal(t, []string{"frecuencia alta"}, got.RecompraDrivers)
	// Verify other computed fields pass through correctly too.
	assert.Equal(t, 72, got.Score)
	assert.Equal(t, "DORMIDO_VALIOSO", got.Segmento)
	assert.Equal(t, "AL_CORRIENTE", got.EstadoPago)
	assert.Equal(t, 300, got.RecenciaDias)
	assert.Equal(t, "AL_DIA", got.TierRiesgo)
}

func TestToClientePulsoContract_CreditoNoAplica(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cohorteFecha := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	c := domain.HydrateWinbackCandidato(domain.HydrateWinbackCandidatoParams{
		ClienteID:    56,
		Nombre:       "Contado Siempre",
		CohorteFecha: cohorteFecha,
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	comp := analytics.PulsoComputado{
		Segmento:        "PERDIDO",
		Score:           15,
		RecenciaDias:    800,
		EstadoPago:      "SIN_CREDITO",
		TierRiesgo:      "AL_DIA",
		ScoreCredito:    0,
		BandaCredito:    "",
		CreditoDrivers:  nil,
		ScoreRecompra:   0,
		BandaRecompra:   "",
		RecompraDrivers: nil,
	}

	got := analytics.ToClientePulsoContract(c, comp)

	assert.Equal(t, 56, got.ClienteID)
	assert.Equal(t, 0, got.ScoreCredito)
	assert.Empty(t, got.BandaCredito)
	assert.Nil(t, got.CreditoDrivers)
	assert.Equal(t, 0, got.ScoreRecompra)
	assert.Empty(t, got.BandaRecompra)
	assert.Nil(t, got.RecompraDrivers)
}

// TestToClientePulsoContract_NuevosFields verifies that the four new driver/resumen
// fields (CLVDrivers, CreditoResumen, RecompraResumen, CLVResumen) round-trip through
// ToClientePulsoContract from PulsoComputado.
func TestToClientePulsoContract_NuevosFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	cohorteFecha := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	c := domain.HydrateWinbackCandidato(domain.HydrateWinbackCandidatoParams{
		ClienteID:    70,
		Nombre:       "Hernández Muñoz",
		CohorteFecha: cohorteFecha,
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	comp := analytics.PulsoComputado{
		Segmento:        "ACTIVO",
		Score:           55,
		CLVDrivers:      []string{"recompra recurrente esperada", "ticket $12,000"},
		CreditoResumen:  "Buen pagador: paga cada ~28 días, 92% a tiempo.",
		RecompraResumen: "Muy probable que recompre — compró hace 2 meses.",
		CLVResumen:      "Valor estimado $8.5k en 12m por su recompra y ticket de $12,000.",
	}

	got := analytics.ToClientePulsoContract(c, comp)

	assert.Equal(t, []string{"recompra recurrente esperada", "ticket $12,000"}, got.CLVDrivers)
	assert.Equal(t, "Buen pagador: paga cada ~28 días, 92% a tiempo.", got.CreditoResumen)
	assert.Equal(t, "Muy probable que recompre — compró hace 2 meses.", got.RecompraResumen)
	assert.Equal(t, "Valor estimado $8.5k en 12m por su recompra y ticket de $12,000.", got.CLVResumen)
}

// TestToClientePulsoContract_NuevosFields_NilCLVDrivers verifies that nil CLVDrivers
// round-trips correctly (no aplica case).
func TestToClientePulsoContract_NuevosFields_NilCLVDrivers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	cohorteFecha := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	c := domain.HydrateWinbackCandidato(domain.HydrateWinbackCandidatoParams{
		ClienteID:    71,
		Nombre:       "Sánchez Valdés",
		CohorteFecha: cohorteFecha,
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	comp := analytics.PulsoComputado{
		CLVDrivers:      nil,
		CreditoResumen:  "Sin saldo a crédito — no se evalúa.",
		RecompraResumen: "Sin historial de compras — no se evalúa.",
		CLVResumen:      "Sin historial de compras — no se evalúa.",
	}

	got := analytics.ToClientePulsoContract(c, comp)

	assert.Nil(t, got.CLVDrivers)
	assert.Equal(t, "Sin saldo a crédito — no se evalúa.", got.CreditoResumen)
	assert.Equal(t, "Sin historial de compras — no se evalúa.", got.RecompraResumen)
	assert.Equal(t, "Sin historial de compras — no se evalúa.", got.CLVResumen)
}

// TestToClientePulsoContract_CobranzaRecenciaFromComp verifies that the
// recency-adjusted cobranza metrics (DiasAtrasoProm / PctPagosATiempo) come from
// the computed PulsoComputado, NOT from the entity's materialized values.
func TestToClientePulsoContract_CobranzaRecenciaFromComp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cohorteFecha := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Entity carries the stale historical values (atraso 1, puntualidad 88.5).
	c := domain.HydrateWinbackCandidato(domain.HydrateWinbackCandidatoParams{
		ClienteID:       60,
		Nombre:          "Moroso Recencia",
		CohorteFecha:    cohorteFecha,
		DiasAtrasoProm:  1,
		PctPagosATiempo: decimal.NewFromFloat(88.5),
		CreatedAt:       now,
		UpdatedAt:       now,
	})

	// comp carries the recency-corrected values.
	comp := analytics.PulsoComputado{
		Segmento:        "FRIO",
		EstadoPago:      "MOROSO",
		DiasAtrasoProm:  113,
		PctPagosATiempo: decimal.NewFromFloat(67.6),
	}

	got := analytics.ToClientePulsoContract(c, comp)

	assert.Equal(t, 113, got.DiasAtrasoProm, "must use comp value, not entity historical")
	assert.Truef(t, decimal.NewFromFloat(67.6).Equal(got.PctPagosATiempo),
		"must use comp value, got %s", got.PctPagosATiempo)
}
