package domain

import "strconv"

// ScoreCredito is a validated integer in [0, 100] representing the credit-risk
// score of a client. Higher scores indicate a better payer and lower risk of
// default (FICO convention: 100 = excellent, 0 = critical risk). The app layer
// computes the raw score via a logistic-regression scorecard and wraps it here.
type ScoreCredito struct {
	value int
}

// NewScoreCredito constructs a ScoreCredito, returning ErrScoreCreditoFueraDeRango
// if n is outside [0, 100].
func NewScoreCredito(n int) (ScoreCredito, error) {
	if n < 0 || n > 100 {
		return ScoreCredito{}, ErrScoreCreditoFueraDeRango
	}
	return ScoreCredito{value: n}, nil
}

// Int returns the underlying integer value.
func (s ScoreCredito) Int() int { return s.value }

// String returns the score as a decimal string (e.g. "75").
func (s ScoreCredito) String() string { return strconv.Itoa(s.value) }
