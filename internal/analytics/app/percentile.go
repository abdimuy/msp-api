// Package app — percentile.go computes rank-based percentiles for a target
// value within a peer cohort.
//
//nolint:misspell // Spanish domain vocabulary per project convention.
package app

import "sort"

// benchmarkMuestraMinima is the minimum cohort size required to serve
// percentile statistics. Cohorts below this threshold are flagged with
// MuestraPequena=true and percentil/cuantiles are left at zero.
const benchmarkMuestraMinima = 30

// percentilEnCohorte returns the percentile rank of valor within cohorte (0..100,
// = 100 * count(v <= valor)/n), plus the cohorte median, p25, p75, and n.
// cohorte need not be sorted; n == 0 → all zeros.
func percentilEnCohorte(valor float64, cohorte []float64) (float64, float64, float64, float64, int) {
	n := len(cohorte)
	if n == 0 {
		return 0, 0, 0, 0, 0
	}
	sorted := make([]float64, n)
	copy(sorted, cohorte)
	sort.Float64s(sorted)

	// Percentile rank: fraction of cohorte with value <= valor, scaled 0..100.
	count := 0
	for _, v := range sorted {
		if v <= valor {
			count++
		}
	}
	percentil := 100.0 * float64(count) / float64(n)
	mediana := quantileFromSorted(sorted, 0.50)
	p25 := quantileFromSorted(sorted, 0.25)
	p75 := quantileFromSorted(sorted, 0.75)
	return percentil, mediana, p25, p75, n
}
