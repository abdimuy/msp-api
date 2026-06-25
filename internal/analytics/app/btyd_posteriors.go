// Package app — btyd_posteriors.go adds a Bayesian uncertainty layer on top of
// the BG/BB closed-form engine in btyd.go. It draws Monte Carlo samples from the
// parameter posteriors and returns credible intervals (p5 / p50 / p95) around
// PAlive, expected repeat purchases in 12 months, CLV, and days to next purchase.
//
// The computation is pure: no I/O, no time.Now(), no mutable global state.
// The RNG is seeded deterministically by a FNV-1a hash of the sanitized inputs, so
// two calls with identical PosteriorInput always return identical Predicciones.
//
//nolint:misspell // Spanish field names per project convention.
package app

import (
	"hash/fnv"
	"math"
	"math/rand" //nolint:gosec // deterministic simulation, not cryptography
	"sort"
)

// ─── value objects ───────────────────────────────────────────────────────────────

// IntervaloEstimado is a point estimate with its credible interval [Lo, Hi].
type IntervaloEstimado struct {
	Punto float64 // posterior median (p50)
	Lo    float64 // p5 credible bound
	Hi    float64 // p95 credible bound
}

// Predicciones groups all Bayesian posterior predictions for a single customer.
type Predicciones struct {
	Disponible          bool              // false when N<=0 (cannot be computed)
	PAlive              IntervaloEstimado // probability of still being active, in [0,1]
	ComprasEsperadas12m IntervaloEstimado // expected repeat purchases next 12 months, ≥0
	CLV                 IntervaloEstimado // lifetime value in pesos
	ProximaCompraDias   IntervaloEstimado // estimated days until next purchase, ≥0
	Draws               int               // number of Monte Carlo samples used
}

// PosteriorInput are the customer data and configuration for sampling.
type PosteriorInput struct {
	X, Tx, N        int     // repeat frequency, recency (month-index of last purchase), observation months
	Frequency       int     // for Gamma-Gamma model (typically == X)
	Monetary        float64 // observed mean transaction value in pesos
	Draws           int     // number of Monte Carlo draws; <=0 → 2000
	Margin          float64 // gross margin fraction for CLV (e.g. 0.528)
	PPaga           float64 // payment probability for CLV; <=0 → 1.0
	PerdidaEsperada float64 // expected credit loss in pesos deducted from CLV; default 0
	HorizonCLV      int     // CLV forecast horizon in months; <=0 → b.HorizonMonths()
	Discount        float64 // monthly discount rate; <=0 → b.MonthlyDiscount()
}

// ─── Posteriors ──────────────────────────────────────────────────────────────────

// Posteriors draws Monte Carlo samples from the BG/BB posterior to produce
// credible intervals (p5/p95) for four customer metrics. The RNG is seeded by a
// FNV-1a hash of the sanitized inputs; two calls with the same PosteriorInput
// always return the same Predicciones (fully deterministic).
//
// Returns Predicciones{Disponible: false} (zero-value) when N <= 0.
func (b BTYD) Posteriors(in PosteriorInput) Predicciones {
	if in.N <= 0 {
		return Predicciones{}
	}

	// Apply defaults.
	draws := in.Draws
	if draws <= 0 {
		draws = 2000
	}
	horizonCLV := in.HorizonCLV
	if horizonCLV <= 0 {
		horizonCLV = b.HorizonMonths()
	}
	discount := in.Discount
	if discount <= 0 {
		discount = b.MonthlyDiscount()
	}
	pPaga := in.PPaga
	if pPaga <= 0 {
		pPaga = 1.0
	}

	// Defensive clamping — prevents math panics on malformed inputs (lo is always 0).
	x := clampInt(in.X, in.N)
	tx := clampInt(in.Tx, in.N)
	n := in.N
	frequency := max(in.Frequency, 0)
	monetary := math.Max(in.Monetary, 0)

	// Deterministic RNG seeded by FNV-1a hash of the sanitized inputs.
	seed := fnvHash(x, tx, n, frequency, monetary, draws)
	rng := rand.New(rand.NewSource(int64(seed))) //nolint:gosec // deterministic simulation, not cryptography

	// Allocate draw accumulators.
	aliveSlice := make([]float64, draws)
	exp12Slice := make([]float64, draws)
	clvSlice := make([]float64, draws)
	diasSlice := make([]float64, draws)

	for i := range draws {
		// 1. Sample Beta posteriors for purchase rate (pi) and churn (theta).
		piI := sampleBeta(rng, b.alpha+float64(x), b.beta+float64(max(n-x, 0)))
		thI := sampleBeta(rng, b.gamma+1, b.delta+float64(max(tx, 0)))

		// 2. P(alive): probability of surviving gap periods without churning.
		gap := max(n-tx, 0)
		aliveI := clamp01Finite(math.Pow(1-thI, float64(gap)))
		aliveSlice[i] = aliveI

		// 3. Expected repeat purchases in the next 12 months.
		// alive ∈ [0,1], pi ∈ (0,1), surv12 ∈ [0,12] — product always finite ≥ 0.
		var surv12 float64
		for m := 1; m <= 12; m++ {
			surv12 += math.Pow(1-thI, float64(m))
		}
		exp12Slice[i] = aliveI * piI * surv12

		// 4. Discounted expected transactions over the CLV horizon (DET).
		// Same finiteness guarantee as exp12: all factors in [0,H].
		var survH float64
		for m := 1; m <= horizonCLV; m++ {
			survH += math.Pow(1-thI, float64(m)) / math.Pow(1+discount, float64(m))
		}
		detI := aliveI * piI * survH

		// 5. Monetary noise: Gamma(k,1)/k has mean 1, spread shrinks as k grows.
		k := b.p*float64(frequency) + b.q // always ≥ q > 1
		mNoiseI := sampleGamma(rng, k) / k
		eM := b.ExpectedAvgProfit(frequency, monetary)

		// 6. CLV draw: margin × avg_ticket × DET × P(paga) − expected_loss.
		// eM and mNoiseI are finite and ≥ 0; detI is finite and ≥ 0.
		clvSlice[i] = in.Margin*eM*mNoiseI*detI*pPaga - in.PerdidaEsperada

		// 7. Days until next purchase: expected inter-purchase gap × days/month.
		//    Use a large cap (max of n months or 10 years) when piI is zero or
		//    the reciprocal is non-finite (subnormal piI edge case).
		diasI := math.Max(float64(n)*30.44, 3650)
		if piI > 0 {
			d := (1.0 / piI) * 30.44
			if isFinite(d) && d >= 0 {
				diasI = d
			}
		}
		diasSlice[i] = diasI
	}

	return Predicciones{
		Disponible:          true,
		PAlive:              intervalFromDraws(aliveSlice),
		ComprasEsperadas12m: intervalFromDraws(exp12Slice),
		CLV:                 intervalFromDraws(clvSlice),
		ProximaCompraDias:   intervalFromDraws(diasSlice),
		Draws:               draws,
	}
}

// ─── posterior summary ───────────────────────────────────────────────────────────

// intervalFromDraws sorts the draws and computes linear-interpolation quantiles
// at p5 (Lo), p50 (Punto), and p95 (Hi).
func intervalFromDraws(draws []float64) IntervaloEstimado {
	sorted := make([]float64, len(draws))
	copy(sorted, draws)
	sort.Float64s(sorted)
	return IntervaloEstimado{
		Lo:    quantileFromSorted(sorted, 0.05),
		Punto: quantileFromSorted(sorted, 0.50),
		Hi:    quantileFromSorted(sorted, 0.95),
	}
}

// quantileFromSorted returns the q-th quantile of a pre-sorted slice via linear
// interpolation (equivalent to numpy default / R type 7).
func quantileFromSorted(sorted []float64, q float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	pos := q * float64(n-1)
	lo := int(pos)
	hi := lo + 1
	if hi >= n {
		return sorted[n-1]
	}
	frac := pos - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// ─── samplers ────────────────────────────────────────────────────────────────────

// sampleGamma returns a draw from Gamma(shape=k, scale=1) using the
// Marsaglia–Tsang squeeze algorithm (Marsaglia & Tsang, 2000, ACM TOMS).
//
// For k < 1 it applies the reduction: if G ~ Gamma(k+1) and U ~ Uniform(0,1),
// then G·U^(1/k) ~ Gamma(k). This covers alpha=0.19 and gamma=0.046 which are
// the real fitted BG/BB parameters (both < 1).
func sampleGamma(rng *rand.Rand, k float64) float64 {
	if k < 1 {
		g := sampleGamma(rng, k+1)
		u := rng.Float64()
		return g * math.Pow(u, 1/k)
	}
	// Marsaglia–Tsang for k ≥ 1.
	d := k - 1.0/3.0
	c := 1.0 / math.Sqrt(9*d)
	for {
		xv := rng.NormFloat64()
		t := 1 + c*xv
		v := t * t * t
		if v <= 0 {
			continue
		}
		u := rng.Float64()
		xv4 := xv * xv * xv * xv
		if u < 1-0.0331*xv4 {
			return d * v
		}
		if math.Log(u) < 0.5*xv*xv+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}

// sampleBeta returns a draw from Beta(a, b) via the ratio-of-gammas method.
// The guard ga+gb==0 is a numerical safety net only; in practice both gammas
// are strictly positive.
func sampleBeta(rng *rand.Rand, a, b float64) float64 {
	ga := sampleGamma(rng, a)
	gb := sampleGamma(rng, b)
	if ga+gb == 0 {
		return 0.5
	}
	return ga / (ga + gb)
}

// ─── numeric helpers ─────────────────────────────────────────────────────────────

// clampInt clamps v to the closed interval [0, hi].
// All call sites pass 0 as the lower bound, so it is hard-coded here to keep
// the signature minimal (avoids the unparam lint warning).
func clampInt(v, hi int) int {
	if v < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}

// fnvHash produces a deterministic uint64 seed from the sanitized posterior
// inputs using FNV-1a 64-bit (hash/fnv). Little-endian encoding of each field.
func fnvHash(x, tx, n, frequency int, monetary float64, draws int) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	writeI64 := func(v int64) {
		buf[0] = byte(v)
		buf[1] = byte(v >> 8)
		buf[2] = byte(v >> 16)
		buf[3] = byte(v >> 24)
		buf[4] = byte(v >> 32)
		buf[5] = byte(v >> 40)
		buf[6] = byte(v >> 48)
		buf[7] = byte(v >> 56)
		_, _ = h.Write(buf[:])
	}
	writeI64(int64(x))
	writeI64(int64(tx))
	writeI64(int64(n))
	writeI64(int64(frequency))
	writeI64(int64(math.Float64bits(monetary))) // bit-exact encoding of monetary
	writeI64(int64(draws))
	return h.Sum64()
}
