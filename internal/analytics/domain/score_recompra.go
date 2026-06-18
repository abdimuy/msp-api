package domain

import "strconv"

// ScoreRecompra is a validated integer in [0, 100] representing the repurchase-
// propensity score of a client. Higher scores indicate a higher probability of
// making another purchase within the next 12 months. The app layer computes the
// raw score via the BG/BB + logistic-regression recompra scorecard and wraps it here.
type ScoreRecompra struct {
	value int
}

// NewScoreRecompra constructs a ScoreRecompra, returning ErrScoreRecompraFueraDeRango
// if n is outside [0, 100].
func NewScoreRecompra(n int) (ScoreRecompra, error) {
	if n < 0 || n > 100 {
		return ScoreRecompra{}, ErrScoreRecompraFueraDeRango
	}
	return ScoreRecompra{value: n}, nil
}

// Int returns the underlying integer value.
func (s ScoreRecompra) Int() int { return s.value }

// String returns the score as a decimal string (e.g. "75").
func (s ScoreRecompra) String() string { return strconv.Itoa(s.value) }
