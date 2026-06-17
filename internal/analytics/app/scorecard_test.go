// Package app_test — scorecard_test.go tests the logistic-regression credit
// scorecard: load/parse validation, Aplicar correctness, and property invariants.
//
//nolint:misspell // Spanish field names per project convention.
package app_test

import (
	"encoding/json"
	"errors"
	"testing"

	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// ─── Load / Parse tests ───────────────────────────────────────────────────────

func TestLoadScorecard_EmbeddedIsValid(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadScorecard()
	if err != nil {
		t.Fatalf("LoadScorecard() unexpected error: %v", err)
	}
	if sc.Version() == "" {
		t.Error("Version() must not be empty")
	}
}

func TestParseScorecard_MalformedJSON(t *testing.T) {
	t.Parallel()

	_, err := app.ParseScorecard([]byte(`{not valid json`))
	if err == nil {
		t.Fatal("ParseScorecard(malformed): expected error, got nil")
	}
	if !isErrScorecardInvalido(err) {
		t.Errorf("ParseScorecard(malformed): expected ErrScorecardInvalido, got %v", err)
	}
}

func TestParseScorecard_EmptyVersion(t *testing.T) {
	t.Parallel()

	data := buildScorecardJSON(t, scorecardOverride{emptyVersion: true})
	_, err := app.ParseScorecard(data)
	if err == nil {
		t.Fatal("ParseScorecard(empty version): expected error, got nil")
	}
	if !isErrScorecardInvalido(err) {
		t.Errorf("expected ErrScorecardInvalido, got %v", err)
	}
}

func TestParseScorecard_EmptyFeatures(t *testing.T) {
	t.Parallel()

	data := buildScorecardJSON(t, scorecardOverride{emptyFeatures: true})
	_, err := app.ParseScorecard(data)
	if err == nil {
		t.Fatal("ParseScorecard(empty features): expected error, got nil")
	}
	if !isErrScorecardInvalido(err) {
		t.Errorf("expected ErrScorecardInvalido, got %v", err)
	}
}

func TestParseScorecard_NonMonotonicBands_BajoLessThanMedio(t *testing.T) {
	t.Parallel()

	// bajo_min <= medio_min violates monotonicity.
	data := buildScorecardJSON(t, scorecardOverride{bajoMin: 40, medioMin: 50, altoMin: 25})
	_, err := app.ParseScorecard(data)
	if err == nil {
		t.Fatal("ParseScorecard(non-monotonic bands): expected error, got nil")
	}
	if !isErrScorecardInvalido(err) {
		t.Errorf("expected ErrScorecardInvalido, got %v", err)
	}
}

func TestParseScorecard_NonMonotonicBands_MedioEqualsAlto(t *testing.T) {
	t.Parallel()

	// medio_min == alto_min violates strict monotonicity.
	data := buildScorecardJSON(t, scorecardOverride{bajoMin: 75, medioMin: 50, altoMin: 50})
	_, err := app.ParseScorecard(data)
	if err == nil {
		t.Fatal("ParseScorecard(medio==alto): expected error, got nil")
	}
	if !isErrScorecardInvalido(err) {
		t.Errorf("expected ErrScorecardInvalido, got %v", err)
	}
}

func TestParseScorecard_BandOutOfRange(t *testing.T) {
	t.Parallel()

	data := buildScorecardJSON(t, scorecardOverride{bajoMin: 110, medioMin: 50, altoMin: 25})
	_, err := app.ParseScorecard(data)
	if err == nil {
		t.Fatal("ParseScorecard(band > 100): expected error, got nil")
	}
	if !isErrScorecardInvalido(err) {
		t.Errorf("expected ErrScorecardInvalido, got %v", err)
	}
}

// ─── Aplicar correctness tests ────────────────────────────────────────────────

func TestAplicar_AllMeanVector_MidScore(t *testing.T) {
	t.Parallel()

	// Use the fixed test scorecard (F1, weight=1.0, mean=0.5, std=0.2, intercept=-1.0).
	// When F1 == mean (0.5): z1 = (0.5-0.5)/0.2 = 0, logit = -1.0,
	// p_bad = sigmoid(-1.0) ≈ 0.269, score = round(100*(1-0.269)) = 73.
	// With bajo_min=75: score 73 falls in MEDIO band.
	// We just verify invariants — no hardcoded band — since the test scorecard may change.
	data := buildScorecardJSON(t, scorecardOverride{})
	sc, err := app.ParseScorecard(data)
	if err != nil {
		t.Fatalf("ParseScorecard: %v", err)
	}

	features := map[string]float64{"F1": 0.5} // F1 at its mean

	score, banda, drivers := sc.Aplicar(features)

	if score.Int() < 0 || score.Int() > 100 {
		t.Errorf("score out of range: %d", score.Int())
	}
	if !banda.IsValid() {
		t.Errorf("invalid banda %q (score=%d)", banda, score.Int())
	}
	// When z_i=0 for every feature, no feature has positive logit contribution → no drivers.
	if len(drivers) != 0 {
		t.Errorf("all-mean vector: expected 0 drivers, got %d: %v", len(drivers), drivers)
	}
}

func TestAplicar_EmptyMap_MidScore(t *testing.T) {
	t.Parallel()

	// Empty map: all features treated as mean → same as all-mean vector.
	data := buildScorecardJSON(t, scorecardOverride{})
	sc, err := app.ParseScorecard(data)
	if err != nil {
		t.Fatalf("ParseScorecard: %v", err)
	}

	score, banda, _ := sc.Aplicar(map[string]float64{})

	if score.Int() < 0 || score.Int() > 100 {
		t.Errorf("score out of range: %d", score.Int())
	}
	if !banda.IsValid() {
		t.Errorf("invalid banda %q (score=%d)", banda, score.Int())
	}
}

func TestAplicar_ExtremeRisk_Critico(t *testing.T) {
	t.Parallel()

	// Fixed test scorecard: F1 with weight=+1.0 (positive weight → increasing F1 increases risk).
	// Set F1 far above mean to trigger high risk (low score).
	data := buildScorecardJSON(t, scorecardOverride{})
	sc, err := app.ParseScorecard(data)
	if err != nil {
		t.Fatalf("ParseScorecard: %v", err)
	}

	// F1=10.0 is (10.0-0.5)/0.2 = 47.5 standard deviations above mean → near certain bad.
	features := map[string]float64{"F1": 10.0}

	score, banda, drivers := sc.Aplicar(features)

	if score.Int() < 0 || score.Int() > 100 {
		t.Errorf("score out of range: %d", score.Int())
	}
	// Extreme risk → expect ALTO or CRITICO (not BAJO).
	if banda == domain.BandaCreditoBajo {
		t.Errorf("extreme-risk vector: did not expect BAJO (score=%d)", score.Int())
	}
	// At least one driver expected (F1 has positive contrib).
	if len(drivers) == 0 {
		t.Errorf("extreme-risk vector: expected at least 1 driver, got 0")
	}
}

func TestAplicar_ExtremeGood_Bajo(t *testing.T) {
	t.Parallel()

	// F1 far below mean → very low risk → high score → BAJO band.
	data := buildScorecardJSON(t, scorecardOverride{})
	sc, err := app.ParseScorecard(data)
	if err != nil {
		t.Fatalf("ParseScorecard: %v", err)
	}

	// F1=-10.0 is far below mean → negative z, weight +1.0 → negative logit contribution → low p_bad → high score.
	features := map[string]float64{"F1": -10.0}

	score, banda, drivers := sc.Aplicar(features)

	if score.Int() < 0 || score.Int() > 100 {
		t.Errorf("score out of range: %d", score.Int())
	}
	if banda != domain.BandaCreditoBajo {
		t.Errorf("extreme-good vector: expected BAJO, got %q (score=%d)", banda, score.Int())
	}
	// No feature has positive logit contribution → zero drivers.
	if len(drivers) != 0 {
		t.Errorf("extreme-good vector: expected 0 drivers, got %d: %v", len(drivers), drivers)
	}
}

func TestAplicar_ScoreInRange(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadScorecard()
	if err != nil {
		t.Fatalf("LoadScorecard: %v", err)
	}

	// These vectors use v1 feature names — some unknown to the scorecard, some known.
	vectors := []map[string]float64{
		{},
		{"DIAS_SIN_PAGAR": 0.0},
		{"DIAS_SIN_PAGAR": 90.0, "PCT_PAGOS_A_TIEMPO_6M": 0.0},
		{"DIAS_SIN_PAGAR": 15.0, "PAGOS_90D": 5.0, "PCT_PAGOS_A_TIEMPO_6M": 0.85, "CADENCIA_DIAS": 20.0},
		{"DIAS_SIN_PAGAR": -100.0},                  // extreme low (std guard)
		{"DIAS_SIN_PAGAR": 500.0, "PAGOS_90D": 0.0}, // extreme high
	}

	for i, fv := range vectors {
		score, banda, drivers := sc.Aplicar(fv)
		if score.Int() < 0 || score.Int() > 100 {
			t.Errorf("vector[%d]: score %d out of [0,100]", i, score.Int())
		}
		if !banda.IsValid() {
			t.Errorf("vector[%d]: invalid banda %q", i, banda)
		}
		if len(drivers) > 3 {
			t.Errorf("vector[%d]: too many drivers: %d", i, len(drivers))
		}
	}
}

func TestAplicar_BandaMatchesScore(t *testing.T) {
	t.Parallel()

	// Use fixed test scorecard with known band boundaries (bajo=75, medio=50, alto=25).
	data := buildScorecardJSON(t, scorecardOverride{})
	sc, err := app.ParseScorecard(data)
	if err != nil {
		t.Fatalf("ParseScorecard: %v", err)
	}

	// Test band boundary semantics by cross-checking score vs expected band.
	// The test scorecard has bajo_min=75, medio_min=50, alto_min=25.
	// We use feature F1 at various values and verify the returned banda is
	// consistent with the computed score.
	vectors := []map[string]float64{
		{},
		{"F1": 0.5},
		{"F1": 10.0},
		{"F1": -10.0},
	}

	for _, fv := range vectors {
		score, banda, _ := sc.Aplicar(fv)
		n := score.Int()
		var expected domain.BandaCredito
		switch {
		case n >= 75:
			expected = domain.BandaCreditoBajo
		case n >= 50:
			expected = domain.BandaCreditoMedio
		case n >= 25:
			expected = domain.BandaCreditoAlto
		default:
			expected = domain.BandaCreditoCritico
		}
		if banda != expected {
			t.Errorf("score=%d: expected banda %q, got %q", n, expected, banda)
		}
	}
}

func TestAplicar_DriversAreSorted(t *testing.T) {
	t.Parallel()

	// Use fixed test scorecard with single feature F1 (weight=+1.0, mean=0.5, std=0.2).
	// When F1=1.0: z=(1.0-0.5)/0.2=2.5, contrib=1.0*2.5=2.5 > 0 → "feature one" is the driver.
	data := buildScorecardJSON(t, scorecardOverride{})
	sc, err := app.ParseScorecard(data)
	if err != nil {
		t.Fatalf("ParseScorecard: %v", err)
	}

	features := map[string]float64{"F1": 1.0}

	_, _, drivers := sc.Aplicar(features)

	if len(drivers) != 1 {
		t.Fatalf("expected 1 driver, got %d: %v", len(drivers), drivers)
	}
	if drivers[0] != "feature one" {
		t.Errorf("driver[0] = %q, want %q", drivers[0], "feature one")
	}
}

// ─── Property tests ───────────────────────────────────────────────────────────

// TestProperty_Scorecard_ScoreAlwaysInRange verifies that Aplicar always returns
// a score in [0, 100] for any finite feature vector using the v1 feature names.
func TestProperty_Scorecard_ScoreAlwaysInRange(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadScorecard()
	if err != nil {
		t.Fatalf("LoadScorecard: %v", err)
	}

	// v1 feature names from scorecard.json
	featureNames := []string{
		"DIAS_SIN_PAGAR", "PAGOS_90D", "PCT_PAGOS_A_TIEMPO_6M",
		"CADENCIA_DIAS", "NUM_PAGOS_TOTAL", "ANTIGUEDAD_DIAS",
	}

	rapid.Check(t, func(t *rapid.T) {
		fv := make(map[string]float64)
		for _, name := range featureNames {
			fv[name] = rapid.Float64Range(-100, 100).Draw(t, name)
		}

		score, banda, drivers := sc.Aplicar(fv)

		if score.Int() < 0 || score.Int() > 100 {
			t.Fatalf("score out of [0,100]: %d (features: %v)", score.Int(), fv)
		}
		if !banda.IsValid() {
			t.Fatalf("invalid banda %q (score=%d)", banda, score.Int())
		}
		if len(drivers) > 3 {
			t.Fatalf("too many drivers: %d", len(drivers))
		}
	})
}

// TestProperty_Scorecard_RiskMonotonicity verifies:
// Increasing a risk-increasing feature (weight > 0) must not increase the score
// (risk doesn't go down). Decreasing it must not decrease the score.
// We use DIAS_SIN_PAGAR (weight=+1.451847 in v1) as the perturbed feature since
// its large positive weight clearly increases risk.
func TestProperty_Scorecard_RiskMonotonicity(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadScorecard()
	if err != nil {
		t.Fatalf("LoadScorecard: %v", err)
	}

	rapid.Check(t, func(t *rapid.T) {
		base := rapid.Float64Range(0.0, 100.0).Draw(t, "base_dias_sin_pagar")
		delta := rapid.Float64Range(0.01, 10.0).Draw(t, "delta")

		// DIAS_SIN_PAGAR has weight +1.451847 in v1 → increasing it increases logit → decreases score.
		featureName := "DIAS_SIN_PAGAR"

		baseFeatures := map[string]float64{featureName: base}
		higherFeatures := map[string]float64{featureName: base + delta}
		lowerFeatures := map[string]float64{featureName: base - delta}

		scoreBase, _, _ := sc.Aplicar(baseFeatures)
		scoreHigher, _, _ := sc.Aplicar(higherFeatures)
		scoreLower, _, _ := sc.Aplicar(lowerFeatures)

		// Increasing a risk-increasing feature must not raise the score.
		if scoreHigher.Int() > scoreBase.Int() {
			t.Fatalf(
				"monotonicity violated: increasing %s from %.4f to %.4f raised score %d → %d",
				featureName, base, base+delta, scoreBase.Int(), scoreHigher.Int(),
			)
		}

		// Decreasing a risk-increasing feature must not lower the score.
		if scoreLower.Int() < scoreBase.Int() {
			t.Fatalf(
				"monotonicity violated: decreasing %s from %.4f to %.4f lowered score %d → %d",
				featureName, base, base-delta, scoreBase.Int(), scoreLower.Int(),
			)
		}
	})
}

// ─── Fuzz test ────────────────────────────────────────────────────────────────

// FuzzAplicar verifies that Aplicar never panics and always returns a score in
// [0,100] for arbitrary float64 inputs including NaN and ±Inf.
// Non-finite inputs are treated as the training mean (z_i=0) so the sigmoid
// remains well-defined and no panic occurs.
func FuzzAplicar(f *testing.F) {
	// Seed corpus: a few representative v1 feature vectors.
	// Args: diasSinPagar, pagos90D, pctPagosATiempo, cadenciaDias, numPagosTotal, antiguedadDias
	f.Add(15.0, 7.0, 0.88, 20.0, 57.0, 800.0) // near-mean
	f.Add(0.0, 0.0, 0.0, 0.0, 0.0, 0.0)       // all-zero
	f.Add(90.0, 0.0, 0.0, 0.0, 2.0, 60.0)     // high risk: late, no recent payments
	f.Add(-1.0, -1.0, -1.0, -1.0, -1.0, -1.0) // negative values

	sc, err := app.LoadScorecard()
	if err != nil {
		f.Fatalf("LoadScorecard: %v", err)
	}

	f.Fuzz(func(t *testing.T, diasSinPagar, pagos90D, pctPagosATiempo, cadenciaDias, numPagosTotal, antiguedadDias float64) {
		features := map[string]float64{
			"DIAS_SIN_PAGAR":        diasSinPagar,
			"PAGOS_90D":             pagos90D,
			"PCT_PAGOS_A_TIEMPO_6M": pctPagosATiempo,
			"CADENCIA_DIAS":         cadenciaDias,
			"NUM_PAGOS_TOTAL":       numPagosTotal,
			"ANTIGUEDAD_DIAS":       antiguedadDias,
		}

		// Must not panic.
		score, banda, drivers := sc.Aplicar(features)

		if score.Int() < 0 || score.Int() > 100 {
			t.Errorf("score out of [0,100]: %d (inputs: %v %v %v %v %v %v)",
				score.Int(), diasSinPagar, pagos90D, pctPagosATiempo, cadenciaDias, numPagosTotal, antiguedadDias)
		}
		if !banda.IsValid() {
			t.Errorf("invalid banda %q", banda)
		}
		if len(drivers) > 3 {
			t.Errorf("too many drivers: %d", len(drivers))
		}
	})
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func isErrScorecardInvalido(err error) bool {
	return errors.Is(err, domain.ErrScorecardInvalido)
}

type scorecardOverride struct {
	emptyVersion  bool
	emptyFeatures bool
	bajoMin       int
	medioMin      int
	altoMin       int
}

// buildScorecardJSON builds a minimal valid scorecard JSON and applies overrides.
func buildScorecardJSON(t *testing.T, ov scorecardOverride) []byte {
	t.Helper()

	version := "v-test"
	if ov.emptyVersion {
		version = "" // allow empty to test validation
	}

	bajoMin := 75
	if ov.bajoMin != 0 {
		bajoMin = ov.bajoMin
	}
	medioMin := 50
	if ov.medioMin != 0 {
		medioMin = ov.medioMin
	}
	altoMin := 25
	if ov.altoMin != 0 {
		altoMin = ov.altoMin
	}

	type feature struct {
		Name   string  `json:"name"`
		Label  string  `json:"label"`
		Weight float64 `json:"weight"`
		Mean   float64 `json:"mean"`
		Std    float64 `json:"std"`
	}

	features := []feature{
		{Name: "F1", Label: "feature one", Weight: 1.0, Mean: 0.5, Std: 0.2},
	}
	if ov.emptyFeatures {
		features = []feature{}
	}

	payload := map[string]any{
		"version":   version,
		"intercept": -1.0,
		"features":  features,
		"bands": map[string]int{
			"bajo_min":  bajoMin,
			"medio_min": medioMin,
			"alto_min":  altoMin,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("buildScorecardJSON: %v", err)
	}
	return data
}
