// Package app — narrativa_input.go maps a candidate + its computed pulso + the
// trait catalog into the generator's fact-anchored input DTO.
//
//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// buildNarrativeInput maps a candidate + its computed pulso + the trait catalog
// into the generator's fact-anchored input DTO.
func buildNarrativeInput(c *domain.WinbackCandidato, comp analytics.PulsoComputado, nota string, catalogo []domain.Rasgo) outbound.NarrativeInput {
	return outbound.NarrativeInput{
		ClienteID: c.ClienteID(),
		Nombre:    c.Nombre(),
		Zona:      c.Zona(),

		Segmento:      comp.Segmento,
		TierRiesgo:    comp.TierRiesgo,
		EstadoPago:    comp.EstadoPago,
		BandaCredito:  comp.BandaCredito,
		ScoreCredito:  comp.ScoreCredito,
		BandaRecompra: comp.BandaRecompra,
		ScoreRecompra: comp.ScoreRecompra,
		BandaCLV:      comp.BandaCLV,

		Saldo:           c.Saldo(),
		Monetary:        c.Monetary(),
		MontoCLV:        comp.MontoCLV,
		Frecuencia:      c.Frecuencia(),
		RecenciaDias:    comp.RecenciaDias,
		CadenciaDias:    c.CadenciaDias(),
		DiasAtrasoProm:  comp.DiasAtrasoProm,
		PctPagosATiempo: comp.PctPagosATiempo,

		CreditoResumen:  comp.CreditoResumen,
		RecompraResumen: comp.RecompraResumen,
		CLVResumen:      comp.CLVResumen,

		CreditoDrivers:  comp.CreditoDrivers,
		RecompraDrivers: comp.RecompraDrivers,
		CLVDrivers:      comp.CLVDrivers,

		Nota:     nota,
		Catalogo: catalogo,
	}
}
