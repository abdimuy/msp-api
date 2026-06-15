// Package analyticshttp — dto_mapper.go contains entity→DTO mapping functions
// for the analytics HTTP transport layer (gate 08 compliance: mappers in a
// dedicated file, not inline in handlers).
//
//nolint:misspell // analytics vocabulary is Spanish per project convention.
package analyticshttp

import (
	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
)

// toWinbackItemDTO maps a WinbackListItem (app enriched result) to its wire DTO.
func toWinbackItemDTO(item analyticsapp.WinbackListItem) WinbackItemDTO {
	c := item.Candidato
	return WinbackItemDTO{
		ClienteID:         c.ClienteID(),
		Nombre:            c.Nombre(),
		Zona:              c.Zona(),
		Telefono:          c.Telefono(),
		FechaUltimaCompra: formatTime(c.FechaUltimaCompra()),
		RecenciaDias:      item.RecenciaDias,
		Frecuencia:        c.Frecuencia(),
		Monetary:          c.Monetary().StringFixed(moneyScale),
		Saldo:             c.Saldo().StringFixed(moneyScale),
		PorLiquidarPct:    c.PorLiquidarPct().StringFixed(moneyScale),
		NextBestProduct:   c.NextBestProduct(),
		Segmento:          item.Segmento.String(),
		Score:             item.Score.Int(),
		EnControl:         c.EnControl(),
		FechaUltimoPago:   formatTime(c.FechaUltimoPago()),
		EstadoPago:        item.EstadoPago.String(),
	}
}

// fillAttributionOutput copies an AtribucionResult into the attribution response
// output body. Rate fields (TasaTreatment, TasaControl, Uplift) use rateScale
// (4 dp) for precision; count fields are assigned directly.
func fillAttributionOutput(out *AttributionOutput, a analyticsapp.AtribucionResult) {
	out.Body.TreatmentTotal = a.TreatmentTotal
	out.Body.TreatmentConvertidos = a.TreatmentConvertidos
	out.Body.ControlTotal = a.ControlTotal
	out.Body.ControlConvertidos = a.ControlConvertidos
	out.Body.TasaTreatment = a.TasaTreatment.StringFixed(rateScale)
	out.Body.TasaControl = a.TasaControl.StringFixed(rateScale)
	out.Body.Uplift = a.Uplift.StringFixed(rateScale)
}
