//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// defaults used by buildCLVParamsJSON.
const (
	defaultMargin   = 0.528
	defaultHorizon  = 24
	defaultDiscount = 0.01
	defaultAltoMin  = 1225.94
	defaultMedioMin = 200.66
	defaultLGD      = 1.0
)

func TestLoadCLVParams_EmbeddedIsValid(t *testing.T) {
	t.Parallel()

	p, err := app.LoadCLVParams()
	require.NoError(t, err)
	assert.True(t, p.Loaded(), "Loaded() must be true after successful load")
	assert.Greater(t, p.Margin(), 0.0, "margin must be > 0")
	assert.LessOrEqual(t, p.Margin(), 1.0, "margin must be <= 1")
	assert.Positive(t, p.HorizonMonths(), "horizon_months must be > 0")
	assert.GreaterOrEqual(t, p.MonthlyDiscount(), 0.0, "monthly_discount must be >= 0")
	assert.Greater(t, p.AltoMinPesos(), p.MedioMinPesos(), "alto_min must be > medio_min")
	assert.GreaterOrEqual(t, p.MedioMinPesos(), 0.0, "medio_min must be >= 0")
	assert.GreaterOrEqual(t, p.LGD(), 0.0, "lgd must be >= 0")
	assert.LessOrEqual(t, p.LGD(), 1.0, "lgd must be <= 1")
}

func TestLoadCLVParams_EmbeddedValues(t *testing.T) {
	t.Parallel()

	p, err := app.LoadCLVParams()
	require.NoError(t, err)

	assert.InDelta(t, 0.528, p.Margin(), 1e-9, "margin mismatch")
	assert.Equal(t, 24, p.HorizonMonths(), "horizon_months mismatch")
	assert.InDelta(t, 0.00948879, p.MonthlyDiscount(), 1e-9, "monthly_discount mismatch")
	assert.InDelta(t, 1225.94, p.AltoMinPesos(), 1e-9, "alto_min mismatch")
	assert.InDelta(t, 200.66, p.MedioMinPesos(), 1e-9, "medio_min mismatch")
	assert.InDelta(t, 1.0, p.LGD(), 1e-9, "lgd mismatch")
}

func TestParseCLVParams_MalformedJSON(t *testing.T) {
	t.Parallel()

	_, err := app.ParseCLVParams([]byte(`{not valid json`))
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrCLVParamsInvalido)
}

func TestParseCLVParams_MarginZero(t *testing.T) {
	t.Parallel()

	data := buildCLVParamsJSON(t, 0.0, defaultHorizon, defaultDiscount, defaultAltoMin, defaultMedioMin, defaultLGD)
	_, err := app.ParseCLVParams(data)
	require.Error(t, err, "margin=0 must fail validation")
	assert.ErrorIs(t, err, domain.ErrCLVParamsInvalido)
}

func TestParseCLVParams_MarginGreaterThanOne(t *testing.T) {
	t.Parallel()

	data := buildCLVParamsJSON(t, 1.01, defaultHorizon, defaultDiscount, defaultAltoMin, defaultMedioMin, defaultLGD)
	_, err := app.ParseCLVParams(data)
	require.Error(t, err, "margin>1 must fail validation")
	assert.ErrorIs(t, err, domain.ErrCLVParamsInvalido)
}

func TestParseCLVParams_HorizonZero(t *testing.T) {
	t.Parallel()

	data := buildCLVParamsJSON(t, defaultMargin, 0, defaultDiscount, defaultAltoMin, defaultMedioMin, defaultLGD)
	_, err := app.ParseCLVParams(data)
	require.Error(t, err, "horizon_months=0 must fail validation")
	assert.ErrorIs(t, err, domain.ErrCLVParamsInvalido)
}

func TestParseCLVParams_HorizonNegative(t *testing.T) {
	t.Parallel()

	data := buildCLVParamsJSON(t, defaultMargin, -1, defaultDiscount, defaultAltoMin, defaultMedioMin, defaultLGD)
	_, err := app.ParseCLVParams(data)
	require.Error(t, err, "horizon_months<0 must fail validation")
	assert.ErrorIs(t, err, domain.ErrCLVParamsInvalido)
}

func TestParseCLVParams_AltoMinEqualsMedioMin(t *testing.T) {
	t.Parallel()

	data := buildCLVParamsJSON(t, defaultMargin, defaultHorizon, defaultDiscount, 500.0, 500.0, defaultLGD)
	_, err := app.ParseCLVParams(data)
	require.Error(t, err, "alto_min == medio_min must fail validation")
	assert.ErrorIs(t, err, domain.ErrCLVParamsInvalido)
}

func TestParseCLVParams_AltoMinLessThanMedioMin(t *testing.T) {
	t.Parallel()

	data := buildCLVParamsJSON(t, defaultMargin, defaultHorizon, defaultDiscount, 100.0, 500.0, defaultLGD)
	_, err := app.ParseCLVParams(data)
	require.Error(t, err, "alto_min < medio_min must fail validation")
	assert.ErrorIs(t, err, domain.ErrCLVParamsInvalido)
}

func TestParseCLVParams_LGDGreaterThanOne(t *testing.T) {
	t.Parallel()

	data := buildCLVParamsJSON(t, defaultMargin, defaultHorizon, defaultDiscount, defaultAltoMin, defaultMedioMin, 1.01)
	_, err := app.ParseCLVParams(data)
	require.Error(t, err, "lgd>1 must fail validation")
	assert.ErrorIs(t, err, domain.ErrCLVParamsInvalido)
}

func TestParseCLVParams_LGDNegative(t *testing.T) {
	t.Parallel()

	data := buildCLVParamsJSON(t, defaultMargin, defaultHorizon, defaultDiscount, defaultAltoMin, defaultMedioMin, -0.1)
	_, err := app.ParseCLVParams(data)
	require.Error(t, err, "lgd<0 must fail validation")
	assert.ErrorIs(t, err, domain.ErrCLVParamsInvalido)
}

func TestParseCLVParams_MonthlyDiscountNegative(t *testing.T) {
	t.Parallel()

	data := buildCLVParamsJSON(t, defaultMargin, defaultHorizon, -0.01, defaultAltoMin, defaultMedioMin, defaultLGD)
	_, err := app.ParseCLVParams(data)
	require.Error(t, err, "monthly_discount<0 must fail validation")
	assert.ErrorIs(t, err, domain.ErrCLVParamsInvalido)
}

func TestCLVParams_ZeroValue_LoadedFalse(t *testing.T) {
	t.Parallel()

	var p app.CLVParams
	assert.False(t, p.Loaded(), "zero-value CLVParams must have Loaded()==false")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// buildCLVParamsJSON builds a CLV params JSON payload with the given values.
// All arguments are passed directly — no zero-sentinel logic.
func buildCLVParamsJSON(t *testing.T, margin float64, horizonMonths int, monthlyDiscount, altoMin, medioMin, lgd float64) []byte {
	t.Helper()

	payload := map[string]any{
		"version":          "v-test",
		"margin":           margin,
		"horizon_months":   horizonMonths,
		"monthly_discount": monthlyDiscount,
		"gamma_gamma":      map[string]float64{"p": 7.35, "q": 14.74, "v": 11712.37},
		"bands_pesos": map[string]float64{
			"alto_min":  altoMin,
			"medio_min": medioMin,
		},
		"riesgo": map[string]float64{
			"lgd": lgd,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("buildCLVParamsJSON: %v", err)
	}
	return data
}
