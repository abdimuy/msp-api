// export_test.go exposes package-internal symbols to the external test package
// (app_test) for white-box unit testing. This file is compiled ONLY during
// testing; it does not appear in the production binary.
package app

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// ExportComputeSegmentoScore exposes the internal computeSegmentoScore function
// for table-driven tests in the app_test package.
func ExportComputeSegmentoScore(c *domain.WinbackCandidato, now time.Time) (domain.Segmento, domain.ScoreWinback, int, domain.EstadoPago) {
	return computeSegmentoScore(c, now)
}

// ExportDeterministicControl exposes deterministicControl for property tests.
func ExportDeterministicControl(clienteID int) bool {
	return deterministicControl(clienteID)
}

// ExportEstadoPagoFor exposes the internal estadoPagoFor function for
// table-driven tests in the app_test package.
func ExportEstadoPagoFor(saldo decimal.Decimal, fechaUltimoPago, now time.Time) domain.EstadoPago {
	return estadoPagoFor(saldo, fechaUltimoPago, now)
}

// ExportComputeCobranzaTier exposes the internal computeCobranzaTier function
// for table-driven tests in the app_test package.
func ExportComputeCobranzaTier(c *domain.WinbackCandidato, now time.Time) domain.TierRiesgo {
	return computeCobranzaTier(c, now)
}

// ExportAjustarCobranzaRecencia exposes the internal ajustarCobranzaRecencia
// function for table-driven tests in the app_test package.
func ExportAjustarCobranzaRecencia(c *domain.WinbackCandidato, now time.Time) (int, decimal.Decimal) {
	return ajustarCobranzaRecencia(c, now)
}

// ExportBuildCreditoFeatures exposes the internal buildCreditoFeatures function
// for table-driven tests in the app_test package.
func ExportBuildCreditoFeatures(c *domain.WinbackCandidato, now time.Time, pagos90d int) map[string]float64 {
	return buildCreditoFeatures(c, now, pagos90d)
}

// ExportComputeCreditoScore exposes the internal computeCreditoScore function
// for table-driven tests in the app_test package.
func ExportComputeCreditoScore(c *domain.WinbackCandidato, now time.Time, sc Scorecard, pagos90d int) (domain.ScoreCredito, domain.BandaCredito, []string, bool) {
	return computeCreditoScore(c, now, sc, pagos90d)
}

// ExportBuildRecompraFeatures exposes the internal buildRecompraFeatures function
// for table-driven tests in the app_test package.
func ExportBuildRecompraFeatures(c *domain.WinbackCandidato, now time.Time, btyd BTYD) map[string]float64 {
	return buildRecompraFeatures(c, now, btyd)
}

// ExportComputeRecompraScore exposes the internal computeRecompraScore function
// for table-driven tests in the app_test package.
func ExportComputeRecompraScore(c *domain.WinbackCandidato, now time.Time, sc RecompraScorecard, btyd BTYD) (domain.ScoreRecompra, domain.BandaRecompra, []string, bool) {
	return computeRecompraScore(c, now, sc, btyd)
}

// ExportMonthIndex exposes the internal monthIndex helper for grid math tests.
func ExportMonthIndex(t time.Time) int {
	return monthIndex(t)
}

// ExportComputeCLV exposes the internal computeCLV function for table-driven
// tests in the app_test package.
func ExportComputeCLV(c *domain.WinbackCandidato, now time.Time, btyd BTYD, creditSc Scorecard, params CLVParams, pagos90d int) (domain.MontoCLV, domain.BandaCLV, bool) {
	return computeCLV(c, now, btyd, creditSc, params, pagos90d)
}

// ExportComputeCLVConRazones exposes the internal computeCLVConRazones function
// for table-driven tests in the app_test package.
func ExportComputeCLVConRazones(c *domain.WinbackCandidato, now time.Time, btyd BTYD, creditSc Scorecard, params CLVParams, pagos90d int) (domain.MontoCLV, domain.BandaCLV, []string, string, bool) {
	return computeCLVConRazones(c, now, btyd, creditSc, params, pagos90d)
}

// ExportRazonesCredito exposes the internal razonesCredito function for tests.
func ExportRazonesCredito(c *domain.WinbackCandidato, contribs []FeatureContrib) []string {
	fcs := make([]featureContrib, len(contribs))
	for i, fc := range contribs {
		fcs[i] = featureContrib{name: fc.Name, label: fc.Label, valor: fc.Valor, logit: fc.Logit}
	}
	return razonesCredito(c, fcs)
}

// ExportRazonesRecompra exposes the internal razonesRecompra function for tests.
func ExportRazonesRecompra(c *domain.WinbackCandidato, contribs []FeatureContrib) []string {
	fcs := make([]featureContrib, len(contribs))
	for i, fc := range contribs {
		fcs[i] = featureContrib{name: fc.Name, label: fc.Label, valor: fc.Valor, logit: fc.Logit}
	}
	return razonesRecompra(c, fcs)
}

// ExportResumenCredito exposes the internal resumenCredito function for tests.
func ExportResumenCredito(c *domain.WinbackCandidato, now time.Time, banda domain.BandaCredito, score domain.ScoreCredito, aplica bool) string {
	return resumenCredito(c, now, banda, score, aplica)
}

// ExportResumenRecompra exposes the internal resumenRecompra function for tests.
func ExportResumenRecompra(c *domain.WinbackCandidato, now time.Time, banda domain.BandaRecompra, score domain.ScoreRecompra, aplica bool) string {
	return resumenRecompra(c, now, banda, score, aplica)
}

// ExportPesosMiles exposes the internal pesosMiles helper for tests.
func ExportPesosMiles(d decimal.Decimal) string { return pesosMiles(d) }

// ExportPesosCompact exposes the internal pesosCompact helper for tests.
func ExportPesosCompact(d decimal.Decimal) string { return pesosCompact(d) }

// ExportPluralDias exposes the internal pluralDias helper for tests.
func ExportPluralDias(n int) string { return pluralDias(n) }

// ExportPluralMeses exposes the internal pluralMeses helper for tests.
func ExportPluralMeses(n int) string { return pluralMeses(n) }

// ExportPluralAnios exposes the internal pluralAnios helper for tests.
func ExportPluralAnios(n int) string { return pluralAnios(n) }

// ExportBuildNarrativeInput exposes the internal buildNarrativeInput function
// for tests in the app_test package.
func ExportBuildNarrativeInput(c *domain.WinbackCandidato, comp analytics.PulsoComputado, nota string, catalogo []domain.Rasgo) outbound.NarrativeInput {
	return buildNarrativeInput(c, comp, nota, catalogo)
}

// ExportCandidatoYPulso exposes candidatoYPulso for tests.
func (s *Service) ExportCandidatoYPulso(ctx context.Context, clienteID int) (*domain.WinbackCandidato, analytics.PulsoComputado, string, error) {
	return s.candidatoYPulso(ctx, clienteID)
}

// ExportAplicarNarrativa exposes the internal aplicarNarrativa method for
// unit tests in narrativa_read_test.go.
func ExportAplicarNarrativa(ctx context.Context, s *Service, clienteID int, comp *analytics.PulsoComputado) {
	s.aplicarNarrativa(ctx, clienteID, comp)
}

// ExportPercentilEnCohorte exposes the internal percentilEnCohorte function for tests.
func ExportPercentilEnCohorte(valor float64, cohorte []float64) (float64, float64, float64, float64, int) {
	return percentilEnCohorte(valor, cohorte)
}

// FeatureContrib is an exported mirror of featureContrib for test use.
// It allows tests to build feature contributions without importing internal types.
type FeatureContrib struct {
	Name  string
	Label string
	Valor float64
	Logit float64
}
