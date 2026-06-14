package analytics

import "github.com/abdimuy/msp-api/internal/analytics/domain"

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
		EnControl: c.EnControl(),
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
