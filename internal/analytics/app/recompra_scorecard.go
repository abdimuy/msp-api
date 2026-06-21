// Package app — recompra_scorecard.go implements the logistic-regression
// repurchase-propensity scorecard applied online in Go. The model coefficients
// live in recompra_scorecard.json (embedded at compile time). The score is
// NON-INVERTED vs the credit scorecard: higher logit = higher propensity to
// repurchase = higher score.
//
//nolint:misspell // Spanish field names per project convention.
package app

import (
	_ "embed"
	"encoding/json"
	"math"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

//go:embed recompra_scorecard.json
var embeddedRecompraScorecardJSON []byte

// ─── JSON schema ──────────────────────────────────────────────────────────────

// recompraScorecardJSON is the canonical JSON shape of recompra_scorecard.json.
// Field names must match exactly (json tags are the contract).
type recompraScorecardJSON struct {
	Version   string            `json:"version"`
	Objetivo  string            `json:"objetivo"`
	Intercept float64           `json:"intercept"`
	Features  []featureJSON     `json:"features"`
	Bands     recompraBandsJSON `json:"bands"`
}

// recompraBandsJSON defines the two score boundaries that map a numeric score to
// a BandaRecompra. Bands are checked from highest score down:
//
//	score >= AltaMin  → ALTA
//	score >= MediaMin → MEDIA
//	else              → BAJA
type recompraBandsJSON struct {
	AltaMin  int `json:"alta_min"`
	MediaMin int `json:"media_min"`
}

// ─── RecompraScorecard value object ──────────────────────────────────────────

// RecompraScorecard is an immutable value object holding the parsed recompra
// propensity model. Construct it once at startup via LoadRecompraScorecard (uses
// the embedded JSON) or ParseRecompraScorecard (accepts raw bytes, useful in tests).
type RecompraScorecard struct {
	raw recompraScorecardJSON
}

// LoadRecompraScorecard constructs a RecompraScorecard from the compile-time-embedded
// recompra_scorecard.json. Returns domain.ErrRecompraScorecardInvalido if the
// embedded data fails validation.
func LoadRecompraScorecard() (RecompraScorecard, error) {
	return ParseRecompraScorecard(embeddedRecompraScorecardJSON)
}

// ParseRecompraScorecard constructs a RecompraScorecard from raw JSON bytes.
// Validates structure and band threshold ordering. Returns
// domain.ErrRecompraScorecardInvalido on any structural error.
func ParseRecompraScorecard(data []byte) (RecompraScorecard, error) {
	var raw recompraScorecardJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return RecompraScorecard{}, domain.ErrRecompraScorecardInvalido
	}
	if err := validateRecompraScorecard(raw); err != nil {
		return RecompraScorecard{}, err
	}
	return RecompraScorecard{raw: raw}, nil
}

// validateRecompraScorecard checks structural constraints on a parsed recompra scorecard.
func validateRecompraScorecard(raw recompraScorecardJSON) error {
	if raw.Version == "" {
		return domain.ErrRecompraScorecardInvalido
	}
	if len(raw.Features) == 0 {
		return domain.ErrRecompraScorecardInvalido
	}
	return validateRecompraBands(raw.Bands)
}

// validateRecompraBands checks that band thresholds are within [0,100] and that
// alta_min > media_min >= 0 (strictly ordered, ascending propensity).
func validateRecompraBands(b recompraBandsJSON) error {
	if b.AltaMin < 0 || b.AltaMin > 100 || b.MediaMin < 0 || b.MediaMin > 100 {
		return domain.ErrRecompraScorecardInvalido
	}
	// alta_min must be strictly greater than media_min.
	if b.AltaMin <= b.MediaMin {
		return domain.ErrRecompraScorecardInvalido
	}
	return nil
}

// Version returns the scorecard version string (e.g. "v1-recompra-20260617").
func (sc RecompraScorecard) Version() string { return sc.raw.Version }

// Loaded reports whether the scorecard was successfully parsed and is ready for
// scoring. A zero-value RecompraScorecard{} returns false.
func (sc RecompraScorecard) Loaded() bool {
	return sc.raw.Version != "" && len(sc.raw.Features) > 0
}

// ─── Aplicar ─────────────────────────────────────────────────────────────────

// Aplicar applies the recompra scorecard to a feature vector and returns the
// propensity score, propensity band, and up to three drivers (Spanish labels of
// features whose positive contribution RAISES repurchase propensity the most).
//
// Feature vector semantics:
//   - Keys are the feature names defined in the scorecard (e.g. "BGBB_EXP_12M").
//   - Missing features are treated as their training mean (z_i = 0, no logit
//     contribution). This keeps the scorer robust to partial feature availability.
//   - Non-finite values (NaN, ±Inf) are treated as the training mean (z_i = 0).
//
// Score formula (NON-INVERTED — higher score = higher propensity):
//
//	z_i     = (x_i - mean_i) / std_i   (std == 0 → z_i = 0)
//	logit   = intercept + Σ weight_i * z_i
//	p_repurchase = 1 / (1 + exp(-logit))
//	score   = round(100 * p_repurchase), clamped to [0, 100]
//
// Drivers: the top-3 features with the largest positive logit contribution
// (weight_i * z_i > 0), sorted descending. These are the features that most
// increase the probability of repurchase for this client.
func (sc RecompraScorecard) Aplicar(features map[string]float64) (domain.ScoreRecompra, domain.BandaRecompra, []string) {
	score, banda, contribs := sc.aplicarContribs(features)
	drivers := topDrivers(positiveContribs(contribs), 3)
	return score, banda, drivers
}

// aplicarContribs applies the recompra scorecard and returns the score, band,
// and EVERY feature's signed contribution (both propensity-raising and
// propensity-lowering) so the razones layer can phrase drivers by direction.
func (sc RecompraScorecard) aplicarContribs(features map[string]float64) (domain.ScoreRecompra, domain.BandaRecompra, []featureContrib) {
	logit, contribs := accumulateContribs(sc.raw.Intercept, sc.raw.Features, features)
	score := logitToRecompraScore(logit)
	banda := sc.scoreToBandaRecompra(score)
	return score, banda, contribs
}

// logitToRecompraScore converts a logit (log-odds of repurchasing) to a score in
// [0, 100] where higher = more likely to repurchase (NON-INVERTED vs credit).
func logitToRecompraScore(logit float64) domain.ScoreRecompra {
	pRepurchase := 1.0 / (1.0 + math.Exp(-logit))
	n := int(math.Round(100.0 * pRepurchase))
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	// NewScoreRecompra cannot fail for n in [0,100]; a panic here indicates a
	// programming error in the formula above.
	score, err := domain.NewScoreRecompra(n)
	if err != nil {
		panic("analytics.recompra_scorecard: score out of [0,100] — programming error: " + err.Error())
	}
	return score
}

// scoreToBandaRecompra maps a ScoreRecompra to a BandaRecompra using the
// scorecard's configured thresholds.
func (sc RecompraScorecard) scoreToBandaRecompra(score domain.ScoreRecompra) domain.BandaRecompra {
	n := score.Int()
	b := sc.raw.Bands
	switch {
	case n >= b.AltaMin:
		return domain.BandaRecompraAlta
	case n >= b.MediaMin:
		return domain.BandaRecompraMedia
	default:
		return domain.BandaRecompraBaja
	}
}
