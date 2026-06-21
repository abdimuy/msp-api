// Package app — scorecard.go implements the logistic-regression credit-risk
// scorecard applied online in Go. The model coefficients live in scorecard.json
// (embedded at compile time) and are trained offline (R4). v0-placeholder ships
// directionally-correct weights until the trainer produces a calibrated model.
//
//nolint:misspell // Spanish field names per project convention.
package app

import (
	_ "embed"
	"encoding/json"
	"math"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

//go:embed scorecard.json
var embeddedScorecardJSON []byte

// ─── JSON schema ──────────────────────────────────────────────────────────────

// scorecardJSON is the canonical JSON shape emitted by the R4 Python trainer
// and consumed here. Field names must match exactly (json tags are the contract).
type scorecardJSON struct {
	Version   string             `json:"version"`
	Intercept float64            `json:"intercept"`
	Features  []featureJSON      `json:"features"`
	Bands     bandThresholdsJSON `json:"bands"`
}

// featureJSON describes a single logistic-regression feature: its name (matches
// the key in the feature map passed to Aplicar), a human-readable Spanish label
// for display in risk-driver explanations, the logit weight, and the training-set
// mean/std used to standardise raw values before applying the weight.
type featureJSON struct {
	Name   string  `json:"name"`
	Label  string  `json:"label"`
	Weight float64 `json:"weight"`
	Mean   float64 `json:"mean"`
	Std    float64 `json:"std"`
}

// bandThresholdsJSON defines the score boundaries that map a numeric score to a
// BandaCredito. Bands are checked from highest score down:
//
//	score >= BajoMin  → BAJO
//	score >= MedioMin → MEDIO
//	score >= AltoMin  → ALTO
//	else              → CRITICO
type bandThresholdsJSON struct {
	BajoMin  int `json:"bajo_min"`
	MedioMin int `json:"medio_min"`
	AltoMin  int `json:"alto_min"`
}

// ─── Scorecard value object ───────────────────────────────────────────────────

// Scorecard is an immutable value object holding the parsed scorecard model.
// Construct it once at startup via LoadScorecard (uses the embedded JSON) or
// ParseScorecard (accepts raw bytes, useful in tests).
type Scorecard struct {
	raw scorecardJSON
}

// LoadScorecard constructs a Scorecard from the compile-time-embedded
// scorecard.json. Returns domain.ErrScorecardInvalido if the embedded data
// fails validation.
func LoadScorecard() (Scorecard, error) {
	return ParseScorecard(embeddedScorecardJSON)
}

// ParseScorecard constructs a Scorecard from raw JSON bytes. Validates structure
// and monotonicity of band thresholds. Returns domain.ErrScorecardInvalido on
// any structural error so callers can use errors.Is for typed handling.
func ParseScorecard(data []byte) (Scorecard, error) {
	var raw scorecardJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return Scorecard{}, domain.ErrScorecardInvalido
	}
	if err := validateScorecard(raw); err != nil {
		return Scorecard{}, err
	}
	return Scorecard{raw: raw}, nil
}

// validateScorecard checks structural constraints on a parsed scorecard.
func validateScorecard(raw scorecardJSON) error {
	if raw.Version == "" {
		return domain.ErrScorecardInvalido
	}
	if len(raw.Features) == 0 {
		return domain.ErrScorecardInvalido
	}
	return validateBands(raw.Bands)
}

// validateBands checks that band thresholds are within [0,100] and strictly
// monotonically decreasing (bajo_min > medio_min > alto_min).
func validateBands(b bandThresholdsJSON) error {
	outOfRange := b.BajoMin < 0 || b.BajoMin > 100 ||
		b.MedioMin < 0 || b.MedioMin > 100 ||
		b.AltoMin < 0 || b.AltoMin > 100
	if outOfRange {
		return domain.ErrScorecardInvalido
	}
	// bajo_min > medio_min > alto_min (ascending score = decreasing risk).
	if b.BajoMin <= b.MedioMin || b.MedioMin <= b.AltoMin {
		return domain.ErrScorecardInvalido
	}
	return nil
}

// Version returns the scorecard version string (e.g. "v0-placeholder").
func (sc Scorecard) Version() string { return sc.raw.Version }

// Loaded reports whether the scorecard was successfully parsed and is ready for
// scoring. A zero-value Scorecard{} (e.g. returned on load failure) returns false.
func (sc Scorecard) Loaded() bool { return sc.raw.Version != "" && len(sc.raw.Features) > 0 }

// ─── Aplicar ─────────────────────────────────────────────────────────────────

// Aplicar applies the scorecard to a feature vector and returns the credit-risk
// score, risk band, and up to three risk drivers (Spanish labels of features
// that push risk UP the most).
//
// Feature vector semantics:
//   - Keys are the feature names defined in the scorecard (e.g. "DIAS_SIN_PAGAR").
//   - Missing features are treated as their training mean (z_i = 0, no logit
//     contribution). This keeps the scorer robust to partial feature availability.
//   - Non-finite values (NaN, ±Inf) are treated as the training mean (z_i = 0)
//     so that fuzz inputs cannot break the range invariant.
//
// Score formula:
//
//	z_i   = (x_i - mean_i) / std_i   (std == 0 → z_i = 0)
//	logit = intercept + Σ weight_i * z_i
//	p_bad = 1 / (1 + exp(-logit))
//	score = round(100 * (1 - p_bad)), clamped to [0, 100]
//
// Drivers: the top-3 features with the largest positive logit contribution
// (weight_i * z_i > 0), sorted descending. These are the features that most
// increase the probability of default for this client.
//
// The returned ScoreCredito and BandaCredito are always valid. A construction
// error here indicates a programming error in the formula — the function panics
// rather than silently returning a zero value.
func (sc Scorecard) Aplicar(features map[string]float64) (domain.ScoreCredito, domain.BandaCredito, []string) {
	logit, contribs := sc.computeLogit(features)
	score := logitToScore(logit)
	banda := sc.scoreToBanda(score)
	drivers := topDrivers(positiveContribs(contribs), 3)
	return score, banda, drivers
}

// aplicarContribs applies the credit scorecard and returns the score, band, and
// EVERY feature's signed contribution (both risk-increasing and protective) so
// the razones layer can phrase drivers with the correct direction.
func (sc Scorecard) aplicarContribs(features map[string]float64) (domain.ScoreCredito, domain.BandaCredito, []featureContrib) {
	logit, contribs := sc.computeLogit(features)
	score := logitToScore(logit)
	banda := sc.scoreToBanda(score)
	return score, banda, contribs
}

// featureContrib holds a feature's name, Spanish label, the raw value used in
// scoring (the client's value, or the training mean when absent/non-finite),
// and its signed logit contribution. The name and valor support quantified
// driver phrasing (see razones.go); the sign of logit gives the driver direction.
type featureContrib struct {
	name  string
	label string
	valor float64
	logit float64
}

// computeLogit iterates the scorecard features in definition order, computes
// each standardised feature value z_i = (x - mean) / std (treating missing or
// non-finite values as z_i = 0), accumulates the logit, and collects EVERY
// feature's signed contribution (both risk-increasing and protective) so the
// razones layer can phrase drivers with the correct direction. Callers that
// only want risk drivers filter with positiveContribs.
func (sc Scorecard) computeLogit(features map[string]float64) (float64, []featureContrib) {
	return accumulateContribs(sc.raw.Intercept, sc.raw.Features, features)
}

// accumulateContribs is the shared logit accumulator used by both the credit
// and recompra scorecards. It sums intercept + Σ weight_i·z_i and returns every
// feature's signed contribution (name, label, raw value, signed logit).
func accumulateContribs(intercept float64, feats []featureJSON, features map[string]float64) (float64, []featureContrib) {
	logit := intercept
	contribs := make([]featureContrib, 0, len(feats))

	for _, f := range feats {
		val, zi := featureValZ(features, f)
		contrib := f.Weight * zi
		logit += contrib
		contribs = append(contribs, featureContrib{name: f.Name, label: f.Label, valor: val, logit: contrib})
	}
	return logit, contribs
}

// positiveContribs returns only the risk-increasing contributions (logit > 0).
func positiveContribs(contribs []featureContrib) []featureContrib {
	out := make([]featureContrib, 0, len(contribs))
	for _, c := range contribs {
		if c.logit > 0 {
			out = append(out, c)
		}
	}
	return out
}

// featureValZ computes the raw value used in scoring and the standardised value
// z_i for feature f given the raw feature vector. The returned value is the
// client's feature value, or the training mean when the feature is absent,
// non-finite, or std==0 (the same conditions under which z_i = 0).
func featureValZ(features map[string]float64, f featureJSON) (float64, float64) {
	val, ok := features[f.Name]
	if !ok || math.IsNaN(val) || math.IsInf(val, 0) || f.Std == 0 {
		return f.Mean, 0
	}
	return val, (val - f.Mean) / f.Std
}

// logitToScore converts a logit (log-odds of being a bad payer) to a score in
// [0, 100] where higher = better payer.
func logitToScore(logit float64) domain.ScoreCredito {
	pBad := 1.0 / (1.0 + math.Exp(-logit))
	n := int(math.Round(100.0 * (1.0 - pBad)))
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	// NewScoreCredito cannot fail for n in [0,100]; a panic here indicates a
	// programming error in the formula above.
	score, err := domain.NewScoreCredito(n)
	if err != nil {
		panic("analytics.scorecard: score out of [0,100] — programming error: " + err.Error())
	}
	return score
}

// scoreToBanda maps a ScoreCredito to a BandaCredito using the scorecard's
// configured thresholds.
func (sc Scorecard) scoreToBanda(score domain.ScoreCredito) domain.BandaCredito {
	n := score.Int()
	b := sc.raw.Bands
	switch {
	case n >= b.BajoMin:
		return domain.BandaCreditoBajo
	case n >= b.MedioMin:
		return domain.BandaCreditoMedio
	case n >= b.AltoMin:
		return domain.BandaCreditoAlto
	default:
		return domain.BandaCreditoCritico
	}
}

// topDrivers returns the labels of the top-n contributions sorted by descending
// logit contribution. Contributions with non-positive logit are excluded — they
// do not increase risk for this client.
func topDrivers(contribs []featureContrib, n int) []string {
	// Insertion sort (at most 4 features in v0 scorecard).
	for i := 1; i < len(contribs); i++ {
		for j := i; j > 0 && contribs[j].logit > contribs[j-1].logit; j-- {
			contribs[j], contribs[j-1] = contribs[j-1], contribs[j]
		}
	}
	if len(contribs) < n {
		n = len(contribs)
	}
	labels := make([]string, n)
	for i := range n {
		labels[i] = contribs[i].label
	}
	return labels
}
