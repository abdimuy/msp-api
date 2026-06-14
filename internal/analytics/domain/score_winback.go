package domain

import "strconv"

// ScoreWinback is a validated integer in [0, 100] representing how strongly a
// winback candidate should be prioritized for outreach. Higher scores indicate
// higher priority. The app layer computes the raw score and wraps it here.
type ScoreWinback struct {
	value int
}

// NewScoreWinback constructs a ScoreWinback, returning ErrScoreWinbackFueraDeRango
// if n is outside [0, 100].
func NewScoreWinback(n int) (ScoreWinback, error) {
	if n < 0 || n > 100 {
		return ScoreWinback{}, ErrScoreWinbackFueraDeRango
	}
	return ScoreWinback{value: n}, nil
}

// Int returns the underlying integer value.
func (s ScoreWinback) Int() int { return s.value }

// String returns the score as a decimal string (e.g. "75").
func (s ScoreWinback) String() string { return strconv.Itoa(s.value) }
