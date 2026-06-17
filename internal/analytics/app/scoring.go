// Package app — scoring.go contains the R1 heuristic constants and pure
// scoring / segmentation logic.
//
// ALL constants below are R1 tunables — adjust them (and re-run tests) to
// recalibrate the model without touching any business logic.
//
//nolint:misspell // Spanish field names per project convention.
package app

import (
	"math"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// ─── Segmentation thresholds ─────────────────────────────────────────────────

const (
	// umbralActivoDias is the recency boundary (days) separating ACTIVE clients
	// from LAPSED ones.  Clients last seen ≤ this value are still active.
	umbralActivoDias = 335

	// umbralPerdidoDias is the recency boundary (days) for the PERDIDO segment.
	// Lapsed clients beyond this threshold are considered lost.
	umbralPerdidoDias = 730

	// frecuenciaLeal is the minimum purchase count for a client to be
	// considered loyal (LEAL_POR_LIQUIDAR segment).
	frecuenciaLeal = 3

	// recenciaMax is the sentinel value used when FechaUltimaCompra is zero
	// (client has no purchase history). Treated as extremely old.
	recenciaMax = 9_999

	// umbralAlCorrienteDias is the maximum days since last payment for a client
	// with outstanding saldo to still be considered AL_CORRIENTE (on time).
	// R1 heuristic: 30 days maps to a monthly payment cycle typical of furniture
	// credit (planes de 12–36 mensualidades).
	umbralAlCorrienteDias = 30

	// umbralAtrasadoDias is the maximum days since last payment before a client
	// is classified as MOROSO instead of ATRASADO.
	// R1 heuristic: 90 days = 3 missed monthly payments.
	umbralAtrasadoDias = 90
)

// ─── Score weights and caps ───────────────────────────────────────────────────

const (
	// scoreValueCap is the monetary cap used to normalise the value component
	// of the score.  Clients with monetary >= scoreValueCap get value=1.0.
	scoreValueCap = 50_000.0

	// Weight constants (must sum to 1.0).
	// wRecencia dominates: winback is primarily about finding lapsed-but-recoverable
	// clients. wValue rewards high lifetime value. wContact gives bonus for contactable
	// clients. wPorLiq adds small signal for outstanding balances worth recovering.
	wRecencia = 0.45
	wValue    = 0.30
	wContact  = 0.10
	wPorLiq   = 0.15
)

// umbralValiosoDecimal is the decimal form of the monetary threshold used in
// the segmentation rule. Declared as a package-level var so it is built once.
var umbralValiosoDecimal = decimal.NewFromInt(20_000)

// ─── Winback recency window constants ────────────────────────────────────────

const (
	// recenciaActivoMaxDias is the upper boundary for "still active" clients.
	// Clients last seen within this window are not winback targets (they are
	// still buying) and receive a low recency score component.
	// R1 heuristic: 90 days ≈ one business quarter.
	recenciaActivoMaxDias = 90

	// recenciaVentanaIniDias is the start of the winback sweet spot.
	// Clients lapsed 180–540 days are the most actionable: not so recent that
	// they are self-serving, not so old that the relationship is cold.
	// R1 heuristic: 180 days ≈ two missed purchase cycles for furniture.
	recenciaVentanaIniDias = 180

	// recenciaVentanaFinDias is the end of the winback sweet spot.
	// R1 heuristic: 540 days ≈ ~18 months; beyond this recovery rate drops sharply.
	recenciaVentanaFinDias = 540

	// recenciaPerdidoDias is the threshold beyond which a client is considered
	// likely lost for winback purposes (score component decays to near zero).
	// R1 heuristic: 730 days = 2 years.
	recenciaPerdidoDias = 730

	// recenciaActivoScore is the recency score component for still-active clients.
	// Low, not zero, so they still appear when explicitly requested.
	recenciaActivoScore = 0.15

	// recenciaPerdidoScore is the recency score component for likely-lost clients.
	// Very low to deprioritise but non-zero so they are not invisible.
	recenciaPerdidoScore = 0.10
)

// ─── Solvency multiplier constants ───────────────────────────────────────────

const (
	// solvenciaAlCorriente / solvenciaLiquidado: full priority — no penalty.
	solvenciaAlCorriente = 1.0
	solvenciaLiquidado   = 1.0

	// solvenciaSinCredito: contado-only client; slightly deprioritised because
	// there is no credit relationship to reactivate.
	solvenciaSinCredito = 0.85

	// solvenciaAtrasado: one to three missed payments; still recoverable but
	// offering more credit without clearing arrears is risky.
	solvenciaAtrasado = 0.6

	// solvenciaMoroso: strongly deprioritise — re-selling on credit to a
	// delinquent client creates more bad debt, not revenue.
	solvenciaMoroso = 0.2
)

// ─── computeSegmentoScore ─────────────────────────────────────────────────────

// computeSegmentoScore is a pure, deterministic function that computes the
// RFM-derived segment, the 0–100 score, the recency in days, and the solvency
// classification for a WinbackCandidato as of now.
//
// Floats are used ONLY for the score arithmetic (an integer [0,100]); all
// money fields remain as decimal.Decimal throughout.
func computeSegmentoScore(c *domain.WinbackCandidato, now time.Time) (domain.Segmento, domain.ScoreWinback, int, domain.EstadoPago) {
	// ── Recencia ──────────────────────────────────────────────────────────────
	recenciaDias := recenciaMax
	if !c.FechaUltimaCompra().IsZero() {
		d := int(now.Sub(c.FechaUltimaCompra()).Hours() / 24)
		if d < 0 {
			d = 0
		}
		recenciaDias = d
	}

	// ── Segmento (first-match wins, in spec order) ────────────────────────────
	seg := segmentoFor(c, recenciaDias)

	// ── EstadoPago (solvency signal) ──────────────────────────────────────────
	ep := estadoPagoFor(c.Saldo(), c.FechaUltimoPago(), now)

	// ── Score 0–100 ──────────────────────────────────────────────────────────
	score := scoreFor(c, recenciaDias, ep)

	return seg, score, recenciaDias, ep
}

func segmentoFor(c *domain.WinbackCandidato, recenciaDias int) domain.Segmento {
	if recenciaDias <= umbralActivoDias {
		if c.Frecuencia() <= 1 {
			return domain.SegmentoNuevo
		}
		return domain.SegmentoActivo
	}
	// Lapsed branch. Lost clients leave first — no further checks needed.
	if recenciaDias > umbralPerdidoDias {
		return domain.SegmentoPerdido
	}
	if c.PorLiquidarPct().IsPositive() && c.Frecuencia() >= frecuenciaLeal {
		return domain.SegmentoLealPorLiquidar
	}
	if c.Monetary().GreaterThanOrEqual(umbralValiosoDecimal) {
		return domain.SegmentoDormidoValioso
	}
	return domain.SegmentoFrio
}

// recenciaWinbackComp maps recency in days to a [0, 1] score component that
// peaks in the dormant-recoverable window and is low for both active and
// likely-lost clients.
//
// Regions:
//
//	[0, activoMax]            → 0.15  (still active, low winback priority)
//	(activoMax, ventanaIni)   → ramp 0.15→1.0 (approaching the sweet spot)
//	[ventanaIni, ventanaFin]  → 1.0   (peak winback window)
//	(ventanaFin, perdido]     → ramp 1.0→0.10 (declining recovery probability)
//	> perdido                 → 0.10  (likely lost)
func recenciaWinbackComp(recenciaDias int) float64 {
	switch {
	case recenciaDias <= recenciaActivoMaxDias:
		return recenciaActivoScore
	case recenciaDias < recenciaVentanaIniDias:
		// Linear ramp 0.15 → 1.0 over (activoMax, ventanaIni).
		t := float64(recenciaDias-recenciaActivoMaxDias) / float64(recenciaVentanaIniDias-recenciaActivoMaxDias)
		return recenciaActivoScore + t*(1.0-recenciaActivoScore)
	case recenciaDias <= recenciaVentanaFinDias:
		return 1.0
	case recenciaDias <= recenciaPerdidoDias:
		// Linear ramp 1.0 → 0.10 over (ventanaFin, perdido].
		t := float64(recenciaDias-recenciaVentanaFinDias) / float64(recenciaPerdidoDias-recenciaVentanaFinDias)
		return 1.0 - t*(1.0-recenciaPerdidoScore)
	default:
		return recenciaPerdidoScore
	}
}

// solvenciaMultiplier returns the [0, 1] multiplier applied to the base score
// based on the client's payment solvency. MOROSO clients are strongly
// deprioritised to avoid re-selling on credit to delinquent payers.
func solvenciaMultiplier(ep domain.EstadoPago) float64 {
	switch ep {
	case domain.EstadoPagoAlCorriente:
		return solvenciaAlCorriente
	case domain.EstadoPagoLiquidado:
		return solvenciaLiquidado
	case domain.EstadoPagoSinCredito:
		return solvenciaSinCredito
	case domain.EstadoPagoAtrasado:
		return solvenciaAtrasado
	case domain.EstadoPagoMoroso:
		return solvenciaMoroso
	default:
		// Unknown EstadoPago: treat conservatively as MOROSO so unknown solvency
		// does not inflate scores.
		return solvenciaMoroso
	}
}

func scoreFor(c *domain.WinbackCandidato, recenciaDias int, ep domain.EstadoPago) domain.ScoreWinback {
	// Value component: clamp to [0, 1].
	valueComp := clamp01(c.Monetary().InexactFloat64() / scoreValueCap)

	// Recency window component: peaked in the dormant-recoverable window.
	recenciaComp := recenciaWinbackComp(recenciaDias)

	// Contactable: 1 if phone number is non-empty.
	contactComp := 0.0
	if c.Telefono() != "" {
		contactComp = 1.0
	}

	// PorLiquidar component: percentage / 100, clamped to [0, 1].
	porLiqComp := clamp01(c.PorLiquidarPct().InexactFloat64() / 100.0)

	base := wRecencia*recenciaComp + wValue*valueComp + wContact*contactComp + wPorLiq*porLiqComp
	raw := 100.0 * base * solvenciaMultiplier(ep)
	n := int(math.Round(raw))
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}

	// NewScoreWinback cannot fail for n in [0,100]; a panic here indicates a
	// programming error in the formula above.
	score, err := domain.NewScoreWinback(n)
	if err != nil {
		panic("analytics.scoring: score out of [0,100] — programming error: " + err.Error())
	}
	return score
}

// clamp01 clamps v to [0.0, 1.0].
func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

// ─── Credit risk scoring ──────────────────────────────────────────────────────

// buildCreditoFeatures assembles the read-time feature vector for the credit
// scorecard v1 (v1-pit-20260617) from the candidate's materialized facts.
//
// Feature definitions:
//
//	DIAS_SIN_PAGAR        — days since last real payment; if FechaUltimoPago is
//	                        zero, falls back to ANTIGUEDAD_DIAS (days since first cargo).
//	PAGOS_90D             — count of real payments in the trailing 90 days.
//	PCT_PAGOS_A_TIEMPO_6M — PctPagosATiempo / 100 (fraction 0–1).
//	CADENCIA_DIAS         — average days between consecutive payments (raw).
//	NUM_PAGOS_TOTAL       — total applied payment count.
//	ANTIGUEDAD_DIAS       — days since first cargo (FechaPrimerCargo); 0 when zero.
//
// The keys MUST match the feature names in scorecard.json. Pure and deterministic.
func buildCreditoFeatures(c *domain.WinbackCandidato, now time.Time) map[string]float64 {
	antiguedadDias := daysSince(c.FechaPrimerCargo(), now)
	diasSinPagar := daysSince(c.FechaUltimoPago(), now)
	if c.FechaUltimoPago().IsZero() {
		diasSinPagar = antiguedadDias
	}
	return map[string]float64{
		"DIAS_SIN_PAGAR":        diasSinPagar,
		"PAGOS_90D":             float64(c.Pagos90D()),
		"PCT_PAGOS_A_TIEMPO_6M": c.PctPagosATiempo().InexactFloat64() / 100.0,
		"CADENCIA_DIAS":         float64(c.CadenciaDias()),
		"NUM_PAGOS_TOTAL":       float64(c.NumPagos()),
		"ANTIGUEDAD_DIAS":       antiguedadDias,
	}
}

// daysSince returns the number of days from t to now, or 0 if t is zero or in the future.
func daysSince(t, now time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	d := now.Sub(t).Hours() / 24
	if d < 0 {
		return 0
	}
	return d
}

// creditoPerformingMaxDias is the maximum days since last payment for the credit
// scorecard to apply. It matches the offline trainer's eligibility window
// (PERFORMING_MAX_DIAS): the model was fit on clients with an active, performing
// credit relationship, so serving it outside that window is out-of-distribution.
// Long-dormant owers have already defaulted and are surfaced by the cobranza
// TierRiesgo (CRITICO), not by this forward-looking default-prediction score.
const creditoPerformingMaxDias = 180

// computeCreditoScore applies the embedded scorecard to a candidate's features.
// Returns aplica=false (zero score, empty banda, nil drivers) when the credit
// score does not apply — i.e. any of:
//   - the scorecard failed to load;
//   - the client has no current credit exposure (saldo == 0: liquidado or contado);
//   - the client is not performing (never paid, or last payment older than
//     creditoPerformingMaxDias) — out of the trained distribution.
//
// The caller renders "no aplica". now must be UTC. This gate keeps the served
// population aligned with the offline training population so the score bands are
// meaningful (see project_credito_scorecard_pit).
func computeCreditoScore(c *domain.WinbackCandidato, now time.Time, sc Scorecard) (domain.ScoreCredito, domain.BandaCredito, []string, bool) {
	if !sc.Loaded() {
		return domain.ScoreCredito{}, domain.BandaCredito(""), nil, false
	}
	// No current outstanding balance → no credit exposure to assess.
	if !c.Saldo().IsPositive() {
		return domain.ScoreCredito{}, domain.BandaCredito(""), nil, false
	}
	// Must be performing (paid recently) to be in the trained distribution.
	if c.FechaUltimoPago().IsZero() || daysSince(c.FechaUltimoPago(), now) > creditoPerformingMaxDias {
		return domain.ScoreCredito{}, domain.BandaCredito(""), nil, false
	}
	features := buildCreditoFeatures(c, now)
	score, banda, drivers := sc.Aplicar(features)
	return score, banda, drivers, true
}

// ─── Cobranza tier constants ──────────────────────────────────────────────────

const (
	// tierCadencia1x is the cadence multiplier for the AL_DIA boundary.
	// Clients whose days-since-payment ≤ 1×cadencia are current.
	tierCadencia1x = 1

	// tierCadencia2x is the cadence multiplier for the VIGILANCIA boundary.
	tierCadencia2x = 2

	// tierCadencia3x is the cadence multiplier for the EN_RIESGO boundary.
	// Beyond 3× → CRITICO.
	tierCadencia3x = 3
)

// computeCobranzaTier computes the TierRiesgo for a client based on their
// personal payment cadence relative to days elapsed since last payment.
//
// Fallback hierarchy:
//  1. saldo == 0 → AL_DIA (no outstanding balance)
//  2. cadenciaDias == 0 (insufficient payment history) → fall back to
//     EstadoPago classification:
//     AL_CORRIENTE / LIQUIDADO / SIN_CREDITO → AL_DIA
//     ATRASADO → VIGILANCIA
//     MOROSO (or unknown) → CRITICO
//  3. cadenciaDias > 0 → compare diasSincePago to N × cadenciaDias:
//     ≤ 1× → AL_DIA
//     ≤ 2× → VIGILANCIA
//     ≤ 3× → EN_RIESGO
//     >  3× → CRITICO
//
// now must be UTC. Zero fechaUltimoPago with saldo > 0 is treated as MOROSO → CRITICO.
func computeCobranzaTier(c *domain.WinbackCandidato, now time.Time) domain.TierRiesgo {
	if !c.Saldo().IsPositive() {
		return domain.TierRiesgoAlDia
	}

	cadencia := c.CadenciaDias()
	if cadencia == 0 {
		// No cadence data — fall back to EstadoPago-based classification.
		ep := estadoPagoFor(c.Saldo(), c.FechaUltimoPago(), now)
		switch ep {
		case domain.EstadoPagoAlCorriente, domain.EstadoPagoLiquidado, domain.EstadoPagoSinCredito:
			return domain.TierRiesgoAlDia
		case domain.EstadoPagoAtrasado:
			return domain.TierRiesgoVigilancia
		case domain.EstadoPagoMoroso:
			return domain.TierRiesgoCritico
		default: // unknown — treat as critical
			return domain.TierRiesgoCritico
		}
	}

	// Cadence available: classify by how many cadence multiples have elapsed.
	if c.FechaUltimoPago().IsZero() {
		return domain.TierRiesgoCritico // has balance but has never paid
	}
	diasSincePago := int(now.Sub(c.FechaUltimoPago()).Hours() / 24)
	if diasSincePago < 0 {
		diasSincePago = 0
	}
	switch {
	case diasSincePago <= cadencia*tierCadencia1x:
		return domain.TierRiesgoAlDia
	case diasSincePago <= cadencia*tierCadencia2x:
		return domain.TierRiesgoVigilancia
	case diasSincePago <= cadencia*tierCadencia3x:
		return domain.TierRiesgoEnRiesgo
	default:
		return domain.TierRiesgoCritico
	}
}

// estadoPagoFor computes the EstadoPago solvency signal from saldo and the
// client's most recent payment date.
//
// Thresholds (R1 heuristics — tune via umbralAlCorrienteDias / umbralAtrasadoDias):
//
//	saldo == 0:
//	  fechaUltimoPago zero → SIN_CREDITO (contado-only, never had open balance)
//	  fechaUltimoPago non-zero → LIQUIDADO (had credit, now fully paid)
//	saldo > 0:
//	  diasSinPagar <= umbralAlCorrienteDias  → AL_CORRIENTE
//	  diasSinPagar <= umbralAtrasadoDias     → ATRASADO
//	  else (including fechaUltimoPago zero)  → MOROSO
//
// now must be UTC; zero fechaUltimoPago is treated as extremely old (MOROSO
// when saldo > 0, SIN_CREDITO when saldo == 0).
func estadoPagoFor(saldo decimal.Decimal, fechaUltimoPago, now time.Time) domain.EstadoPago {
	if !saldo.IsPositive() {
		// Client has no outstanding balance.
		if fechaUltimoPago.IsZero() {
			return domain.EstadoPagoSinCredito
		}
		return domain.EstadoPagoLiquidado
	}
	// saldo > 0: classify by how long since last payment.
	if fechaUltimoPago.IsZero() {
		// Never paid — treat as extremely delinquent.
		return domain.EstadoPagoMoroso
	}
	diasSinPagar := int(now.Sub(fechaUltimoPago).Hours() / 24)
	if diasSinPagar < 0 {
		diasSinPagar = 0
	}
	switch {
	case diasSinPagar <= umbralAlCorrienteDias:
		return domain.EstadoPagoAlCorriente
	case diasSinPagar <= umbralAtrasadoDias:
		return domain.EstadoPagoAtrasado
	default:
		return domain.EstadoPagoMoroso
	}
}
