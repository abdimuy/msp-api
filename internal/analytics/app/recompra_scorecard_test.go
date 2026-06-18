// Package app_test — recompra_scorecard_test.go tests the BG/BB + logistic-regression
// recompra propensity scorecard: load/parse validation, Aplicar correctness, and
// the NON-INVERTED convention (higher score = higher propensity).
//
//nolint:misspell // Spanish field names per project convention.
package app_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// ─── Load / Parse tests ───────────────────────────────────────────────────────

func TestLoadRecompraScorecard_EmbeddedIsValid(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadRecompraScorecard()
	if err != nil {
		t.Fatalf("LoadRecompraScorecard() unexpected error: %v", err)
	}
	if sc.Version() == "" {
		t.Error("Version() must not be empty")
	}
	if !sc.Loaded() {
		t.Error("Loaded() must be true after successful load")
	}
}

func TestParseRecompraScorecard_MalformedJSON(t *testing.T) {
	t.Parallel()

	_, err := app.ParseRecompraScorecard([]byte(`{not valid json`))
	if err == nil {
		t.Fatal("ParseRecompraScorecard(malformed): expected error, got nil")
	}
	if !errors.Is(err, domain.ErrRecompraScorecardInvalido) {
		t.Errorf("expected ErrRecompraScorecardInvalido, got %v", err)
	}
}

func TestParseRecompraScorecard_EmptyVersion(t *testing.T) {
	t.Parallel()

	data := buildRecompraScorecardJSON(t, recompraOverride{emptyVersion: true})
	_, err := app.ParseRecompraScorecard(data)
	if err == nil {
		t.Fatal("ParseRecompraScorecard(empty version): expected error, got nil")
	}
	if !errors.Is(err, domain.ErrRecompraScorecardInvalido) {
		t.Errorf("expected ErrRecompraScorecardInvalido, got %v", err)
	}
}

func TestParseRecompraScorecard_EmptyFeatures(t *testing.T) {
	t.Parallel()

	data := buildRecompraScorecardJSON(t, recompraOverride{emptyFeatures: true})
	_, err := app.ParseRecompraScorecard(data)
	if err == nil {
		t.Fatal("ParseRecompraScorecard(empty features): expected error, got nil")
	}
	if !errors.Is(err, domain.ErrRecompraScorecardInvalido) {
		t.Errorf("expected ErrRecompraScorecardInvalido, got %v", err)
	}
}

func TestParseRecompraScorecard_MediaMinEqualsAltaMin(t *testing.T) {
	t.Parallel()

	// media_min == alta_min violates strict ordering.
	data := buildRecompraScorecardJSON(t, recompraOverride{altaMin: 50, mediaMin: 50})
	_, err := app.ParseRecompraScorecard(data)
	if err == nil {
		t.Fatal("ParseRecompraScorecard(media==alta): expected error, got nil")
	}
	if !errors.Is(err, domain.ErrRecompraScorecardInvalido) {
		t.Errorf("expected ErrRecompraScorecardInvalido, got %v", err)
	}
}

func TestParseRecompraScorecard_MediaMinGreaterThanAltaMin(t *testing.T) {
	t.Parallel()

	// media_min > alta_min violates ordering.
	data := buildRecompraScorecardJSON(t, recompraOverride{altaMin: 30, mediaMin: 60})
	_, err := app.ParseRecompraScorecard(data)
	if err == nil {
		t.Fatal("ParseRecompraScorecard(media>alta): expected error, got nil")
	}
	if !errors.Is(err, domain.ErrRecompraScorecardInvalido) {
		t.Errorf("expected ErrRecompraScorecardInvalido, got %v", err)
	}
}

func TestParseRecompraScorecard_AltaMinOutOfRange(t *testing.T) {
	t.Parallel()

	data := buildRecompraScorecardJSON(t, recompraOverride{altaMin: 110, mediaMin: 22})
	_, err := app.ParseRecompraScorecard(data)
	if err == nil {
		t.Fatal("ParseRecompraScorecard(alta_min>100): expected error, got nil")
	}
	if !errors.Is(err, domain.ErrRecompraScorecardInvalido) {
		t.Errorf("expected ErrRecompraScorecardInvalido, got %v", err)
	}
}

func TestParseRecompraScorecard_MediaMinNegative(t *testing.T) {
	t.Parallel()

	data := buildRecompraScorecardJSON(t, recompraOverride{altaMin: 53, mediaMin: -1})
	_, err := app.ParseRecompraScorecard(data)
	if err == nil {
		t.Fatal("ParseRecompraScorecard(media_min<0): expected error, got nil")
	}
	if !errors.Is(err, domain.ErrRecompraScorecardInvalido) {
		t.Errorf("expected ErrRecompraScorecardInvalido, got %v", err)
	}
}

// ─── Aplicar convention tests (NON-INVERTED direction) ───────────────────────

// TestAplicarRecompra_Convention verifies the critical semantic: an active, frequent,
// recently-purchasing client scores HIGHER than a one-time long-lapsed buyer.
// This catches any inversion bug (using 1-sigmoid instead of sigmoid).
func TestAplicarRecompra_Convention(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadRecompraScorecard()
	if err != nil {
		t.Fatalf("LoadRecompraScorecard: %v", err)
	}

	// Active frequent buyer: low recency, high frequency, high p_alive, recent purchase.
	// RECENCIA_MESES=1 (low), FRECUENCIA_V=5 (high), BGBB_EXP_12M=2.0 (high),
	// BGBB_P_ALIVE=0.95 (high), PCT_PAGOS_A_TIEMPO=0.9 (good), MONETARY_LOG=9.2,
	// ANTIGUEDAD_MESES=48, DIAS_SIN_PAGAR=15.
	activeFeatures := map[string]float64{
		"RECENCIA_MESES":     1.0,
		"FRECUENCIA_V":       5.0,
		"BGBB_EXP_12M":       2.0,
		"BGBB_P_ALIVE":       0.95,
		"PCT_PAGOS_A_TIEMPO": 0.9,
		"MONETARY_LOG":       9.2,
		"ANTIGUEDAD_MESES":   48.0,
		"DIAS_SIN_PAGAR":     15.0,
	}

	// One-time lapsed buyer: high recency, zero frequency, low p_alive.
	// RECENCIA_MESES=36 (very high), FRECUENCIA_V=0, BGBB_EXP_12M=0.01,
	// BGBB_P_ALIVE=0.05, PCT_PAGOS_A_TIEMPO=0.2, MONETARY_LOG=7.0,
	// ANTIGUEDAD_MESES=48, DIAS_SIN_PAGAR=800.
	lapsedFeatures := map[string]float64{
		"RECENCIA_MESES":     36.0,
		"FRECUENCIA_V":       0.0,
		"BGBB_EXP_12M":       0.01,
		"BGBB_P_ALIVE":       0.05,
		"PCT_PAGOS_A_TIEMPO": 0.2,
		"MONETARY_LOG":       7.0,
		"ANTIGUEDAD_MESES":   48.0,
		"DIAS_SIN_PAGAR":     800.0,
	}

	activeScore, activeBanda, _ := sc.Aplicar(activeFeatures)
	lapsedScore, lapsedBanda, _ := sc.Aplicar(lapsedFeatures)

	t.Logf("active score=%d banda=%s", activeScore.Int(), activeBanda)
	t.Logf("lapsed score=%d banda=%s", lapsedScore.Int(), lapsedBanda)

	if activeScore.Int() <= lapsedScore.Int() {
		t.Errorf("convention violated: active score (%d) must be > lapsed score (%d) — check NON-INVERTED formula",
			activeScore.Int(), lapsedScore.Int())
	}

	if activeBanda != domain.BandaRecompraAlta {
		t.Errorf("active frequent buyer should be ALTA, got %q (score=%d)", activeBanda, activeScore.Int())
	}
	if lapsedBanda != domain.BandaRecompraBaja {
		t.Errorf("lapsed one-time buyer should be BAJA, got %q (score=%d)", lapsedBanda, lapsedScore.Int())
	}
}

func TestAplicarRecompra_ScoreInRange(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadRecompraScorecard()
	if err != nil {
		t.Fatalf("LoadRecompraScorecard: %v", err)
	}

	vectors := []map[string]float64{
		{},
		{"RECENCIA_MESES": 0.0, "FRECUENCIA_V": 0.0},
		{"RECENCIA_MESES": 60.0, "FRECUENCIA_V": 0.0, "BGBB_P_ALIVE": 0.0},
		{"RECENCIA_MESES": 1.0, "FRECUENCIA_V": 10.0, "BGBB_EXP_12M": 5.0, "BGBB_P_ALIVE": 0.99},
		{"DIAS_SIN_PAGAR": -100.0}, // extreme (guard against non-finite z)
		{"DIAS_SIN_PAGAR": 2000.0}, // extreme high
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

func TestAplicarRecompra_BandaMatchesScore(t *testing.T) {
	t.Parallel()

	sc, err := app.LoadRecompraScorecard()
	if err != nil {
		t.Fatalf("LoadRecompraScorecard: %v", err)
	}

	// The embedded recompra_scorecard.json has alta_min=53, media_min=22.
	vectors := []map[string]float64{
		{},
		{"RECENCIA_MESES": 1.0, "FRECUENCIA_V": 5.0, "BGBB_P_ALIVE": 0.95},
		{"RECENCIA_MESES": 60.0, "FRECUENCIA_V": 0.0, "BGBB_P_ALIVE": 0.01},
	}

	for _, fv := range vectors {
		score, banda, _ := sc.Aplicar(fv)
		n := score.Int()
		var expected domain.BandaRecompra
		switch {
		case n >= 53:
			expected = domain.BandaRecompraAlta
		case n >= 22:
			expected = domain.BandaRecompraMedia
		default:
			expected = domain.BandaRecompraBaja
		}
		if banda != expected {
			t.Errorf("score=%d: expected banda %q, got %q", n, expected, banda)
		}
	}
}

func TestRecompraScorecardLoaded_ZeroValue(t *testing.T) {
	t.Parallel()

	var sc app.RecompraScorecard
	if sc.Loaded() {
		t.Error("zero-value RecompraScorecard.Loaded() must be false")
	}
	if sc.Version() != "" {
		t.Errorf("zero-value RecompraScorecard.Version() must be empty, got %q", sc.Version())
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

type recompraOverride struct {
	emptyVersion  bool
	emptyFeatures bool
	altaMin       int
	mediaMin      int
}

// buildRecompraScorecardJSON builds a minimal valid recompra scorecard JSON and applies overrides.
func buildRecompraScorecardJSON(t *testing.T, ov recompraOverride) []byte {
	t.Helper()

	version := "v-test-recompra"
	if ov.emptyVersion {
		version = ""
	}

	altaMin := 53
	if ov.altaMin != 0 {
		altaMin = ov.altaMin
	}
	mediaMin := 22
	if ov.mediaMin != 0 {
		mediaMin = ov.mediaMin
	}

	type feature struct {
		Name   string  `json:"name"`
		Label  string  `json:"label"`
		Weight float64 `json:"weight"`
		Mean   float64 `json:"mean"`
		Std    float64 `json:"std"`
	}

	features := []feature{
		{Name: "RECENCIA_MESES", Label: "compró recientemente", Weight: -0.5, Mean: 25.0, Std: 19.0},
	}
	if ov.emptyFeatures {
		features = []feature{}
	}

	payload := map[string]any{
		"version":   version,
		"objetivo":  "recompra_12m",
		"intercept": -0.3,
		"features":  features,
		"bands": map[string]int{
			"alta_min":  altaMin,
			"media_min": mediaMin,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("buildRecompraScorecardJSON: %v", err)
	}
	return data
}
