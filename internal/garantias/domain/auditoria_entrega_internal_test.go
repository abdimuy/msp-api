package domain

import "testing"

// TestSuperaUmbralDesvio_Estricto pins the boundary rule: a distance exactly at
// UmbralDesvioMetros is still sin_desvio; only a strictly greater one is a
// deviation. The equality case is checked here, on the predicate itself,
// because a coordinate pair whose haversine lands exactly on 300.0 m depends on
// floating point rounding and is not portable between machines. This test is
// what makes changing > to >= fail.
func TestSuperaUmbralDesvio_Estricto(t *testing.T) {
	t.Parallel()
	casos := map[string]struct {
		metros float64
		quiere bool
	}{
		"muy por debajo": {metros: 0, quiere: false},
		"justo debajo":   {metros: UmbralDesvioMetros - 0.1, quiere: false},
		"exactamente":    {metros: UmbralDesvioMetros, quiere: false},
		"justo arriba":   {metros: UmbralDesvioMetros + 0.1, quiere: true},
	}
	for nombre, c := range casos {
		t.Run(nombre, func(t *testing.T) {
			t.Parallel()
			if got := superaUmbralDesvio(c.metros); got != c.quiere {
				t.Errorf("superaUmbralDesvio(%v) = %v, want %v", c.metros, got, c.quiere)
			}
		})
	}
}
