// Package app — razones.go builds quantified driver bullets and titular
// resúmenes for each customer-intelligence score (crédito, recompra, CLV).
// All user-facing strings are in Spanish; identifiers and comments in English.
//
//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// ─── Money helpers ────────────────────────────────────────────────────────────

// pesosMiles renders a peso amount rounded to whole pesos with thousands
// separators, e.g. 9483.21 -> "$9,483", 19094.5 -> "$19,095".
func pesosMiles(d decimal.Decimal) string {
	// Round half-up to whole pesos.
	n := d.Round(0).IntPart()
	if n < 0 {
		n = -n
	}
	// Build the number string with thousands separators manually.
	s := strconv.FormatInt(n, 10)
	// Insert commas every 3 digits from the right.
	result := make([]byte, 0, len(s)+len(s)/3+1)
	for i, ch := range s {
		rem := len(s) - i
		if i > 0 && rem%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(ch))
	}
	return "$" + string(result)
}

// pesosCompact renders compactly: abs >= 1000 -> "$19.1k" (one decimal),
// else "$950" (whole pesos). e.g. 19094 -> "$19.1k", 950.4 -> "$950".
func pesosCompact(d decimal.Decimal) string {
	abs := d.Abs().InexactFloat64()
	if abs >= 1000 {
		k := abs / 1000.0
		// One decimal, strip trailing zero only when it is ".0" — fmt %.1f handles it.
		return fmt.Sprintf("$%.1fk", k)
	}
	// Whole pesos.
	return fmt.Sprintf("$%.0f", abs)
}

// ─── Driver-selection helper ──────────────────────────────────────────────────

// topContribsByAbs returns the n contributions with the largest absolute logit
// magnitude, sorted descending by |logit|. Phrasing each by the sign of its
// logit yields the correct direction plus any compensating factor.
func topContribsByAbs(contribs []featureContrib, n int) []featureContrib {
	// Copy so we do not mutate the caller's slice.
	sorted := make([]featureContrib, len(contribs))
	copy(sorted, contribs)

	sort.Slice(sorted, func(i, j int) bool {
		return math.Abs(sorted[i].logit) > math.Abs(sorted[j].logit)
	})

	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}

// ─── Crédito reasons ─────────────────────────────────────────────────────────

// razonCreditoTexto builds a quantified Spanish driver bullet for one crédito
// feature contribution. fc.valor is the raw feature value; fc.logit sign gives
// the direction (logit > 0 increases risk).
func razonCreditoTexto(c *domain.WinbackCandidato, fc featureContrib) string {
	switch fc.name {
	case "DIAS_SIN_PAGAR":
		if fc.logit > 0 {
			cad := c.CadenciaDias()
			if cad > 0 {
				return fmt.Sprintf("%d días sin pagar (su ritmo: ~%d)", int(fc.valor), cad)
			}
			return fmt.Sprintf("%d días sin pagar", int(fc.valor))
		}
		return fmt.Sprintf("pagó hace %d días", int(fc.valor))

	case "PAGOS_90D":
		return fmt.Sprintf("%d pagos en los últimos 90 días", int(fc.valor))

	case "PCT_PAGOS_A_TIEMPO_6M":
		return fmt.Sprintf("%.0f%% de pagos a tiempo", fc.valor*100)

	case "CADENCIA_DIAS":
		if fc.logit > 0 {
			return fmt.Sprintf("cobranza espaciada (cada ~%d días)", int(fc.valor))
		}
		return fmt.Sprintf("paga seguido (cada ~%d días)", int(fc.valor))

	case "NUM_PAGOS_TOTAL":
		return fmt.Sprintf("%d pagos en total", int(fc.valor))

	case "ANTIGUEDAD_DIAS":
		meses := int(fc.valor/30.44 + 0.5)
		if meses >= 12 {
			return fmt.Sprintf("cliente desde hace ~%d años", meses/12)
		}
		return fmt.Sprintf("cliente desde hace ~%d meses", meses)

	default:
		return fc.label
	}
}

// razonesCredito maps the top-3 contributions (by absolute logit) through
// razonCreditoTexto. Returns a non-nil slice (len 0..3).
func razonesCredito(c *domain.WinbackCandidato, contribs []featureContrib) []string {
	top := topContribsByAbs(contribs, 3)
	out := make([]string, 0, len(top))
	for _, fc := range top {
		out = append(out, razonCreditoTexto(c, fc))
	}
	return out
}

// ─── Recompra reasons ─────────────────────────────────────────────────────────

// razonRecompraTexto builds a quantified Spanish driver bullet for one recompra
// feature contribution.
func razonRecompraTexto(_ *domain.WinbackCandidato, fc featureContrib) string {
	switch fc.name {
	case "BGBB_EXP_12M":
		return fmt.Sprintf("≈%.1f compras esperadas en 12m", fc.valor)

	case "BGBB_P_ALIVE":
		return fmt.Sprintf("%.0f%% prob. de seguir activo", fc.valor*100)

	case "RECENCIA_MESES":
		return fmt.Sprintf("compró hace %d meses", int(fc.valor))

	case "FRECUENCIA_V":
		return fmt.Sprintf("%d meses con compra", int(fc.valor))

	case "ANTIGUEDAD_MESES":
		return fmt.Sprintf("%d meses de antigüedad", int(fc.valor))

	case "MONETARY_LOG":
		ticket := math.Expm1(fc.valor)
		return "ticket ~" + pesosMiles(decimal.NewFromFloat(ticket))

	case "PCT_PAGOS_A_TIEMPO":
		return fmt.Sprintf("%.0f%% de pagos a tiempo", fc.valor*100)

	case "DIAS_SIN_PAGAR":
		return fmt.Sprintf("%d días sin pagar", int(fc.valor))

	default:
		return fc.label
	}
}

// razonesRecompra maps the top-3 contributions (by absolute logit) through
// razonRecompraTexto. Returns a non-nil slice (len 0..3).
func razonesRecompra(c *domain.WinbackCandidato, contribs []featureContrib) []string {
	top := topContribsByAbs(contribs, 3)
	out := make([]string, 0, len(top))
	for _, fc := range top {
		out = append(out, razonRecompraTexto(c, fc))
	}
	return out
}

// ─── Resumen helpers (titulars) ───────────────────────────────────────────────

// mesesFrase returns a Spanish phrase for a month count:
// 0 -> "este mes", 1 -> "hace 1 mes", n -> "hace N meses".
func mesesFrase(m int) string {
	switch m {
	case 0:
		return "este mes"
	case 1:
		return "hace 1 mes"
	default:
		return fmt.Sprintf("hace %d meses", m)
	}
}

// resumenCredito produces a one-line titular for the crédito score.
// When aplica is false the titular explains why. When aplica is true the
// titular phrases the result in terms of the client's banda.
func resumenCredito(c *domain.WinbackCandidato, now time.Time, banda domain.BandaCredito, _ domain.ScoreCredito, aplica bool) string {
	if !aplica {
		if !c.Saldo().IsPositive() {
			return "Sin saldo a crédito — no se evalúa."
		}
		return "Crédito inactivo — sin pagos recientes para evaluar."
	}

	d := int(daysSince(c.FechaUltimoPago(), now))
	saldo := pesosCompact(c.Saldo())
	cad := c.CadenciaDias()
	pct := c.PctPagosATiempo().InexactFloat64()

	switch banda {
	case domain.BandaCreditoBajo:
		if cad > 0 {
			return fmt.Sprintf("Buen pagador: paga cada ~%d días, %.0f%% a tiempo.", cad, pct)
		}
		return fmt.Sprintf("Buen pagador: %.0f%% de pagos a tiempo.", pct)

	case domain.BandaCreditoMedio:
		return fmt.Sprintf("Riesgo medio: %d días sin pagar, debe %s.", d, saldo)

	case domain.BandaCreditoAlto:
		return fmt.Sprintf("Riesgo alto: %d días sin pagar y debe %s.", d, saldo)

	case domain.BandaCreditoCritico:
		return fmt.Sprintf("Riesgo crítico: %d días sin pagar y debe %s.", d, saldo)

	default:
		return fmt.Sprintf("Riesgo crítico: %d días sin pagar y debe %s.", d, saldo)
	}
}

// resumenRecompra produces a one-line titular for the recompra score.
func resumenRecompra(c *domain.WinbackCandidato, now time.Time, banda domain.BandaRecompra, _ domain.ScoreRecompra, aplica bool) string {
	if !aplica {
		return "Sin historial de compras — no se evalúa."
	}

	recenciaMeses := monthIndex(now) - monthIndex(c.FechaUltimaVenta())
	if recenciaMeses < 0 {
		recenciaMeses = 0
	}

	switch banda {
	case domain.BandaRecompraAlta:
		return fmt.Sprintf("Muy probable que recompre — compró %s.", mesesFrase(recenciaMeses))

	case domain.BandaRecompraMedia:
		return fmt.Sprintf("Recompra moderada — compró %s.", mesesFrase(recenciaMeses))

	case domain.BandaRecompraBaja:
		if recenciaMeses >= 12 {
			return fmt.Sprintf("Poco probable — no compra hace %d meses.", recenciaMeses)
		}
		return fmt.Sprintf("Poco probable que recompre — compró %s.", mesesFrase(recenciaMeses))

	default:
		if recenciaMeses >= 12 {
			return fmt.Sprintf("Poco probable — no compra hace %d meses.", recenciaMeses)
		}
		return fmt.Sprintf("Poco probable que recompre — compró %s.", mesesFrase(recenciaMeses))
	}
}
