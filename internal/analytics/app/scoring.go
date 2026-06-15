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
