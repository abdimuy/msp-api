// Package analyticshttp — dto_mapper.go contains entity→DTO mapping functions
// for the analytics HTTP transport layer (gate 08 compliance: mappers in a
// dedicated file, not inline in handlers).
//
//nolint:misspell // analytics vocabulary is Spanish per project convention.
package analyticshttp

import (
	"github.com/abdimuy/msp-api/internal/analytics"
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
		Etiqueta:          etiquetaFor(item.Segmento, item.EstadoPago),
		Resumen:           resumenFor(item),
		Tier:              tierFor(item),
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

// ─── Cartera mappers ──────────────────────────────────────────────────────────

// toSaludCarteraDTO maps a SaludCarteraContract to its wire DTO.
// Money fields use moneyScale (2 dp); ratio fields use rateScale (4 dp).
func toSaludCarteraDTO(c analytics.SaludCarteraContract) SaludCarteraDTO {
	return SaludCarteraDTO{
		SaldoTotal:       c.SaldoTotal.StringFixed(moneyScale),
		SaldoMoroso:      c.SaldoMoroso.StringFixed(moneyScale),
		PAR:              c.PAR.StringFixed(rateScale),
		CEIRate:          c.CEIRate.StringFixed(rateScale),
		ImporteColectado: c.ImporteColectado.StringFixed(moneyScale),
		CuentasTotal:     c.CuentasTotal,
		CuentasEnMora:    c.CuentasEnMora,
		MargenRealProxy:  c.MargenRealProxy.StringFixed(moneyScale),
	}
}

// toAgingBucketDTO maps an AgingBucketContract to its wire DTO.
func toAgingBucketDTO(c analytics.AgingBucketContract) AgingBucketDTO {
	return AgingBucketDTO{
		Bucket:   c.Bucket,
		Saldo:    c.Saldo.StringFixed(moneyScale),
		Conteo:   c.Conteo,
		PctSaldo: c.PctSaldo.StringFixed(rateScale),
	}
}

// toCosechaDTO maps a CosechaContract to its wire DTO.
func toCosechaDTO(c analytics.CosechaContract) CosechaDTO {
	return CosechaDTO{
		CohortMonth: c.CohortMonth,
		AgeMonths:   c.AgeMonths,
		Saldo:       c.Saldo.StringFixed(moneyScale),
		Conteo:      c.Conteo,
	}
}

// toCobradorPerformanceDTO maps a CobradorPerformanceContract to its wire DTO.
func toCobradorPerformanceDTO(c analytics.CobradorPerformanceContract) CobradorPerformanceDTO {
	return CobradorPerformanceDTO{
		CobradorID:       c.CobradorID,
		CobradorNombre:   c.CobradorNombre,
		ZonaClienteID:    c.ZonaClienteID,
		CEI:              c.CEI.StringFixed(rateScale),
		PAR:              c.PAR.StringFixed(rateScale),
		PctCorriente:     c.PctCorriente.StringFixed(rateScale),
		SaldoTotal:       c.SaldoTotal.StringFixed(moneyScale),
		SaldoMoroso:      c.SaldoMoroso.StringFixed(moneyScale),
		CuentasTotal:     c.CuentasTotal,
		ImporteColectado: c.ImporteColectado.StringFixed(moneyScale),
	}
}

// toCuentaRiesgoDTO maps a CuentaRiesgoContract to its wire DTO.
// Date fields use formatTime (RFC3339Nano UTC; empty string for zero times).
func toCuentaRiesgoDTO(c analytics.CuentaRiesgoContract) CuentaRiesgoDTO {
	return CuentaRiesgoDTO{
		ClienteID:       c.ClienteID,
		Nombre:          c.Nombre,
		Zona:            c.Zona,
		TierRiesgo:      c.TierRiesgo,
		Segmento:        c.Segmento,
		EstadoPago:      c.EstadoPago,
		Saldo:           c.Saldo.StringFixed(moneyScale),
		DiasAtrasoProm:  c.DiasAtrasoProm,
		PctPagosATiempo: c.PctPagosATiempo.StringFixed(moneyScale),
		CadenciaDias:    c.CadenciaDias,
		FechaUltimoPago: formatTime(c.FechaUltimoPago),
		FechaProxPago:   formatTime(c.FechaProxPago),
	}
}

// toRollRateDTO maps a RollRateContract to its wire DTO.
// Date fields use formatTime (RFC3339Nano UTC; empty string when Disponible=false).
func toRollRateDTO(c analytics.RollRateContract) RollRateDTO {
	return RollRateDTO{
		Disponible:         c.Disponible,
		RollRate:           c.RollRate,
		FechaCorteAnterior: formatTime(c.FechaCorteAnterior),
		FechaCorteReciente: formatTime(c.FechaCorteReciente),
	}
}
