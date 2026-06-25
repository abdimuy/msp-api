//nolint:misspell // Spanish field names per project convention.
package app

import (
	"math"
	"math/rand" //nolint:gosec // test-only RNG, not cryptography
	"testing"

	"pgregory.net/rapid"
)

// ─── RED phase anchor ────────────────────────────────────────────────────────────
// TestPosteriorsDeterminism was written first (RED) to drive the Posteriors()
// skeleton; it became GREEN once the FNV seed + RNG path was wired up.

func TestPosteriorsDeterminism(t *testing.T) {
	t.Parallel()
	b := loadBTYDForTest(t)
	in := PosteriorInput{
		X: 3, Tx: 8, N: 12,
		Frequency: 3, Monetary: 5000,
		Draws: 200, Margin: 0.528, PPaga: 1.0,
	}
	p1 := b.Posteriors(in)
	p2 := b.Posteriors(in)
	if p1 != p2 {
		t.Errorf("Posteriors is not deterministic:\np1=%+v\np2=%+v", p1, p2)
	}
}

// ─── credible interval contains the point ─────────────────────────────────────────

func TestPosteriorsICContainsPoint(t *testing.T) {
	t.Parallel()
	b := loadBTYDForTest(t)
	in := PosteriorInput{
		X: 5, Tx: 10, N: 24,
		Frequency: 5, Monetary: 8000,
		Draws: 500, Margin: 0.528, PPaga: 1.0,
	}
	p := b.Posteriors(in)
	if !p.Disponible {
		t.Fatal("expected Disponible=true")
	}
	for _, tc := range []struct {
		name string
		iv   IntervaloEstimado
	}{
		{"PAlive", p.PAlive},
		{"ComprasEsperadas12m", p.ComprasEsperadas12m},
		{"CLV", p.CLV},
		{"ProximaCompraDias", p.ProximaCompraDias},
	} {
		if tc.iv.Lo > tc.iv.Punto+1e-12 {
			t.Errorf("%s: Lo(%.6f) > Punto(%.6f)", tc.name, tc.iv.Lo, tc.iv.Punto)
		}
		if tc.iv.Punto > tc.iv.Hi+1e-12 {
			t.Errorf("%s: Punto(%.6f) > Hi(%.6f)", tc.name, tc.iv.Punto, tc.iv.Hi)
		}
	}
}

// ─── interval width decreases with more history ───────────────────────────────────

func TestPosteriorsAnchoDecreceCHistoria(t *testing.T) {
	t.Parallel()
	b := loadBTYDForTest(t)

	// Small history (n=6, x≈n/2=3)
	small := b.Posteriors(PosteriorInput{
		X: 3, Tx: 4, N: 6,
		Frequency: 3, Monetary: 5000,
		Draws: 2000, Margin: 0.528, PPaga: 1.0,
	})
	// Large history (n=60, x≈n/2=30)
	large := b.Posteriors(PosteriorInput{
		X: 30, Tx: 40, N: 60,
		Frequency: 30, Monetary: 5000,
		Draws: 2000, Margin: 0.528, PPaga: 1.0,
	})

	if small.ComprasEsperadas12m.Punto <= 0 || large.ComprasEsperadas12m.Punto <= 0 {
		t.Skip("punto <= 0, skipping relative width comparison")
	}
	smallWidth := (small.ComprasEsperadas12m.Hi - small.ComprasEsperadas12m.Lo) / small.ComprasEsperadas12m.Punto
	largeWidth := (large.ComprasEsperadas12m.Hi - large.ComprasEsperadas12m.Lo) / large.ComprasEsperadas12m.Punto

	if smallWidth <= largeWidth {
		t.Errorf("expected relative width to decrease with more history: small=%v large=%v", smallWidth, largeWidth)
	}
}

// ─── bounds ──────────────────────────────────────────────────────────────────────

func TestPosteriorsBounds(t *testing.T) {
	t.Parallel()
	b := loadBTYDForTest(t)
	p := b.Posteriors(PosteriorInput{
		X: 4, Tx: 9, N: 18,
		Frequency: 4, Monetary: 6000,
		Draws: 500, Margin: 0.528, PPaga: 0.8, PerdidaEsperada: 200,
	})
	if !p.Disponible {
		t.Fatal("expected Disponible=true")
	}
	if p.PAlive.Lo < 0 || p.PAlive.Hi > 1+1e-12 {
		t.Errorf("PAlive out of [0,1]: Lo=%v Hi=%v", p.PAlive.Lo, p.PAlive.Hi)
	}
	if p.PAlive.Punto < 0 || p.PAlive.Punto > 1+1e-12 {
		t.Errorf("PAlive.Punto=%v out of [0,1]", p.PAlive.Punto)
	}
	if p.ComprasEsperadas12m.Lo < 0 {
		t.Errorf("ComprasEsperadas12m.Lo=%v < 0", p.ComprasEsperadas12m.Lo)
	}
	if !isFinite(p.CLV.Punto) || !isFinite(p.CLV.Lo) || !isFinite(p.CLV.Hi) {
		t.Errorf("CLV not all finite: %+v", p.CLV)
	}
	if p.ProximaCompraDias.Lo < 0 {
		t.Errorf("ProximaCompraDias.Lo=%v < 0", p.ProximaCompraDias.Lo)
	}
}

// ─── degradation: N=0 → Disponible=false, zero-value ─────────────────────────────

func TestPosteriorsDegradacion(t *testing.T) {
	t.Parallel()
	b := loadBTYDForTest(t)
	p := b.Posteriors(PosteriorInput{N: 0})
	if p.Disponible {
		t.Error("expected Disponible=false for N=0")
	}
	if p.Draws != 0 {
		t.Errorf("expected Draws=0 for N=0, got %d", p.Draws)
	}
	zero := Predicciones{}
	if p != zero {
		t.Errorf("expected zero-value Predicciones for N=0, got %+v", p)
	}
}

// ─── edge: X=0 (no repeat purchase) ─────────────────────────────────────────────

func TestPosteriorsEdgeXZero(t *testing.T) {
	t.Parallel()
	b := loadBTYDForTest(t)
	p := b.Posteriors(PosteriorInput{
		X: 0, Tx: 0, N: 12,
		Frequency: 0, Monetary: 3000,
		Draws: 200, Margin: 0.528, PPaga: 1.0,
	})
	if !p.Disponible {
		t.Fatal("expected Disponible=true for X=0, N=12")
	}
	if !isFinite(p.PAlive.Punto) || p.PAlive.Punto < 0 || p.PAlive.Punto > 1+1e-12 {
		t.Errorf("PAlive.Punto=%v not in [0,1]", p.PAlive.Punto)
	}
}

// ─── edge: Monetary=0 → CLV finite ────────────────────────────────────────────────

func TestPosteriorsEdgeMonetaryZero(t *testing.T) {
	t.Parallel()
	b := loadBTYDForTest(t)
	p := b.Posteriors(PosteriorInput{
		X: 3, Tx: 6, N: 12,
		Frequency: 3, Monetary: 0,
		Draws: 200, Margin: 0.528, PPaga: 1.0,
	})
	if !isFinite(p.CLV.Punto) {
		t.Errorf("CLV.Punto=%v not finite for Monetary=0", p.CLV.Punto)
	}
}

// ─── Gamma k<1 branch ─────────────────────────────────────────────────────────────
// alpha=0.19 < 1 and gamma=0.046 < 1 exercise the k<1 branch on every Posteriors
// call. Here we also directly verify sampleGamma(0.2) produces finite positive values.

func TestGammaK1Branch(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // test helper, not cryptography
	for i := range 1000 {
		v := sampleGamma(rng, 0.2)
		if !isFinite(v) || v <= 0 {
			t.Fatalf("sampleGamma(0.2) draw %d = %v: expected finite positive", i, v)
		}
	}
	// Also confirm via Posteriors (full pipeline uses alpha=0.19 < 1 and gamma=0.046 < 1).
	b := loadBTYDForTest(t)
	p := b.Posteriors(PosteriorInput{
		X: 2, Tx: 5, N: 10,
		Frequency: 2, Monetary: 4000,
		Draws: 200, Margin: 0.528, PPaga: 1.0,
	})
	if !p.Disponible {
		t.Fatal("expected Disponible=true")
	}
}

// ─── defaults ─────────────────────────────────────────────────────────────────────

func TestPosteriorsDefaults(t *testing.T) {
	t.Parallel()
	b := loadBTYDForTest(t)
	// Draws<=0 → 2000; HorizonCLV<=0 → b.HorizonMonths(); Discount<=0 → b.MonthlyDiscount(); PPaga<=0 → 1.0
	p := b.Posteriors(PosteriorInput{
		X: 2, Tx: 4, N: 8,
		Frequency: 2, Monetary: 3000,
		Margin: 0.528,
		// Draws, HorizonCLV, Discount, PPaga all zero → defaults
	})
	if p.Draws != 2000 {
		t.Errorf("expected Draws=2000 (default), got %d", p.Draws)
	}
	if !p.Disponible {
		t.Fatal("expected Disponible=true")
	}
}

// ─── perdida deducted from CLV ────────────────────────────────────────────────────

func TestPosteriorsPerdidaDeducted(t *testing.T) {
	t.Parallel()
	b := loadBTYDForTest(t)
	noPerdida := b.Posteriors(PosteriorInput{
		X: 3, Tx: 6, N: 12,
		Frequency: 3, Monetary: 5000,
		Draws: 200, Margin: 0.528, PPaga: 1.0, PerdidaEsperada: 0,
	})
	conPerdida := b.Posteriors(PosteriorInput{
		X: 3, Tx: 6, N: 12,
		Frequency: 3, Monetary: 5000,
		Draws: 200, Margin: 0.528, PPaga: 1.0, PerdidaEsperada: 1000,
	})
	// CLV with loss should be lower by approximately the perdida.
	if noPerdida.CLV.Punto <= conPerdida.CLV.Punto {
		t.Errorf("PerdidaEsperada=1000 should reduce CLV: noPerdida=%v conPerdida=%v",
			noPerdida.CLV.Punto, conPerdida.CLV.Punto)
	}
	diff := noPerdida.CLV.Punto - conPerdida.CLV.Punto
	if math.Abs(diff-1000) > 1e-6 {
		t.Errorf("CLV difference should be exactly 1000 (same draws), got %.6f", diff)
	}
}

// ─── property test ────────────────────────────────────────────────────────────────

func TestPosteriorsProperty(t *testing.T) {
	t.Parallel()
	b := loadBTYDForTest(t)
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 200).Draw(rt, "n")
		x := rapid.IntRange(0, n).Draw(rt, "x")
		tx := rapid.IntRange(0, n).Draw(rt, "tx")
		monetary := rapid.Float64Range(0, 1e6).Draw(rt, "monetary")
		margin := rapid.Float64Range(0, 1).Draw(rt, "margin")
		pPaga := rapid.Float64Range(0.001, 1.0).Draw(rt, "ppaga")
		perdida := rapid.Float64Range(0, 1e5).Draw(rt, "perdida")

		p := b.Posteriors(PosteriorInput{
			X: x, Tx: tx, N: n,
			Frequency: x, Monetary: monetary,
			Draws:           200, // small for speed
			Margin:          margin,
			PPaga:           pPaga,
			PerdidaEsperada: perdida,
		})
		if !p.Disponible {
			rt.Fatal("Disponible=false for N>0")
		}
		if p.Draws != 200 {
			rt.Fatalf("Draws=%d, expected 200", p.Draws)
		}
		// PAlive in [0,1]
		if p.PAlive.Lo < 0 || p.PAlive.Hi > 1+1e-12 {
			rt.Fatalf("PAlive out of [0,1]: %+v", p.PAlive)
		}
		// Lo <= Punto <= Hi for all four metrics
		for _, iv := range []IntervaloEstimado{p.PAlive, p.ComprasEsperadas12m, p.CLV, p.ProximaCompraDias} {
			if iv.Lo > iv.Punto+1e-12 {
				rt.Fatalf("Lo(%.6f) > Punto(%.6f)", iv.Lo, iv.Punto)
			}
			if iv.Punto > iv.Hi+1e-12 {
				rt.Fatalf("Punto(%.6f) > Hi(%.6f)", iv.Punto, iv.Hi)
			}
		}
		// ComprasEsperadas12m ≥ 0
		if p.ComprasEsperadas12m.Lo < 0 {
			rt.Fatalf("ComprasEsperadas12m.Lo=%v < 0", p.ComprasEsperadas12m.Lo)
		}
		// CLV all finite
		if !isFinite(p.CLV.Punto) || !isFinite(p.CLV.Lo) || !isFinite(p.CLV.Hi) {
			rt.Fatalf("CLV not all finite: %+v", p.CLV)
		}
		// ProximaCompraDias ≥ 0
		if p.ProximaCompraDias.Lo < 0 {
			rt.Fatalf("ProximaCompraDias.Lo=%v < 0", p.ProximaCompraDias.Lo)
		}
	})
}

// ─── fuzz ─────────────────────────────────────────────────────────────────────────

func FuzzPosteriors(f *testing.F) {
	b, err := LoadBTYD()
	if err != nil {
		f.Fatalf("LoadBTYD: %v", err)
	}
	// Seed corpus.
	type seed struct {
		x, tx, n, freq int
		monetary       float64
	}
	for _, s := range []seed{
		{0, 0, 1, 0, 0},
		{3, 8, 12, 3, 5000},
		{12, 24, 24, 12, 100000},
		{0, 0, 60, 0, 500},
		{59, 59, 60, 59, 8000},
	} {
		f.Add(s.x, s.tx, s.n, s.freq, s.monetary)
	}
	f.Fuzz(func(t *testing.T, x, tx, n, freq int, monetary float64) {
		// Sanitize to valid domain.
		if n < 0 {
			n = -n
		}
		n %= 201 // 0..200
		if x < 0 {
			x = -x
		}
		if n > 0 {
			x %= n + 1
		} else {
			x = 0
		}
		if tx < 0 {
			tx = -tx
		}
		if n > 0 {
			tx %= n + 1
		} else {
			tx = 0
		}
		if freq < 0 {
			freq = -freq
		}
		if math.IsNaN(monetary) || math.IsInf(monetary, 0) {
			monetary = 0
		}
		if monetary < 0 {
			monetary = -monetary
		}
		monetary = math.Mod(monetary, 1e6+1)

		p := b.Posteriors(PosteriorInput{
			X: x, Tx: tx, N: n,
			Frequency: freq, Monetary: monetary,
			Draws: 50, Margin: 0.528, PPaga: 1.0,
		})
		if n == 0 {
			if p.Disponible {
				t.Fatalf("Disponible=true for N=0")
			}
			return
		}
		if !p.Disponible {
			t.Fatalf("Disponible=false for N=%d>0", n)
		}
		// Invariants.
		if p.PAlive.Lo < 0 || p.PAlive.Hi > 1+1e-12 {
			t.Fatalf("PAlive out of [0,1]: %+v (x=%d,tx=%d,n=%d)", p.PAlive, x, tx, n)
		}
		for _, iv := range []IntervaloEstimado{p.PAlive, p.ComprasEsperadas12m, p.CLV, p.ProximaCompraDias} {
			if iv.Lo > iv.Punto+1e-12 {
				t.Fatalf("Lo(%.6f) > Punto(%.6f)", iv.Lo, iv.Punto)
			}
			if iv.Punto > iv.Hi+1e-12 {
				t.Fatalf("Punto(%.6f) > Hi(%.6f)", iv.Punto, iv.Hi)
			}
		}
		if !isFinite(p.CLV.Punto) {
			t.Fatalf("CLV.Punto not finite (x=%d,tx=%d,n=%d,monetary=%v)", x, tx, n, monetary)
		}
		if p.ProximaCompraDias.Lo < 0 {
			t.Fatalf("ProximaCompraDias.Lo=%v < 0", p.ProximaCompraDias.Lo)
		}
	})
}

// ─── helper function unit tests ──────────────────────────────────────────────────

func TestQuantileFromSortedEdgeCases(t *testing.T) {
	t.Parallel()
	// Empty slice → 0.
	if got := quantileFromSorted(nil, 0.5); got != 0 {
		t.Errorf("quantileFromSorted(nil, 0.5) = %v, want 0", got)
	}
	// Single element → that element for any q.
	if got := quantileFromSorted([]float64{7.5}, 0.5); got != 7.5 {
		t.Errorf("quantileFromSorted([7.5], 0.5) = %v, want 7.5", got)
	}
	// q=1.0 on a 5-element slice: pos=4, lo=4, hi=5 >= n=5 → return last element.
	sorted5 := []float64{1, 2, 3, 4, 5}
	if got := quantileFromSorted(sorted5, 1.0); got != 5 {
		t.Errorf("quantileFromSorted([1..5], 1.0) = %v, want 5", got)
	}
	// Normal interpolation sanity: q=0.5 on 2 elements → midpoint.
	if got := quantileFromSorted([]float64{2, 4}, 0.5); got != 3 {
		t.Errorf("quantileFromSorted([2,4], 0.5) = %v, want 3", got)
	}
}

func TestClampIntBranches(t *testing.T) {
	t.Parallel()
	if got := clampInt(-5, 10); got != 0 {
		t.Errorf("clampInt(-5,10) = %d, want 0 (v<0 branch)", got)
	}
	if got := clampInt(15, 10); got != 10 {
		t.Errorf("clampInt(15,10) = %d, want 10 (v>hi branch)", got)
	}
	if got := clampInt(5, 10); got != 5 {
		t.Errorf("clampInt(5,10) = %d, want 5 (default branch)", got)
	}
}

func TestIntervalFromDrawsEdge(t *testing.T) {
	t.Parallel()
	// Single draw → Lo == Punto == Hi.
	iv := intervalFromDraws([]float64{3.14})
	if iv.Lo != 3.14 || iv.Punto != 3.14 || iv.Hi != 3.14 {
		t.Errorf("intervalFromDraws([3.14]) = %+v, want all 3.14", iv)
	}
}

// newTestRNG constructs a seeded *rand.Rand for use in test helpers.
func newTestRNG(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed)) //nolint:gosec // test-only, not cryptography
}
