// Package domain_test — tendencia_test.go verifies CalcularTendencia.
//
//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"math"
	"testing"

	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

// ─── Deterministic cases ──────────────────────────────────────────────────────

func TestCalcularTendencia_SerieCreciente(t *testing.T) {
	t.Parallel()
	got := domain.CalcularTendencia([]float64{1, 2, 3, 4, 5})
	if got.Slope <= 0 {
		t.Errorf("expected Slope > 0 for increasing series, got %f", got.Slope)
	}
	if got.Direccion != domain.DireccionMejorando {
		t.Errorf("expected DireccionMejorando, got %q", got.Direccion)
	}
}

func TestCalcularTendencia_SerieDecreciente(t *testing.T) {
	t.Parallel()
	got := domain.CalcularTendencia([]float64{5, 4, 3, 2, 1})
	if got.Slope >= 0 {
		t.Errorf("expected Slope < 0 for decreasing series, got %f", got.Slope)
	}
	if got.Direccion != domain.DireccionEmpeorando {
		t.Errorf("expected DireccionEmpeorando, got %q", got.Direccion)
	}
}

func TestCalcularTendencia_SeriePlana(t *testing.T) {
	t.Parallel()
	got := domain.CalcularTendencia([]float64{3, 3, 3, 3})
	if math.Abs(got.Slope) > 1e-9 {
		t.Errorf("expected Slope ≈ 0 for flat series, got %f", got.Slope)
	}
	if got.Direccion != domain.DireccionEstable {
		t.Errorf("expected DireccionEstable, got %q", got.Direccion)
	}
}

func TestCalcularTendencia_CambioDetectado(t *testing.T) {
	t.Parallel()
	// mediaPrevia = 10; ultimo = 50; |50-10| = 40 > 0.20*10 = 2 → Cambio=true
	got := domain.CalcularTendencia([]float64{10, 10, 10, 10, 50})
	if !got.Cambio {
		t.Error("expected Cambio=true when last point deviates notably from prior mean")
	}
}

func TestCalcularTendencia_SinCambio(t *testing.T) {
	t.Parallel()
	// mediaPrevia = 10; ultimo = 11; |11-10| = 1 ≤ 0.20*10 = 2 → Cambio=false
	got := domain.CalcularTendencia([]float64{10, 10, 10, 10, 11})
	if got.Cambio {
		t.Error("expected Cambio=false when last point is close to prior mean")
	}
}

func TestCalcularTendencia_NVacio_NoPanic(t *testing.T) {
	t.Parallel()
	got := domain.CalcularTendencia([]float64{})
	if got.Slope != 0 || got.Direccion != domain.DireccionEstable || got.Cambio {
		t.Errorf("expected zero Tendencia for empty series, got %+v", got)
	}
}

func TestCalcularTendencia_NUno_NoPanic(t *testing.T) {
	t.Parallel()
	got := domain.CalcularTendencia([]float64{42.5})
	if got.Slope != 0 || got.Direccion != domain.DireccionEstable || got.Cambio {
		t.Errorf("expected zero Tendencia for single-element series, got %+v", got)
	}
}

// TestCalcularTendencia_ValoresNoFinitos_SlopeSaneado verifies that non-finite
// inputs (Inf/NaN) never yield a non-finite Slope — the defensive guard zeroes it.
func TestCalcularTendencia_ValoresNoFinitos_SlopeSaneado(t *testing.T) {
	t.Parallel()
	cases := [][]float64{
		{math.Inf(1), 0},
		{0, math.Inf(-1)},
		{math.NaN(), 1, 2},
		{math.Inf(1), math.Inf(-1), 0},
	}
	for _, valores := range cases {
		got := domain.CalcularTendencia(valores)
		if math.IsNaN(got.Slope) || math.IsInf(got.Slope, 0) {
			t.Errorf("expected finite Slope for %v, got %f", valores, got.Slope)
		}
	}
}

// ─── Property tests ───────────────────────────────────────────────────────────

// TestCalcularTendencia_Property verifies that CalcularTendencia never panics
// with arbitrary inputs, always returns a valid Direccion, and always returns
// a finite Slope.
func TestCalcularTendencia_Property(t *testing.T) {
	t.Parallel()
	validDir := map[string]struct{}{
		domain.DireccionMejorando:  {},
		domain.DireccionEstable:    {},
		domain.DireccionEmpeorando: {},
	}

	rapid.Check(t, func(rt *rapid.T) {
		// Generate a slice of 0–20 float64s including negatives and zeros.
		n := rapid.IntRange(0, 20).Draw(rt, "n")
		valores := make([]float64, n)
		for i := range valores {
			valores[i] = rapid.Float64Range(-1e6, 1e6).Draw(rt, "v")
		}

		got := domain.CalcularTendencia(valores)

		if _, ok := validDir[got.Direccion]; !ok {
			rt.Fatalf("Direccion %q is not one of the three valid values", got.Direccion)
		}
		if math.IsNaN(got.Slope) || math.IsInf(got.Slope, 0) {
			rt.Fatalf("Slope is not finite: %f", got.Slope)
		}
	})
}
