//nolint:misspell // analytics vocabulary is Spanish per project convention.
package analyticshttp

import (
	"fmt"

	"github.com/shopspring/decimal"

	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// Tier thresholds — "intrinsic client quality" independent of winback score.
//
// Monetary high:  >= 50,000 MXN lifetime purchases.
// Frecuencia high: >= 4 distinct purchases.
// A MOROSO client is capped at tier C (solvency risk overrides value).
const (
	tierMonetarioAltoMXN = 50_000
	tierFrecuenciaAlta   = 4
)

// etiquetaFor returns a 2-3 word display label for the client type.
// EstadoPago MOROSO and ATRASADO override segment labels.
func etiquetaFor(seg domain.Segmento, ep domain.EstadoPago) string {
	switch ep {
	case domain.EstadoPagoMoroso:
		return "Moroso"
	case domain.EstadoPagoAtrasado:
		return "Atrasado en pagos"
	case domain.EstadoPagoSinCredito, domain.EstadoPagoLiquidado, domain.EstadoPagoAlCorriente:
		// Fall through to segment-based label.
	}
	switch seg {
	case domain.SegmentoDormidoValioso:
		return "Valioso dormido"
	case domain.SegmentoLealPorLiquidar:
		return "Leal dormido"
	case domain.SegmentoActivo:
		return "Activo"
	case domain.SegmentoNuevo:
		return "Nuevo"
	case domain.SegmentoFrio:
		return "Frío"
	case domain.SegmentoPerdido:
		return "Perdido"
	default:
		return seg.String()
	}
}

// tierFor returns an intrinsic quality letter (A/B/C/D) based on monetary and
// frecuencia. A MOROSO client is capped at tier C at most.
func tierFor(item analyticsapp.WinbackListItem) string {
	c := item.Candidato
	monetarioAlto := c.Monetary().GreaterThanOrEqual(decimal.NewFromInt(tierMonetarioAltoMXN))
	frecuenciaAlta := c.Frecuencia() >= tierFrecuenciaAlta

	var tier string
	switch {
	case monetarioAlto && frecuenciaAlta:
		tier = "A"
	case monetarioAlto || frecuenciaAlta:
		tier = "B"
	case c.Monetary().GreaterThanOrEqual(decimal.NewFromInt(15_000)) || c.Frecuencia() >= 2:
		tier = "C"
	default:
		tier = "D"
	}

	// MOROSO cap: solvency risk prevents tier A or B from being shown as high quality.
	if item.EstadoPago == domain.EstadoPagoMoroso && (tier == "A" || tier == "B") {
		tier = "C"
	}

	return tier
}

// resumenDormidoValioso composes the summary for the DORMIDO_VALIOSO segment.
// Extracted to keep resumenFor's cyclomatic complexity within the lint budget.
func resumenDormidoValioso(ep domain.EstadoPago, recenciaStr string) string {
	base := fmt.Sprintf("Gran comprador, sin visita hace %s.", recenciaStr)
	if ep == domain.EstadoPagoLiquidado || ep == domain.EstadoPagoSinCredito {
		base += " Buen pagador."
	}
	return base
}

// resumenFor composes a short one-line summary (≤ ~12 words) explaining why
// this client is on the winback list and what action to take.
func resumenFor(item analyticsapp.WinbackListItem) string {
	c := item.Candidato
	ep := item.EstadoPago
	seg := item.Segmento
	recencia := item.RecenciaDias
	porLiquidarPct := c.PorLiquidarPct()
	nbp := c.NextBestProduct()

	// Format recencia as "~N meses" when >= 60 days, else "N días".
	recenciaStr := formatRecencia(recencia)

	switch ep {
	case domain.EstadoPagoMoroso:
		return "Debe y dejó de pagar. No vender a crédito."
	case domain.EstadoPagoAtrasado:
		return fmt.Sprintf("Atrasado. Sin visita hace %s.", recenciaStr)
	case domain.EstadoPagoSinCredito, domain.EstadoPagoLiquidado, domain.EstadoPagoAlCorriente:
		// Fall through to segment-based summary.
	}

	switch seg {
	case domain.SegmentoDormidoValioso:
		return resumenDormidoValioso(ep, recenciaStr)
	case domain.SegmentoLealPorLiquidar:
		if !porLiquidarPct.IsZero() && porLiquidarPct.LessThan(decimal.NewFromInt(20)) {
			return "Ya casi termina de pagar. Listo para reenganche."
		}
		return fmt.Sprintf("Cliente leal, sin visita hace %s.", recenciaStr)
	case domain.SegmentoActivo:
		if nbp != "" {
			return fmt.Sprintf("Activo. Ofrecer: %s.", nbp)
		}
		return "Cliente activo con potencial de compra."
	case domain.SegmentoNuevo:
		return "Primera compra reciente. Consolidar relación."
	case domain.SegmentoFrio:
		return "Bajo valor y distante. Prioridad baja."
	case domain.SegmentoPerdido:
		return fmt.Sprintf("Sin compras hace %s. Recuperación difícil.", recenciaStr)
	default:
		return fmt.Sprintf("Sin actividad hace %s.", recenciaStr)
	}
}

// formatRecencia returns "~N meses" when recenciaDias >= 60, else "N días".
func formatRecencia(dias int) string {
	if dias >= 60 {
		meses := dias / 30
		return fmt.Sprintf("~%d meses", meses)
	}
	return fmt.Sprintf("%d días", dias)
}
