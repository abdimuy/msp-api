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

// ─── Deterministic mutation-killing tests ─────────────────────────────────────
//
// Each test below is designed to fail under a specific surviving gremlins mutant
// so that the mutation is "killed" in subsequent runs.

// TestCalcularTendencia_SlopeExacto kills the ARITHMETIC_BASE mutant on line 62
// (slope = num / den). The series [0, 2, 4, 6] is an exact arithmetic progression
// with slope = 2.0 by construction; any ±/×/÷ swap on num/den produces a value
// ≠ 2.0, which this assertion catches.
func TestCalcularTendencia_SlopeExacto(t *testing.T) {
	t.Parallel()
	// [0, 2, 4, 6]: meanY=3, meanI=1.5 → num=10, den=5 → slope=2.0 exactly.
	got := domain.CalcularTendencia([]float64{0, 2, 4, 6})
	if math.Abs(got.Slope-2.0) > 1e-9 {
		t.Errorf("expected Slope == 2.0, got %v", got.Slope)
	}
}

// TestCalcularTendencia_MediaAbs_AcumulacionCorrecta kills the INVERT_ASSIGNMENTS
// mutant on line 71 (mediaAbs += → mediaAbs -=). With values [9.6, 10, 10.4]:
//   - correct: slope=0.4, mediaAbs=10.0, umbral=0.5 → Estable (slope < umbral)
//   - mutated (-=): mediaAbs=-10.0/3, max→1.0, umbral=0.05 → slope 0.4 > 0.05 → Mejorando
func TestCalcularTendencia_MediaAbs_AcumulacionCorrecta(t *testing.T) {
	t.Parallel()
	// slope=0.4, mediaAbs=10.0, umbral=0.5 → Estable
	got := domain.CalcularTendencia([]float64{9.6, 10, 10.4})
	if math.Abs(got.Slope-0.4) > 1e-9 {
		t.Errorf("expected Slope == 0.4, got %v", got.Slope)
	}
	if got.Direccion != domain.DireccionEstable {
		t.Errorf("expected DireccionEstable (slope 0.4 < umbral 0.5), got %q", got.Direccion)
	}
}

// TestCalcularTendencia_MediaAbs_DivisionCorrecta kills REMOVE_SELF_ASSIGNMENTS on
// line 73 (mediaAbs /= n removed) AND the ARITHMETIC_BASE mutant on line 74
// (0.05 constant). With values [9, 10, 11]:
//   - correct: slope=1.0, mediaAbs=10.0, umbral=0.5 → Mejorando (slope > umbral)
//   - mutant (no /=n, line 73): mediaAbs=30, umbral=1.5 → slope 1.0 < 1.5 → Estable
//   - mutant (0.05→0.5, line 74): umbral=5.0 → slope 1.0 < 5.0 → Estable
func TestCalcularTendencia_MediaAbs_DivisionCorrecta(t *testing.T) {
	t.Parallel()
	// slope=1.0, mediaAbs=10.0, umbral=0.5 → Mejorando
	got := domain.CalcularTendencia([]float64{9, 10, 11})
	if math.Abs(got.Slope-1.0) > 1e-9 {
		t.Errorf("expected Slope == 1.0, got %v", got.Slope)
	}
	if got.Direccion != domain.DireccionMejorando {
		t.Errorf("expected DireccionMejorando (slope 1.0 > umbral 0.5), got %q", got.Direccion)
	}
}

// TestCalcularTendencia_UmbralFrontera_Positivo kills 4 NOT-COVERED CONDITIONALS_BOUNDARY
// mutants on lines 78/80 (> vs >= for the switch cases). For n=2, slope = b-a exactly.
// With small values mediaAbs < 1.0 → floor applies → umbral = 0.05.
//   - slope = 0.06 (JUST above 0.05) → Mejorando
//   - slope = 0.04 (JUST below 0.05) → Estable
//   - slope = 0.05 (AT boundary)   → Estable  (> not >=; mutation >=  → Mejorando)
func TestCalcularTendencia_UmbralFrontera_Positivo(t *testing.T) {
	t.Parallel()
	// Just above umbral → Mejorando
	got := domain.CalcularTendencia([]float64{0.0, 0.06}) // slope=0.06, umbral=0.05
	if got.Direccion != domain.DireccionMejorando {
		t.Errorf("[0,0.06] expected DireccionMejorando, got %q", got.Direccion)
	}
	// Just below umbral → Estable
	got = domain.CalcularTendencia([]float64{0.0, 0.04}) // slope=0.04
	if got.Direccion != domain.DireccionEstable {
		t.Errorf("[0,0.04] expected DireccionEstable, got %q", got.Direccion)
	}
	// Exactly at umbral → Estable (not >=)
	got = domain.CalcularTendencia([]float64{0.0, 0.05}) // slope=0.05 == umbral
	if got.Direccion != domain.DireccionEstable {
		t.Errorf("[0,0.05] expected DireccionEstable (boundary: > not >=), got %q", got.Direccion)
	}
}

// TestCalcularTendencia_UmbralFrontera_Negativo is the symmetric counterpart of
// TestCalcularTendencia_UmbralFrontera_Positivo for the empeorando branch (slope < -umbral).
func TestCalcularTendencia_UmbralFrontera_Negativo(t *testing.T) {
	t.Parallel()
	// Just below -umbral → Empeorando
	got := domain.CalcularTendencia([]float64{0.06, 0.0}) // slope=-0.06
	if got.Direccion != domain.DireccionEmpeorando {
		t.Errorf("[0.06,0] expected DireccionEmpeorando, got %q", got.Direccion)
	}
	// Just above -umbral → Estable
	got = domain.CalcularTendencia([]float64{0.04, 0.0}) // slope=-0.04
	if got.Direccion != domain.DireccionEstable {
		t.Errorf("[0.04,0] expected DireccionEstable, got %q", got.Direccion)
	}
	// Exactly at -umbral → Estable (not <=)
	got = domain.CalcularTendencia([]float64{0.05, 0.0}) // slope=-0.05 == -umbral
	if got.Direccion != domain.DireccionEstable {
		t.Errorf("[0.05,0] expected DireccionEstable (boundary: < not <=), got %q", got.Direccion)
	}
}

// TestCalcularTendencia_CambioFrontera kills the CONDITIONALS_BOUNDARY mutant on
// line 94 (> vs >= for the Cambio threshold 0.20*max(mediaPrevia,1.0)).
// With mediaPrevia=10.0: threshold = 0.20*10 = 2.0.
//   - |ultimo - mediaPrevia| = 2.1 (JUST above 2.0) → Cambio=true
//   - |ultimo - mediaPrevia| = 2.0 (AT boundary)    → Cambio=false  (> not >=)
//   - |ultimo - mediaPrevia| = 1.9 (JUST below 2.0) → Cambio=false
func TestCalcularTendencia_CambioFrontera(t *testing.T) {
	t.Parallel()
	// mediaPrevia = (10+10)/2 = 10.0; threshold = 2.0
	// Just above threshold → Cambio=true
	got := domain.CalcularTendencia([]float64{10.0, 10.0, 12.1})
	if !got.Cambio {
		t.Error("[10,10,12.1] expected Cambio=true (|2.1| > 2.0)")
	}
	// At threshold exactly → Cambio=false (> not >=)
	got = domain.CalcularTendencia([]float64{10.0, 10.0, 12.0})
	if got.Cambio {
		t.Error("[10,10,12.0] expected Cambio=false (|2.0| not > 2.0, boundary: > not >=)")
	}
	// Just below threshold → Cambio=false
	got = domain.CalcularTendencia([]float64{10.0, 10.0, 11.9})
	if got.Cambio {
		t.Error("[10,10,11.9] expected Cambio=false (|1.9| < 2.0)")
	}
	// Negative direction: same threshold, ultimo below mean
	got = domain.CalcularTendencia([]float64{10.0, 10.0, 7.9})
	if !got.Cambio {
		t.Error("[10,10,7.9] expected Cambio=true (|-2.1| > 2.0)")
	}
	got = domain.CalcularTendencia([]float64{10.0, 10.0, 8.0})
	if got.Cambio {
		t.Error("[10,10,8.0] expected Cambio=false (|-2.0| not > 2.0)")
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
