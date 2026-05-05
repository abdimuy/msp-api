package reliability_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/reliability"
)

func TestDefaultRetry_HasBoundedAttempts(t *testing.T) {
	t.Parallel()
	cfg := reliability.DefaultRetry()
	assert.Equal(t, 3, cfg.MaxAttempts)
	assert.Greater(t, cfg.MaxBackoff, time.Duration(0))
}

func TestNewRetry_RetriesUntilMaxAttempts(t *testing.T) {
	t.Parallel()
	policy := reliability.NewRetry[int](reliability.RetryConfig{
		MaxAttempts: 3,
		Backoff:     time.Millisecond,
		MaxBackoff:  time.Millisecond * 5,
		Jitter:      0,
	})

	calls := 0
	_, err := failsafe.With[int](policy).Get(func() (int, error) {
		calls++
		return 0, errors.New("transient")
	})

	require.Error(t, err)
	assert.Equal(t, 3, calls, "retry must run exactly MaxAttempts times")
}

func TestNewRetry_StopsOnSuccess(t *testing.T) {
	t.Parallel()
	policy := reliability.NewRetry[int](reliability.RetryConfig{
		MaxAttempts: 5,
		Backoff:     time.Millisecond,
		MaxBackoff:  time.Millisecond * 5,
		Jitter:      0,
	})

	calls := 0
	got, err := failsafe.With[int](policy).Get(func() (int, error) {
		calls++
		if calls < 3 {
			return 0, errors.New("transient")
		}
		return 42, nil
	})

	require.NoError(t, err)
	assert.Equal(t, 42, got)
	assert.Equal(t, 3, calls)
}

func TestNewRetry_AppliesDefaultsWhenZero(t *testing.T) {
	t.Parallel()
	// Zero config triggers DefaultRetry inside NewRetry.
	policy := reliability.NewRetry[int](reliability.RetryConfig{})
	calls := 0
	_, _ = failsafe.With[int](policy).Get(func() (int, error) {
		calls++
		return 0, errors.New("x")
	})
	assert.Equal(t, 3, calls) // default MaxAttempts == 3
}

func TestDefaultCircuit_HasReasonableThresholds(t *testing.T) {
	t.Parallel()
	cfg := reliability.DefaultCircuit()
	assert.Positive(t, cfg.FailureThreshold)
	assert.Positive(t, cfg.SuccessThreshold)
	assert.Positive(t, cfg.Delay)
}

func TestNewCircuit_OpensAfterFailureThreshold(t *testing.T) {
	t.Parallel()
	breaker := reliability.NewCircuit[int](reliability.CircuitConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Delay:            time.Hour, // never auto-half-open during test
	})

	exec := failsafe.With[int](breaker)

	// 2 failures should trip the breaker.
	_, _ = exec.Get(func() (int, error) { return 0, errors.New("x") })
	_, _ = exec.Get(func() (int, error) { return 0, errors.New("x") })

	// Subsequent call short-circuits with a circuit-open error.
	_, err := exec.Get(func() (int, error) { return 1, nil })
	require.Error(t, err)
	assert.True(t, reliability.IsCircuitOpen(err), "expected circuit-open error")
}

func TestIsCircuitOpen_NotForRandomErrors(t *testing.T) {
	t.Parallel()
	assert.False(t, reliability.IsCircuitOpen(errors.New("plain")))
	assert.False(t, reliability.IsCircuitOpen(context.DeadlineExceeded))
}
