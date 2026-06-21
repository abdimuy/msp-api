package analytics

import (
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// ToWinbackCandidatoContract projects the domain entity into the cross-module
// view. Called only from infra/clients of consumer modules — never from this
// module's own service layer.
//
// Segmento and Score are enrichment fields computed at read time by the analytics
// app/HTTP layer and are NOT populated by this projection. They are left as their
// zero values ("" and 0 respectively). A caller that needs them must set them
// after calling this function.
func ToWinbackCandidatoContract(c *domain.WinbackCandidato) WinbackCandidatoContract {
	return WinbackCandidatoContract{
		ClienteID:         c.ClienteID(),
		Nombre:            c.Nombre(),
		Zona:              c.Zona(),
		Telefono:          c.Telefono(),
		FechaUltimaCompra: c.FechaUltimaCompra(),
		Frecuencia:        c.Frecuencia(),
		Monetary:          c.Monetary(),
		Saldo:             c.Saldo(),
		PorLiquidarPct:    c.PorLiquidarPct(),
		NextBestProduct:   c.NextBestProduct(),
		// Segmento and Score are intentionally zero — see function doc.
		EnControl:       c.EnControl(),
		FechaUltimoPago: c.FechaUltimoPago(),
		// EstadoPago intentionally left empty — computed at read time by caller.
	}
}

// ToWinbackCandidatoContracts projects a slice of domain entities into the
// cross-module view. Helper for callers that need list results.
func ToWinbackCandidatoContracts(candidates []*domain.WinbackCandidato) []WinbackCandidatoContract {
	result := make([]WinbackCandidatoContract, len(candidates))
	for i, c := range candidates {
		result[i] = ToWinbackCandidatoContract(c)
	}
	return result
}

// PulsoComputado carries the read-time computed enrichment values that the
// analytics app scoring layer produces for a single client's pulso. Callers
// assemble this struct from the various compute* functions and pass it to
// ToClientePulsoContract, keeping the mapper signature stable as new
// computed fields are added.
type PulsoComputado struct {
	Segmento        string
	Score           int
	RecenciaDias    int
	EstadoPago      string
	TierRiesgo      string
	DiasAtrasoProm  int
	PctPagosATiempo decimal.Decimal
	ScoreCredito    int
	BandaCredito    string
	CreditoDrivers  []string
	ScoreRecompra   int
	BandaRecompra   string
	RecompraDrivers []string
	MontoCLV        decimal.Decimal
	BandaCLV        string
}

// ToClientePulsoContract projects a WinbackCandidato plus the read-time computed
// pulse values into the cross-module ClientePulsoContract. The computed values
// (segmento, score, recenciaDias, estadoPago, tierRiesgo, scoreCredito, etc.) are
// produced by the analytics app scoring layer and passed in via PulsoComputado —
// this mapper only assembles the flat struct.
func ToClientePulsoContract(c *domain.WinbackCandidato, comp PulsoComputado) ClientePulsoContract {
	return ClientePulsoContract{
		ClienteID:         c.ClienteID(),
		Score:             comp.Score,
		Segmento:          comp.Segmento,
		EstadoPago:        comp.EstadoPago,
		RecenciaDias:      comp.RecenciaDias,
		Frecuencia:        c.Frecuencia(),
		Monetary:          c.Monetary(),
		Saldo:             c.Saldo(),
		PorLiquidarPct:    c.PorLiquidarPct(),
		FechaUltimaCompra: c.FechaUltimaCompra(),
		FechaUltimoPago:   c.FechaUltimoPago(),
		NextBestProduct:   c.NextBestProduct(),
		NumPagos:          c.NumPagos(),
		CadenciaDias:      c.CadenciaDias(),
		DiasAtrasoProm:    comp.DiasAtrasoProm,
		PctPagosATiempo:   comp.PctPagosATiempo,
		FechaProxPago:     c.FechaProxPago(),
		MontoProxPago:     c.MontoProxPago(),
		TierRiesgo:        comp.TierRiesgo,
		ScoreCredito:      comp.ScoreCredito,
		BandaCredito:      comp.BandaCredito,
		CreditoDrivers:    comp.CreditoDrivers,
		ScoreRecompra:     comp.ScoreRecompra,
		BandaRecompra:     comp.BandaRecompra,
		RecompraDrivers:   comp.RecompraDrivers,
		MontoCLV:          comp.MontoCLV,
		BandaCLV:          comp.BandaCLV,
	}
}
