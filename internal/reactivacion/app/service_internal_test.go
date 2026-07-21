//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_ControlPct(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 50, Config{}.controlPct(), "zero falls back to default")
	assert.Equal(t, 30, Config{ControlPct: 30}.controlPct())
	assert.Equal(t, 0, Config{ControlPct: -10}.controlPct(), "negative clamps to 0")
	assert.Equal(t, 100, Config{ControlPct: 250}.controlPct(), "over 100 clamps to 100")
	assert.Equal(t, 100, Config{ControlPct: 100}.controlPct())
}

func TestDeterministicControl_StableAndBounded(t *testing.T) {
	t.Parallel()
	// Stable: same input → same output across calls.
	for _, id := range []int{1, 24037, 999999} {
		first := deterministicControl(id, 50)
		assert.Equal(t, first, deterministicControl(id, 50))
	}
	// pct=0 → nobody is control; pct=100 → everybody is control.
	for _, id := range []int{1, 2, 3, 24037, 555} {
		assert.False(t, deterministicControl(id, 0))
		assert.True(t, deterministicControl(id, 100))
	}
}

func TestDeterministicControl_ApproximatelyHalf(t *testing.T) {
	t.Parallel()
	const n = 10000
	control := 0
	for id := 1; id <= n; id++ {
		if deterministicControl(id, 50) {
			control++
		}
	}
	// Expect roughly half; allow a wide tolerance band.
	assert.Greater(t, control, n*40/100)
	assert.Less(t, control, n*60/100)
}
