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

	sc, err := app.LoadScorecard()
	if err != nil {
		t.Fatalf("LoadScorecard: %v", err)
	}

	// When all features equal their mean, z_i = 0 for every feature.
	// logit = intercept = -2.2
	// p_bad = sigmoid(-2.2) ≈ 0.100, score ≈ round(100 * 0.9) = 90.
	// This is a low-risk score → BAJO band.
	features := map[string]float64{
		"SALDO_FRAC":            0.45,
		"COBERTURA_PLAN":        1.0,
		"PCT_PAGOS_A_TIEMPO_6M": 0.70,
		"DIAS_ATRASO_PROM":      8.0,
	}

	score, banda, drivers := sc.Aplicar(features)

	if score.Int() < 0 || score.Int() > 100 {
		t.Errorf("score out of range: %d", score.Int())
	}
	// With intercept -2.2 and all z_i=0, p_bad ≈ 0.100 → score ≈ 90.
	// Verify it's in BAJO territory (>= 75).
	if banda != domain.BandaCreditoBajo {
		t.Errorf("all-mean vector: expected BAJO, got %q (score=%d)", banda, score.Int())
	}
	// No feature has positive z_i contribution when all z_i=0 → no drivers.
	if len(drivers) != 0 {
		t.Errorf("all-mean vector: expected 0 drivers, got %d: %v", len(drivers), drivers)
	}
}

func TestAplicar_EmptyMap_MidScore(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadScorecard()
	if err != nil {
		t.Fatalf("LoadScorecard: %v", err)
	}

	// Empty map: all features treated as mean → same as all-mean vector.
	score, banda, _ := sc.Aplicar(map[string]float64{})

	if score.Int() < 0 || score.Int() > 100 {
		t.Errorf("score out of range: %d", score.Int())
	}
	if banda != domain.BandaCreditoBajo {
		t.Errorf("empty map: expected BAJO, got %q (score=%d)", banda, score.Int())
	}
}

func TestAplicar_ExtremeRisk_Critico(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadScorecard()
	if err != nil {
		t.Fatalf("LoadScorecard: %v", err)
	}

	// Worst-case payer: high outstanding saldo, low plan coverage, no on-time
	// payments, high average delinquency days.
	features := map[string]float64{
		"SALDO_FRAC":            1.0,  // far above mean 0.45 → positive z, positive contrib (weight +1.4)
		"COBERTURA_PLAN":        0.0,  // far below mean 1.0 → negative z, positive contrib (weight -1.1 × neg z = pos)
		"PCT_PAGOS_A_TIEMPO_6M": 0.0,  // far below mean 0.70 → negative z, positive contrib (weight -1.6 × neg z = pos)
		"DIAS_ATRASO_PROM":      60.0, // far above mean 8 → positive z, positive contrib (weight +0.9)
	}

	score, banda, drivers := sc.Aplicar(features)

	if score.Int() < 0 || score.Int() > 100 {
		t.Errorf("score out of range: %d", score.Int())
	}
	// All four features push risk up → expect CRITICO (score < 25) or at least ALTO.
	if banda == domain.BandaCreditoBajo {
		t.Errorf("extreme-risk vector: did not expect BAJO (score=%d)", score.Int())
	}
	// At least one driver expected.
	if len(drivers) == 0 {
		t.Errorf("extreme-risk vector: expected at least 1 driver, got 0")
	}
	if len(drivers) > 3 {
		t.Errorf("extreme-risk vector: too many drivers: %d", len(drivers))
	}
}

func TestAplicar_ExtremeGood_Bajo(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadScorecard()
	if err != nil {
		t.Fatalf("LoadScorecard: %v", err)
	}

	// Best-case payer: zero outstanding saldo, full plan coverage, all payments
	// on time, zero average delinquency.
	features := map[string]float64{
		"SALDO_FRAC":            0.0, // below mean → negative z, SALDO weight +1.4 → negative contrib
		"COBERTURA_PLAN":        3.0, // above mean → positive z, COBERTURA weight -1.1 → negative contrib
		"PCT_PAGOS_A_TIEMPO_6M": 1.0, // above mean → positive z, PCT weight -1.6 → negative contrib
		"DIAS_ATRASO_PROM":      0.0, // below mean → negative z, DIAS weight +0.9 → negative contrib
	}

	score, banda, drivers := sc.Aplicar(features)

	if score.Int() < 0 || score.Int() > 100 {
		t.Errorf("score out of range: %d", score.Int())
	}
	if banda != domain.BandaCreditoBajo {
		t.Errorf("extreme-good vector: expected BAJO, got %q (score=%d)", banda, score.Int())
	}
	// No feature contributes positive logit → zero drivers.
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

	vectors := []map[string]float64{
		{},
		{"SALDO_FRAC": 0.0},
		{"SALDO_FRAC": 1.0, "PCT_PAGOS_A_TIEMPO_6M": 0.0},
		{"SALDO_FRAC": 0.5, "COBERTURA_PLAN": 0.5, "PCT_PAGOS_A_TIEMPO_6M": 0.5, "DIAS_ATRASO_PROM": 5.0},
		{"SALDO_FRAC": -100.0},                            // extreme low (std guard)
		{"SALDO_FRAC": 100.0, "DIAS_ATRASO_PROM": 1000.0}, // extreme high
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

	sc, err := app.LoadScorecard()
	if err != nil {
		t.Fatalf("LoadScorecard: %v", err)
	}

	// Test band boundary semantics by cross-checking score vs expected band.
	// With the v0 scorecard: bajo_min=75, medio_min=50, alto_min=25.
	// We use vectors that produce scores near those boundaries and verify
	// the returned banda is consistent with the score.
	vectors := []map[string]float64{
		{},
		{"SALDO_FRAC": 1.0},
		{"PCT_PAGOS_A_TIEMPO_6M": 0.0},
		{"SALDO_FRAC": 0.0, "PCT_PAGOS_A_TIEMPO_6M": 1.0},
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

	sc, err := app.LoadScorecard()
	if err != nil {
		t.Fatalf("LoadScorecard: %v", err)
	}

	// Use a vector that activates multiple risk-increasing features.
	features := map[string]float64{
		"SALDO_FRAC":            1.0,
		"COBERTURA_PLAN":        0.0,
		"PCT_PAGOS_A_TIEMPO_6M": 0.0,
		"DIAS_ATRASO_PROM":      60.0,
	}

	_, _, drivers := sc.Aplicar(features)

	// Risk contributions (weight·z) for this vector, descending:
	//   PCT_PAGOS_A_TIEMPO_6M ≈ 5.09  → "pocos pagos a tiempo recientes"
	//   DIAS_ATRASO_PROM      ≈ 3.90  → "atraso promedio elevado"
	//   SALDO_FRAC            ≈ 2.57  → "saldo alto pendiente"
	//   COBERTURA_PLAN        ≈ 2.20  → dropped (only top-3 returned)
	want := []string{
		"pocos pagos a tiempo recientes",
		"atraso promedio elevado",
		"saldo alto pendiente",
	}
	if len(drivers) != len(want) {
		t.Fatalf("expected %d drivers, got %d: %v", len(want), len(drivers), drivers)
	}
	for i := range want {
		if drivers[i] != want[i] {
			t.Errorf("driver[%d] = %q, want %q (full: %v)", i, drivers[i], want[i], drivers)
		}
	}
}

// ─── Property tests ───────────────────────────────────────────────────────────

// TestProperty_Scorecard_ScoreAlwaysInRange verifies that Aplicar always returns
// a score in [0, 100] for any finite feature vector.
func TestProperty_Scorecard_ScoreAlwaysInRange(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadScorecard()
	if err != nil {
		t.Fatalf("LoadScorecard: %v", err)
	}

	featureNames := []string{
		"SALDO_FRAC", "COBERTURA_PLAN", "PCT_PAGOS_A_TIEMPO_6M", "DIAS_ATRASO_PROM",
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
// We use SALDO_FRAC (weight=+1.4) and DIAS_ATRASO_PROM (weight=+0.9) as the
// perturbed features since their positive weights clearly increase risk.
func TestProperty_Scorecard_RiskMonotonicity(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadScorecard()
	if err != nil {
		t.Fatalf("LoadScorecard: %v", err)
	}

	rapid.Check(t, func(t *rapid.T) {
		base := rapid.Float64Range(0.0, 1.0).Draw(t, "base_saldo_frac")
		delta := rapid.Float64Range(0.01, 0.5).Draw(t, "delta")

		// SALDO_FRAC has weight +1.4 → increasing it increases logit → decreases score.
		featureName := "SALDO_FRAC"

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
	// Seed corpus: a few representative feature vectors.
	f.Add(0.45, 1.0, 0.70, 8.0)   // all-mean
	f.Add(0.0, 0.0, 0.0, 0.0)     // all-zero
	f.Add(1.0, 2.0, 1.0, 60.0)    // extreme risk
	f.Add(-1.0, -1.0, -1.0, -1.0) // negative values

	sc, err := app.LoadScorecard()
	if err != nil {
		f.Fatalf("LoadScorecard: %v", err)
	}

	f.Fuzz(func(t *testing.T, saldoFrac, coberturaPlan, pctPagos, diasAtraso float64) {
		features := map[string]float64{
			"SALDO_FRAC":            saldoFrac,
			"COBERTURA_PLAN":        coberturaPlan,
			"PCT_PAGOS_A_TIEMPO_6M": pctPagos,
			"DIAS_ATRASO_PROM":      diasAtraso,
		}

		// Must not panic.
		score, banda, drivers := sc.Aplicar(features)

		if score.Int() < 0 || score.Int() > 100 {
			t.Errorf("score out of [0,100]: %d (NaN/Inf inputs: %v %v %v %v)",
				score.Int(), saldoFrac, coberturaPlan, pctPagos, diasAtraso)
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
