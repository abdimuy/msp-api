//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// TestBuildNarrativeInput_Mapping verifies that buildNarrativeInput correctly
// maps candidate accessors and comp fields into the NarrativeInput DTO. It
// checks a representative sample: a band, a decimal magnitude, a drivers slice,
// and that Catalogo equals CatalogoRasgos.
func TestBuildNarrativeInput_Mapping(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	c, err := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:         123,
		Nombre:            "María López",
		Zona:              "Norte",
		Telefono:          "555-1234",
		FechaUltimaCompra: now.AddDate(0, -3, 0),
		Frecuencia:        7,
		Monetary:          decimal.NewFromInt(35_000),
		Saldo:             decimal.NewFromInt(5_000),
		PorLiquidarPct:    decimal.NewFromFloat(14.3),
		CohorteFecha:      now.AddDate(-2, 0, 0),
		Now:               now,
		CadenciaDias:      30,
		DiasAtrasoProm:    5,
		PctPagosATiempo:   decimal.NewFromFloat(85.5),
	})
	require.NoError(t, err)

	comp := analytics.PulsoComputado{
		Segmento:        "FRIO",
		Score:           55,
		RecenciaDias:    90,
		EstadoPago:      "AL_DIA",
		TierRiesgo:      "BAJO",
		DiasAtrasoProm:  5,
		PctPagosATiempo: decimal.NewFromFloat(85.5),
		ScoreCredito:    72,
		BandaCredito:    "BAJO_RIESGO",
		CreditoDrivers:  []string{"Paga a tiempo", "Saldo bajo"},
		ScoreRecompra:   64,
		BandaRecompra:   "PROBABLE",
		RecompraDrivers: []string{"Frecuencia alta"},
		MontoCLV:        decimal.NewFromInt(80_000),
		BandaCLV:        "ALTO",
		CLVDrivers:      []string{"Valor histórico alto"},
		CreditoResumen:  "Buen pagador",
		RecompraResumen: "Probable recompra",
		CLVResumen:      "CLV alto",
	}

	in := app.ExportBuildNarrativeInput(c, comp, "", app.CatalogoRasgos)

	// Identity fields from candidate
	assert.Equal(t, 123, in.ClienteID)
	assert.Equal(t, "María López", in.Nombre)
	assert.Equal(t, "Norte", in.Zona)

	// Band from comp (representative band check)
	assert.Equal(t, "BAJO_RIESGO", in.BandaCredito)
	assert.Equal(t, "PROBABLE", in.BandaRecompra)
	assert.Equal(t, "ALTO", in.BandaCLV)
	assert.Equal(t, "FRIO", in.Segmento)
	assert.Equal(t, "BAJO", in.TierRiesgo)
	assert.Equal(t, "AL_DIA", in.EstadoPago)

	// Scores from comp
	assert.Equal(t, 72, in.ScoreCredito)
	assert.Equal(t, 64, in.ScoreRecompra)

	// Decimal magnitudes: Monetary from candidato, MontoCLV from comp
	assert.True(t, decimal.NewFromInt(35_000).Equal(in.Monetary), "Monetary mismatch")
	assert.True(t, decimal.NewFromInt(80_000).Equal(in.MontoCLV), "MontoCLV mismatch")
	assert.True(t, decimal.NewFromInt(5_000).Equal(in.Saldo), "Saldo mismatch")
	assert.True(t, decimal.NewFromFloat(85.5).Equal(in.PctPagosATiempo), "PctPagosATiempo mismatch")

	// Integer fields from candidato and comp
	assert.Equal(t, 7, in.Frecuencia)
	assert.Equal(t, 90, in.RecenciaDias)
	assert.Equal(t, 30, in.CadenciaDias)
	assert.Equal(t, 5, in.DiasAtrasoProm)

	// Drivers slice (representative check)
	assert.Equal(t, []string{"Paga a tiempo", "Saldo bajo"}, in.CreditoDrivers)
	assert.Equal(t, []string{"Frecuencia alta"}, in.RecompraDrivers)
	assert.Equal(t, []string{"Valor histórico alto"}, in.CLVDrivers)

	// Titulars from comp
	assert.Equal(t, "Buen pagador", in.CreditoResumen)
	assert.Equal(t, "Probable recompra", in.RecompraResumen)
	assert.Equal(t, "CLV alto", in.CLVResumen)

	// Catalogo must equal CatalogoRasgos exactly
	assert.Equal(t, app.CatalogoRasgos, in.Catalogo,
		"Catalogo must be CatalogoRasgos unchanged")
}
