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
)

// ─── Score weights and caps ───────────────────────────────────────────────────

const (
	// scoreValueCap is the monetary cap used to normalise the value component
	// of the score.  Clients with monetary >= scoreValueCap get value=1.0.
	scoreValueCap = 50_000.0

	// Weight constants (must sum to 1.0).
	wValue   = 0.40
	wProp    = 0.30
	wContact = 0.15
	wPorLiq  = 0.15
)

// umbralValiosoDecimal is the decimal form of the monetary threshold used in
// the segmentation rule. Declared as a package-level var so it is built once.
var umbralValiosoDecimal = decimal.NewFromInt(20_000)

// ─── computeSegmentoScore ─────────────────────────────────────────────────────

// computeSegmentoScore is a pure, deterministic function that computes the
// RFM-derived segment, the 0–100 score, and the recency in days for a
// WinbackCandidato as of now.
//
// Floats are used ONLY for the score arithmetic (an integer [0,100]); all
// money fields remain as decimal.Decimal throughout.
func computeSegmentoScore(c *domain.WinbackCandidato, now time.Time) (domain.Segmento, domain.ScoreWinback, int) {
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

	// ── Score 0–100 ──────────────────────────────────────────────────────────
	score := scoreFor(c, recenciaDias)

	return seg, score, recenciaDias
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

func scoreFor(c *domain.WinbackCandidato, recenciaDias int) domain.ScoreWinback {
	// Value component: clamp to [0, 1].
	valueComp := clamp01(c.Monetary().InexactFloat64() / scoreValueCap)

	// Propensity component: linearly decays from 1 (just lapsed) to 0 (PERDIDO).
	// Clients still active (recencia <= umbralActivoDias) get propensity = 1.
	var propComp float64
	if recenciaDias <= umbralActivoDias {
		propComp = 1.0
	} else {
		propComp = clamp01(1.0 - float64(recenciaDias-umbralActivoDias)/float64(umbralPerdidoDias-umbralActivoDias))
	}

	// Contactable: 1 if phone number is non-empty.
	contactComp := 0.0
	if c.Telefono() != "" {
		contactComp = 1.0
	}

	// PorLiquidar component: percentage / 100, clamped to [0, 1].
	porLiqComp := clamp01(c.PorLiquidarPct().InexactFloat64() / 100.0)

	raw := 100.0 * (wValue*valueComp + wProp*propComp + wContact*contactComp + wPorLiq*porLiqComp)
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
