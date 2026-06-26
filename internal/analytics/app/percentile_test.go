//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"math"
	"testing"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/app"
)

// TestPercentilEnCohorte_Empty verifies that an empty cohorte returns all zeros.
func TestPercentilEnCohorte_Empty(t *testing.T) {
	t.Parallel()
	pct, med, p25, p75, n := app.ExportPercentilEnCohorte(50.0, nil)
	assert.Equal(t, 0, n)
	assert.InDelta(t, 0.0, pct, 1e-9)
	assert.InDelta(t, 0.0, med, 1e-9)
	assert.InDelta(t, 0.0, p25, 1e-9)
	assert.InDelta(t, 0.0, p75, 1e-9)
}

// TestPercentilEnCohorte_SingleElement verifies a single-element cohorte.
func TestPercentilEnCohorte_SingleElement(t *testing.T) {
	t.Parallel()
	// valor == only element → percentil = 100%
	pct, med, p25, p75, n := app.ExportPercentilEnCohorte(42.0, []float64{42.0})
	assert.Equal(t, 1, n)
	assert.InDelta(t, 100.0, pct, 1e-9)
	assert.InDelta(t, 42.0, med, 1e-9)
	assert.InDelta(t, 42.0, p25, 1e-9)
	assert.InDelta(t, 42.0, p75, 1e-9)
}

// TestPercentilEnCohorte_KnownDistribution verifies known percentil/quantile values.
func TestPercentilEnCohorte_KnownDistribution(t *testing.T) {
	t.Parallel()
	// cohorte: 10 evenly spaced values [10, 20, ..., 100]
	cohorte := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	// valor = 50 → 5 values ≤ 50 out of 10 → percentil = 50%
	pct, med, p25, p75, n := app.ExportPercentilEnCohorte(50.0, cohorte)
	assert.Equal(t, 10, n)
	assert.InDelta(t, 50.0, pct, 1e-9, "percentil at median of uniform distribution")
	// median = Q50 of [10..100] → linear interpolation at pos 4.5 → (50+60)/2 = 55
	assert.InDelta(t, 55.0, med, 1e-9, "median of [10,20,...,100]")
	// p25: pos = 0.25 * 9 = 2.25 → 30 + 0.25*(40-30) = 32.5
	assert.InDelta(t, 32.5, p25, 1e-9, "p25 of [10,20,...,100]")
	// p75: pos = 0.75 * 9 = 6.75 → 70 + 0.75*(80-70) = 77.5
	assert.InDelta(t, 77.5, p75, 1e-9, "p75 of [10,20,...,100]")
}

// TestPercentilEnCohorte_ValorBelowMin verifies valor below min → percentil 0.
func TestPercentilEnCohorte_ValorBelowMin(t *testing.T) {
	t.Parallel()
	cohorte := []float64{10, 20, 30, 40, 50}
	pct, _, _, _, _ := app.ExportPercentilEnCohorte(5.0, cohorte)
	assert.InDelta(t, 0.0, pct, 1e-9, "valor below min → percentil 0")
}

// TestPercentilEnCohorte_ValorAboveMax verifies valor above max → percentil 100.
func TestPercentilEnCohorte_ValorAboveMax(t *testing.T) {
	t.Parallel()
	cohorte := []float64{10, 20, 30, 40, 50}
	pct, _, _, _, _ := app.ExportPercentilEnCohorte(100.0, cohorte)
	assert.InDelta(t, 100.0, pct, 1e-9, "valor above max → percentil 100")
}

// TestPercentilEnCohorte_UnsortedInput verifies the function sorts the cohorte.
func TestPercentilEnCohorte_UnsortedInput(t *testing.T) {
	t.Parallel()
	cohorte := []float64{50, 10, 30, 20, 40}
	// same as sorted [10,20,30,40,50], valor=30 → 3/5 = 60%
	pct, _, _, _, n := app.ExportPercentilEnCohorte(30.0, cohorte)
	assert.Equal(t, 5, n)
	assert.InDelta(t, 60.0, pct, 1e-9)
}

// TestPercentilEnCohorte_Properties uses rapid to check invariants on random distributions.
func TestPercentilEnCohorte_Properties(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a non-empty cohorte of floats in [0, 100].
		size := rapid.IntRange(1, 50).Draw(rt, "size")
		cohorte := make([]float64, size)
		for i := range cohorte {
			cohorte[i] = rapid.Float64Range(0, 100).Draw(rt, "v")
		}
		valor := rapid.Float64Range(0, 100).Draw(rt, "valor")

		pct, med, p25, p75, n := app.ExportPercentilEnCohorte(valor, cohorte)

		// n must equal len(cohorte)
		require.Equal(rt, size, n, "n must equal size")

		// percentil ∈ [0, 100]
		assert.GreaterOrEqual(rt, pct, 0.0, "percentil >= 0")
		assert.LessOrEqual(rt, pct, 100.0, "percentil <= 100")

		// monotonicity: higher valor → percentil ≥ (not strictly for ties)
		higherValor := valor + 10
		pctHigher, _, _, _, _ := app.ExportPercentilEnCohorte(higherValor, cohorte)
		assert.GreaterOrEqual(rt, pctHigher, pct, "monotonicity: higher valor → percentil ≥")

		// quantile ordering: p25 ≤ mediana ≤ p75
		assert.LessOrEqual(rt, p25, med, "p25 ≤ mediana")
		assert.LessOrEqual(rt, med, p75, "mediana ≤ p75")

		// no panics on NaN-free float64 inputs
		assert.False(rt, math.IsNaN(pct), "percentil must not be NaN")
		assert.False(rt, math.IsNaN(med), "mediana must not be NaN")
	})
}
