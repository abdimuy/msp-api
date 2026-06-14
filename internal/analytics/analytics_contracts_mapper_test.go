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
