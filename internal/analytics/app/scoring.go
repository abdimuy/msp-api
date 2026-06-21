// Package app — scoring.go contains the R1 heuristic constants and pure
// scoring / segmentation logic.
//
// ALL constants below are R1 tunables — adjust them (and re-run tests) to
// recalibrate the model without touching any business logic.
//
//nolint:misspell // Spanish field names per project convention.
package app

import (
	"fmt"
	"math"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// monthIndex converts a time.Time to a monotonic month index (year*12 + month - 1).
// Returns 0 for zero time (caller is responsible for gating on zero dates).
func monthIndex(t time.Time) int {
	if t.IsZero() {
		return 0
	}
	return t.Year()*12 + int(t.Month()) - 1
}

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

// ─── Recompra / CLV dormancy gates ───────────────────────────────────────────

// clvRecenciaMaxMeses is the maximum months since the last V purchase for a
// client to receive CLV banda ALTO. Clients dormant beyond this threshold have
// a BG/BB P_alive that remains artificially high (the model was fit on 2022-2023
// obs windows where the max observation span was ~24 months; gamma≈0.046 gives
// a very slow modeled churn rate). Empirical audit at 2026-02-20 found:
//   - 28 months dormant → P_alive=0.93, CLV=$2,803 ALTO  (cliente 69230)
//   - 36 months dormant → P_alive=0.82, CLV=$1,380 ALTO  (cliente 1255115)
//   - 52 months dormant → P_alive=0.75, CLV=$818  MEDIO  (cliente 457649)
//
// A 2-3 year dormant client rated ALTO CLV is not operationally defensible.
// This gate hard-caps the CLV banda at MEDIO for clients beyond the threshold,
// keeping the pesos monto (used for ordering) unchanged. The threshold mirrors
// the BG/BB training horizon of 24 months so served scores stay in-distribution.
// Tune this constant if a new model is fit with a longer observation window.
const clvRecenciaMaxMeses = 24

// clvSinRepeatSignalMaxBanda caps the CLV banda at MEDIO when x==0 (the client
// has only a single V purchase month — no repeat signal). With x==0 the BG/BB
// engine falls back to population priors (DET≈1.35, P_alive=1.0) producing
// CLV ALTO from zero individual evidence. Empirical: cliente 3074781, brand new
// 3 days ago, single $5,500 purchase → CLV=$3,932 ALTO — indefensible.
// BAJO is not appropriate either (the client just bought, signal is neutral),
// so MEDIO is the right conservative band until a repeat purchase is observed.
const clvSinRepeatSignalMaxBanda = domain.BandaCLVMedio

// recompraRecenciaMaxMeses is the maximum months since the last V purchase for a
// client to receive recompra banda ALTA. Mirrors clvRecenciaMaxMeses rationale:
// cliente 69230 (28 months dormant) scored recompra=64 ALTA because
// P_alive=0.93 and FRECUENCIA_V=5 outweigh the RECENCIA_MESES=-0.559 term.
// A dormant-beyond-2yr client rated ALTA recompra overrides the field team's
// judgment about who to prioritise for outreach. Gate caps at MEDIA.
const recompraRecenciaMaxMeses = 24

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
// pagos90d is the count of real payments in the trailing 90 days, supplied by
// the caller. It is passed in (not read from c.Pagos90D()) because it must be
// computed LIVE at read time: the materialized column is a rolling-window count
// frozen at refresh time and goes stale as soon as the serving clock moves past
// the last refresh. See outbound.WinbackRepo.ContarPagosRecientes.
//
// The keys MUST match the feature names in scorecard.json. Pure and deterministic.
func buildCreditoFeatures(c *domain.WinbackCandidato, now time.Time, pagos90d int) map[string]float64 {
	antiguedadDias := daysSince(c.FechaPrimerCargo(), now)
	diasSinPagar := daysSince(c.FechaUltimoPago(), now)
	if c.FechaUltimoPago().IsZero() {
		diasSinPagar = antiguedadDias
	}
	return map[string]float64{
		"DIAS_SIN_PAGAR":        diasSinPagar,
		"PAGOS_90D":             float64(pagos90d),
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
func computeCreditoScore(c *domain.WinbackCandidato, now time.Time, sc Scorecard, pagos90d int) (domain.ScoreCredito, domain.BandaCredito, []string, bool) {
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
	features := buildCreditoFeatures(c, now, pagos90d)
	score, banda, contribs := sc.aplicarContribs(features)
	drivers := razonesCredito(c, contribs)
	return score, banda, drivers, true
}

// ─── Repurchase propensity scoring ───────────────────────────────────────────

// buildRecompraFeatures assembles the read-time feature vector for the recompra
// propensity scorecard from the candidate's materialized V-sale and payment facts.
//
// BG/BB monthly grid (month index = year*12 + month - 1; constant offset cancels):
//
//	acqMonth  = monthIndex(FechaPrimerVenta)
//	lastMonth = monthIndex(FechaUltimaVenta)
//	nowMonth  = monthIndex(now)
//	n         = max(0, nowMonth - acqMonth)         — observation periods
//	tx        = clamp(lastMonth - acqMonth, 0, n)   — recency in months
//	x         = max(0, VentasMesesDistintos - 1)    — repeat months (excl. acquisition)
//
// Feature definitions:
//
//	BGBB_EXP_12M      — expected purchases in the next 12 months (BG/BB).
//	BGBB_P_ALIVE      — probability the client is still "alive" (BG/BB).
//	RECENCIA_MESES    — months since last V sale (n − tx).
//	FRECUENCIA_V      — distinct V months beyond acquisition (x).
//	ANTIGUEDAD_MESES  — total observation months (n).
//	MONETARY_LOG      — log1p of average V ticket (mean IMPORTE_NETO).
//	PCT_PAGOS_A_TIEMPO — fraction of payments within cadence + 7d tolerance (0–1).
//	DIAS_SIN_PAGAR    — days since last real payment; falls back to days since
//	                    first cargo when FechaUltimoPago is zero.
//
// The keys MUST match the feature names in recompra_scorecard.json. Pure and deterministic.
// now must be UTC.
func buildRecompraFeatures(c *domain.WinbackCandidato, now time.Time, btyd BTYD) map[string]float64 {
	acqMonth := monthIndex(c.FechaPrimerVenta())
	lastMonth := monthIndex(c.FechaUltimaVenta())
	nowMonth := monthIndex(now)

	n := nowMonth - acqMonth
	if n < 0 {
		n = 0
	}
	tx := lastMonth - acqMonth
	if tx < 0 {
		tx = 0
	}
	if tx > n {
		tx = n
	}
	x := c.VentasMesesDistintos() - 1
	if x < 0 {
		x = 0
	}

	diasSinPagar := daysSince(c.FechaUltimoPago(), now)
	if c.FechaUltimoPago().IsZero() {
		diasSinPagar = daysSince(c.FechaPrimerCargo(), now)
	}

	return map[string]float64{
		"BGBB_EXP_12M":       btyd.ExpectedPurchases(12, x, tx, n),
		"BGBB_P_ALIVE":       btyd.PAlive(x, tx, n),
		"RECENCIA_MESES":     float64(nowMonth - lastMonth),
		"FRECUENCIA_V":       float64(x),
		"ANTIGUEDAD_MESES":   float64(n),
		"MONETARY_LOG":       math.Log1p(c.MonetaryVProm().InexactFloat64()),
		"PCT_PAGOS_A_TIEMPO": c.PctPagosATiempo().InexactFloat64() / 100.0,
		"DIAS_SIN_PAGAR":     diasSinPagar,
	}
}

// computeRecompraScore applies the embedded recompra scorecard to a candidate's
// features. Returns aplica=false (zero score, empty banda, nil drivers) when the
// recompra score does not apply — i.e. any of:
//   - the scorecard failed to load;
//   - the btyd engine failed to load;
//   - the client has no V purchase history (FechaPrimerVenta is zero);
//   - VentasMesesDistintos < 1 (insufficient V purchase data).
//
// The caller renders "no aplica". now must be UTC. Unlike computeCreditoScore,
// this gate does NOT require saldo > 0 — recompra applies to any client with
// ≥1 V sale, including fully-paid and contado clients.
func computeRecompraScore(c *domain.WinbackCandidato, now time.Time, sc RecompraScorecard, btyd BTYD) (domain.ScoreRecompra, domain.BandaRecompra, []string, bool) {
	if !sc.Loaded() || !btyd.Loaded() {
		return domain.ScoreRecompra{}, domain.BandaRecompra(""), nil, false
	}
	if c.FechaPrimerVenta().IsZero() || c.VentasMesesDistintos() < 1 {
		return domain.ScoreRecompra{}, domain.BandaRecompra(""), nil, false
	}
	features := buildRecompraFeatures(c, now, btyd)
	score, banda, contribs := sc.aplicarContribs(features)
	drivers := razonesRecompra(c, contribs)

	// Dormancy gate: cap recompra banda at MEDIA for clients dormant beyond
	// recompraRecenciaMaxMeses. The BG/BB P_alive inflates the score for dormant
	// clients with strong historical frequency (e.g. cliente 69230: 28 months
	// dormant, P_alive=0.93, recompra=64 ALTA). See recompraRecenciaMaxMeses comment.
	nowMonth := monthIndex(now)
	lastMonth := monthIndex(c.FechaUltimaVenta())
	if nowMonth-lastMonth > recompraRecenciaMaxMeses && banda == domain.BandaRecompraAlta {
		banda = domain.BandaRecompraMedia
	}

	return score, banda, drivers, true
}

// ─── Risk-adjusted CLV ───────────────────────────────────────────────────────

// clvVGrid computes the BG/BB monthly grid values (x, tx, n) for a client,
// identical to the grid used in buildRecompraFeatures. Returns (0, 0, 0) when
// FechaPrimerVenta is zero (caller gates before calling).
func clvVGrid(c *domain.WinbackCandidato, now time.Time) (int, int, int) {
	acqMonth := monthIndex(c.FechaPrimerVenta())
	lastMonth := monthIndex(c.FechaUltimaVenta())
	nowMonth := monthIndex(now)

	n := nowMonth - acqMonth
	if n < 0 {
		n = 0
	}
	tx := lastMonth - acqMonth
	if tx < 0 {
		tx = 0
	}
	if tx > n {
		tx = n
	}
	x := c.VentasMesesDistintos() - 1
	if x < 0 {
		x = 0
	}
	return x, tx, n
}

// clvBandaFor maps a CLV pesos amount to a BandaCLV using the configured thresholds.
func clvBandaFor(clvPesos float64, params CLVParams) domain.BandaCLV {
	switch {
	case clvPesos >= params.AltoMinPesos():
		return domain.BandaCLVAlto
	case clvPesos >= params.MedioMinPesos():
		return domain.BandaCLVMedio
	default:
		return domain.BandaCLVBajo
	}
}

// applyCLVBandaGates applies dormancy and no-repeat-signal caps to a CLV banda.
// The pesos monto is always kept unchanged — only the display band is capped.
func applyCLVBandaGates(banda domain.BandaCLV, x, recenciaMeses int) domain.BandaCLV {
	if recenciaMeses > clvRecenciaMaxMeses && banda == domain.BandaCLVAlto {
		banda = domain.BandaCLVMedio
	}
	if x == 0 && banda == domain.BandaCLVAlto {
		banda = clvSinRepeatSignalMaxBanda
	}
	return banda
}

// buildCLVDrivers builds the ordered list of up to 3 quantified CLV driver bullets.
func buildCLVDrivers(det, saldo, pPaga, perdida, eM float64) []string {
	drivers := make([]string, 0, 3)

	// 1. Recompra phrase based on DET (expected transaction count).
	switch {
	case det >= 1:
		drivers = append(drivers, "recompra recurrente esperada")
	case det >= 0.5:
		drivers = append(drivers, "recompra moderada esperada")
	default:
		drivers = append(drivers, "poca recompra esperada")
	}

	// 2. Credit loss driver — only when meaningful.
	if saldo > 0 && pPaga < 1 && perdida > 0 {
		drivers = append(drivers, fmt.Sprintf("riesgo de impago (-%s esperado)", pesosCompact(decimal.NewFromFloat(perdida))))
	}

	// 3. Ticket.
	drivers = append(drivers, "ticket "+pesosMiles(decimal.NewFromFloat(eM)))

	// Cap to 3.
	if len(drivers) > 3 {
		drivers = drivers[:3]
	}
	return drivers
}

// buildCLVResumen builds the one-line titular for a CLV score.
func buildCLVResumen(monto domain.MontoCLV, eM, saldo, clvRaw, clvFinal, perdida float64, horizonMonths int) string {
	switch {
	case saldo > 0 && clvRaw <= 0 && perdida > 0:
		// Loss-dominated zero: the expected credit loss erases future purchase value.
		return fmt.Sprintf(
			"Vale ~$0 ajustado por riesgo: debe %s y su probabilidad de pago es muy baja, así que la pérdida esperada borra el valor de sus compras futuras.",
			pesosCompact(decimal.NewFromFloat(saldo)),
		)
	case clvFinal <= 0:
		return fmt.Sprintf("Valor bajo: ~$0 estimado en %dm.", horizonMonths)
	default:
		return fmt.Sprintf(
			"Valor estimado %s en %dm por su recompra y ticket de %s.",
			pesosCompact(monto.Decimal()),
			horizonMonths,
			pesosMiles(decimal.NewFromFloat(eM)),
		)
	}
}

// computeCLVConRazones computes CLV plus its quantified drivers and titular.
// computeCLV delegates to this and drops the extra returns (keeps its signature).
//
// Returns aplica=false (zero monto, empty banda, nil drivers, empty resumen)
// when there is no purchase history to project from (no V grid), or
// btyd/params not loaded.
//
// CLV_final = margin · E[M] · DET · P(paga) − pérdida_esperada, floored at 0.
//
//	E[M]  = Gamma-Gamma expected ticket (btyd.ExpectedAvgProfit); for x==0 (no
//	        repeat signal) falls back to the observed mean V ticket (MonetaryVProm).
//	DET   = btyd.DET over the configured horizon/discount.
//	P(paga) = credit Score / 100 when the credit score applies; else 1.0.
//	pérdida_esperada = (1 − P(paga)) · saldo_actual · LGD.
func computeCLVConRazones(c *domain.WinbackCandidato, now time.Time, btyd BTYD, creditSc Scorecard, params CLVParams, pagos90d int) (domain.MontoCLV, domain.BandaCLV, []string, string, bool) {
	const noAplicaResumen = "Sin historial de compras — no se evalúa."

	if !btyd.Loaded() || !params.Loaded() {
		return domain.MontoCLV{}, domain.BandaCLV(""), nil, noAplicaResumen, false
	}
	if c.FechaPrimerVenta().IsZero() || c.VentasMesesDistintos() < 1 {
		return domain.MontoCLV{}, domain.BandaCLV(""), nil, noAplicaResumen, false
	}

	x, tx, n := clvVGrid(c, now)

	// E[M]: use Gamma-Gamma shrinkage estimate when repeat signal exists.
	monetary := c.MonetaryVProm().InexactFloat64()
	eM := monetary
	if x >= 1 {
		eM = btyd.ExpectedAvgProfit(x, monetary)
	}

	det := btyd.DET(x, tx, n, params.HorizonMonths(), params.MonthlyDiscount())

	// P(paga): pull from credit score when client has active performing credit.
	cScore, _, _, cAplica := computeCreditoScore(c, now, creditSc, pagos90d)
	pPaga := 1.0
	if cAplica {
		pPaga = float64(cScore.Int()) / 100.0
	}

	saldo := c.Saldo().InexactFloat64()
	perdida := (1 - pPaga) * saldo * params.LGD()

	gross := params.Margin() * eM * det
	clvRaw := gross*pPaga - perdida
	clvFinal := clvRaw
	if clvFinal < 0 {
		clvFinal = 0
	}

	// NewMontoCLV cannot fail here since clvFinal >= 0 is guaranteed above.
	// A panic indicates a programming error (clvFinal < 0 slipped through the floor).
	monto, err := domain.NewMontoCLV(decimal.NewFromFloat(clvFinal).Round(2))
	if err != nil {
		panic("analytics.scoring: CLV monto negative after floor — programming error: " + err.Error())
	}

	banda := clvBandaFor(clvFinal, params)
	nowMonth := monthIndex(now)
	lastMonth := monthIndex(c.FechaUltimaVenta())
	banda = applyCLVBandaGates(banda, x, nowMonth-lastMonth)

	drivers := buildCLVDrivers(det, saldo, pPaga, perdida, eM)
	resumen := buildCLVResumen(monto, eM, saldo, clvRaw, clvFinal, perdida, params.HorizonMonths())

	return monto, banda, drivers, resumen, true
}

// computeCLV computes the risk-adjusted customer lifetime value (pesos) for a
// client as of now. Returns aplica=false (zero monto, empty banda) when there is
// no purchase history to project from (no V grid), or btyd/params not loaded.
//
// CLV_final = margin · E[M] · DET · P(paga) − pérdida_esperada, floored at 0.
//
//	E[M]  = Gamma-Gamma expected ticket (btyd.ExpectedAvgProfit); for x==0 (no
//	        repeat signal) falls back to the observed mean V ticket (MonetaryVProm),
//	        per clv_params monetary_fallback_when_freq0.
//	DET   = btyd.DET over the configured horizon/discount.
//	P(paga) = credit Score / 100 when the credit score applies; else 1.0
//	          (no open credit exposure → no modeled default on future value).
//	pérdida_esperada = (1 − P(paga)) · saldo_actual · LGD.
//
// Bands: monto >= alto_min → ALTO; >= medio_min → MEDIO; else BAJO (pesos cuts).
//
// v1 limitation: a dormant ower whose credit score does not apply gets P(paga)=1.0
// (existing-saldo default risk not subtracted) — mitigated because such clients
// have low DET → low gross CLV, and are flagged separately by the cobranza TierRiesgo.
func computeCLV(c *domain.WinbackCandidato, now time.Time, btyd BTYD, creditSc Scorecard, params CLVParams, pagos90d int) (domain.MontoCLV, domain.BandaCLV, bool) {
	monto, banda, _, _, aplica := computeCLVConRazones(c, now, btyd, creditSc, params, pagos90d)
	return monto, banda, aplica
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

// ajustarCobranzaRecencia corrects the two materialized cobranza metrics that are
// computed at refresh time purely from the gaps between historical payments and
// therefore ignore the currently-open gap (days since the last payment until now):
//   - DiasAtrasoProm — average historical arrears in days.
//   - PctPagosATiempo — share of historical payments made on time (0–100).
//
// A client correctly flagged MOROSO who has not paid in months still shows a tiny
// historical atraso and a high punctuality, which misrepresents the open debt. This
// read-time adjustment folds the open gap into both values, mirroring how
// estadoPagoFor already uses now to derive the solvency signal.
//
// The historical values are returned unchanged when there is nothing to correct:
//   - saldo == 0 (liquidado / sin crédito): a client with no debt is not "behind".
//   - no known payment rhythm (cadencia <= 0, no payments, or no last-payment date):
//     there is no baseline to project the open gap against; EstadoPago already
//     flags such clients as MOROSO.
//
// now must be UTC.
func ajustarCobranzaRecencia(c *domain.WinbackCandidato, now time.Time) (int, decimal.Decimal) {
	diasHist := c.DiasAtrasoProm()
	pctHist := c.PctPagosATiempo()

	// No outstanding balance → not "behind"; keep historical values untouched.
	if !c.Saldo().IsPositive() {
		return diasHist, pctHist
	}

	cadencia := c.CadenciaDias()
	// No known payment rhythm → leave historical values as-is.
	if cadencia <= 0 || c.NumPagos() == 0 || c.FechaUltimoPago().IsZero() {
		return diasHist, pctHist
	}

	diasSinPagar := int(now.Sub(c.FechaUltimoPago()).Hours() / 24)
	if diasSinPagar < 0 {
		diasSinPagar = 0
	}
	// Arrears of the currently-open gap: days overdue beyond one cadence cycle.
	atrasoActual := diasSinPagar - cadencia
	if atrasoActual < 0 {
		atrasoActual = 0
	}

	diasAtraso := diasHist
	if atrasoActual > diasAtraso {
		diasAtraso = atrasoActual
	}

	// missed = expected payments skipped within the open gap (integer division).
	missed := atrasoActual / cadencia
	pct := pctHist
	if denom := c.NumPagos() + missed; denom > 0 {
		numPagos := decimal.NewFromInt(int64(c.NumPagos()))
		pct = pctHist.Mul(numPagos).Div(decimal.NewFromInt(int64(denom)))
	}
	return diasAtraso, pct
}
