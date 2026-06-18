// Package app — btyd.go implements a pure, dependency-free BTYD (Buy-Till-You-Die)
// math engine: the BG/BB (Beta-Geometric/Beta-Binomial) discrete-time model for
// repeat-purchase frequency plus the Gamma-Gamma model for monetary value. The
// closed forms are ported verbatim from the Python `lifetimes` library
// (beta_geo_beta_binom_fitter / gamma_gamma_fitter) and validated against
// committed reference fixtures (btyd_fixtures.json, gg_fixtures.json) within 1e-6.
//
// The BG/BB shape parameters (alpha, beta, gamma, delta) are fit offline and live
// in btyd_params.json; the Gamma-Gamma parameters (p, q, v) live in clv_params.json.
// Both are embedded at compile time. This engine performs no I/O and holds no
// mutable state — BTYD is an immutable value object constructed once via LoadBTYD.
//
//nolint:misspell // Spanish field names per project convention.
package app

import (
	_ "embed"
	"encoding/json"
	"math"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

//go:embed btyd_params.json
var embeddedBTYDParamsJSON []byte

//go:embed clv_params.json
var embeddedCLVParamsJSON []byte

// ─── JSON schema ──────────────────────────────────────────────────────────────

// btydParamsJSON is the subset of btyd_params.json this engine consumes. Only the
// bgbb block is needed; other fields (version, fit metadata) are ignored.
type btydParamsJSON struct {
	BGBB bgbbJSON `json:"bgbb"`
}

// bgbbJSON holds the four BG/BB shape parameters fit by the offline harness.
type bgbbJSON struct {
	Alpha float64 `json:"alpha"`
	Beta  float64 `json:"beta"`
	Gamma float64 `json:"gamma"`
	Delta float64 `json:"delta"`
}

// clvParamsJSON is the subset of clv_params.json this engine consumes: the
// Gamma-Gamma parameters plus DET horizon/discount defaults.
type clvParamsJSON struct {
	GammaGamma      gammaGammaJSON `json:"gamma_gamma"`
	HorizonMonths   int            `json:"horizon_months"`
	MonthlyDiscount float64        `json:"monthly_discount"`
}

// gammaGammaJSON holds the three Gamma-Gamma parameters fit by the offline harness.
type gammaGammaJSON struct {
	P float64 `json:"p"`
	Q float64 `json:"q"`
	V float64 `json:"v"`
}

// ─── BTYD value object ──────────────────────────────────────────────────────────

// BTYD is an immutable value object holding the parsed BG/BB and Gamma-Gamma
// parameters and exposing the closed-form predictive quantities. Construct it once
// at startup via LoadBTYD (uses the embedded JSONs) or ParseBTYD (accepts raw
// bytes, useful in tests).
type BTYD struct {
	alpha float64
	beta  float64
	gamma float64
	delta float64

	p float64
	q float64
	v float64

	// horizonMonths / monthlyDiscount are the DET defaults carried from
	// clv_params.json. They are not used by the public methods directly (DET
	// takes them as arguments) but are exposed via accessors for the CLV layer.
	horizonMonths   int
	monthlyDiscount float64

	loaded bool
}

// LoadBTYD constructs a BTYD from the compile-time-embedded btyd_params.json and
// clv_params.json. Returns domain.ErrBTYDParamsInvalido if either embedded blob
// fails validation.
func LoadBTYD() (BTYD, error) {
	return ParseBTYD(embeddedBTYDParamsJSON, embeddedCLVParamsJSON)
}

// ParseBTYD constructs a BTYD from raw JSON bytes for the BG/BB params and the CLV
// (Gamma-Gamma) params. Validates that every shape parameter is finite and > 0.
// Returns domain.ErrBTYDParamsInvalido on any structural error so callers can use
// errors.Is for typed handling.
func ParseBTYD(bgbbJSON, clvJSON []byte) (BTYD, error) {
	var bp btydParamsJSON
	if err := json.Unmarshal(bgbbJSON, &bp); err != nil {
		return BTYD{}, domain.ErrBTYDParamsInvalido
	}
	var cp clvParamsJSON
	if err := json.Unmarshal(clvJSON, &cp); err != nil {
		return BTYD{}, domain.ErrBTYDParamsInvalido
	}

	b := BTYD{
		alpha:           bp.BGBB.Alpha,
		beta:            bp.BGBB.Beta,
		gamma:           bp.BGBB.Gamma,
		delta:           bp.BGBB.Delta,
		p:               cp.GammaGamma.P,
		q:               cp.GammaGamma.Q,
		v:               cp.GammaGamma.V,
		horizonMonths:   cp.HorizonMonths,
		monthlyDiscount: cp.MonthlyDiscount,
		loaded:          true,
	}
	if err := b.validate(); err != nil {
		return BTYD{}, err
	}
	return b, nil
}

// validate checks that every shape parameter is finite and strictly positive.
func (b BTYD) validate() error {
	for _, v := range []float64{b.alpha, b.beta, b.gamma, b.delta, b.p, b.q, b.v} {
		if !finitePositive(v) {
			return domain.ErrBTYDParamsInvalido
		}
	}
	return nil
}

// finitePositive reports whether v is a finite, strictly positive float.
func finitePositive(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0
}

// Loaded reports whether the engine was successfully parsed and is ready to use.
// A zero-value BTYD{} (e.g. returned on load failure) returns false.
func (b BTYD) Loaded() bool { return b.loaded }

// HorizonMonths returns the default CLV horizon (months) carried from clv_params.json.
func (b BTYD) HorizonMonths() int { return b.horizonMonths }

// MonthlyDiscount returns the default monthly discount rate carried from clv_params.json.
func (b BTYD) MonthlyDiscount() float64 { return b.monthlyDiscount }

// ─── log/Beta helpers ───────────────────────────────────────────────────────────

// lbeta returns log B(a, b) = lgamma(a) + lgamma(b) - lgamma(a+b). All arguments
// in this engine are positive, so the lgamma signs are all +1 and can be ignored.
func lbeta(a, b float64) float64 {
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	lab, _ := math.Lgamma(a + b)
	return la + lb - lab
}

// betaf returns the Beta function B(a, b) = exp(lbeta(a, b)).
func betaf(a, b float64) float64 {
	return math.Exp(lbeta(a, b))
}

// logAddExp returns log(exp(a) + exp(b)) computed in a numerically stable way.
func logAddExp(a, b float64) float64 {
	hi := math.Max(a, b)
	if math.IsInf(hi, -1) {
		// Both -Inf: log(0+0) = -Inf. Avoids -Inf - (-Inf) = NaN below.
		return hi
	}
	return hi + math.Log1p(math.Exp(-math.Abs(a-b)))
}

// ─── BG/BB closed forms ─────────────────────────────────────────────────────────

// loglikelihood returns the BG/BB log-likelihood contribution for a single
// customer with frequency x, recency tx, and n observation periods. Ported from
// lifetimes BetaGeoBetaBinomFitter._loglikelihood.
func (b BTYD) loglikelihood(x, tx, n int) float64 {
	fx, ftx, fn := float64(x), float64(tx), float64(n)

	betalnAB := lbeta(b.alpha, b.beta)
	betalnGD := lbeta(b.gamma, b.delta)

	a := lbeta(b.alpha+fx, b.beta+fn-fx) - betalnAB +
		lbeta(b.gamma, b.delta+fn) - betalnGD

	sum := 1e-15
	recencyT := n - tx - 1 // integer; if < 0 the loop body never runs
	for j := 0; j <= recencyT; j++ {
		fj := float64(j)
		sum += betaf(b.alpha+fx, b.beta+ftx-fx+fj) * betaf(b.gamma+1, b.delta+ftx+fj)
	}
	bb := math.Log(sum) - betalnGD - betalnAB

	return logAddExp(a, bb)
}

// PAlive returns the probability that a customer with frequency x, recency tx, and
// n observation periods is still "alive" (will transact again). Ported from
// lifetimes conditional_probability_alive with m = 0.
//
// At large n the true probability can decay toward 0; that is correct model
// behavior and is not forced up. The result is clamped to [0, 1] only to absorb
// floating-point error.
func (b BTYD) PAlive(x, tx, n int) float64 {
	fx, fn := float64(x), float64(n)

	p1 := lbeta(b.alpha+fx, b.beta+fn-fx) - lbeta(b.alpha, b.beta)
	p2 := lbeta(b.gamma, b.delta+fn) - lbeta(b.gamma, b.delta) // m = 0
	p3 := b.loglikelihood(x, tx, n)

	return clamp01Finite(math.Exp(p1 + p2 - p3))
}

// ExpectedPurchases returns E[X(n, n+m)], the expected number of transactions a
// customer with frequency x, recency tx, and n observation periods will make in
// the next m periods. Ported from lifetimes
// conditional_expected_number_of_purchases_up_to_time.
//
// The result is guaranteed ≥ 0. For m ≤ 0 it returns 0 (E[X(0)] = 0 by definition;
// the formula already yields p4-p5 = 0 at m = 0).
func (b BTYD) ExpectedPurchases(m, x, tx, n int) float64 {
	if m <= 0 {
		return 0
	}
	// gamma == 1 would divide by zero in p3; our gamma ≈ 0.046, but guard
	// defensively so the public method never returns Inf/NaN.
	if b.gamma == 1 {
		return 0
	}

	fx, fn, fm := float64(x), float64(n), float64(m)

	ll := b.loglikelihood(x, tx, n)
	p1 := math.Exp(-ll)
	p2 := math.Exp(lbeta(b.alpha+fx+1, b.beta+fn-fx) - lbeta(b.alpha, b.beta))

	lg1, _ := math.Lgamma(b.gamma + b.delta)
	lg2, _ := math.Lgamma(1 + b.delta)
	p3 := b.delta / (b.gamma - 1) * math.Exp(lg1-lg2)

	lg4a, _ := math.Lgamma(1 + b.delta + fn)
	lg4b, _ := math.Lgamma(b.gamma + b.delta + fn)
	p4 := math.Exp(lg4a - lg4b)

	lg5a, _ := math.Lgamma(1 + b.delta + fn + fm)
	lg5b, _ := math.Lgamma(b.gamma + b.delta + fn + fm)
	p5 := math.Exp(lg5a - lg5b)

	res := p1 * p2 * p3 * (p4 - p5)
	if !isFinite(res) || res < 0 {
		return 0
	}
	return res
}

// DET returns the discounted expected number of transactions over horizonMonths
// periods, discounting each marginal period's expected transactions at
// monthlyDiscount. Matches clv_params.json["det_definition"]:
//
//	DET = Σ_{m=1..H} (E[X(m)] - E[X(m-1)]) / (1+d)^m
//
// The result is guaranteed ≥ 0.
func (b BTYD) DET(x, tx, n, horizonMonths int, monthlyDiscount float64) float64 {
	if horizonMonths <= 0 {
		return 0
	}
	det := 0.0
	prev := 0.0 // E[X(0)] = 0
	for m := 1; m <= horizonMonths; m++ {
		cur := b.ExpectedPurchases(m, x, tx, n)
		marginal := cur - prev
		det += marginal / math.Pow(1+monthlyDiscount, float64(m))
		prev = cur
	}
	if !isFinite(det) || det < 0 {
		return 0
	}
	return det
}

// ExpectedAvgProfit returns the Gamma-Gamma conditional expected average profit
// (mean transaction value) for a customer with the given repeat frequency and
// observed mean monetary value. Ported from lifetimes
// conditional_expected_average_profit (Fader/Hardie note 025 eq. 5).
//
// For frequency == 0 the individual weight is 0 and the population mean is
// returned. The result is guaranteed ≥ 0.
func (b BTYD) ExpectedAvgProfit(frequency int, monetary float64) float64 {
	ff := float64(frequency)

	denom := b.p*ff + b.q - 1
	var individualWeight float64
	if denom != 0 {
		individualWeight = b.p * ff / denom
	}
	populationMean := b.v * b.p / (b.q - 1)

	res := (1-individualWeight)*populationMean + individualWeight*monetary
	if !isFinite(res) || res < 0 {
		return 0
	}
	return res
}

// ─── tiny numeric helpers ───────────────────────────────────────────────────────

// clamp01Finite clamps v into [0, 1], mapping non-finite values to 0. (Distinct
// from the scoring.go clamp01, which assumes a finite input.)
func clamp01Finite(v float64) float64 {
	if !isFinite(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// isFinite reports whether v is neither NaN nor ±Inf.
func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
