// Package reliability wraps failsafe-go policies the codebase uses for
// outbound calls (Firebird/Microsip pulls, push retries, etc.).
package reliability

import (
	"errors"
	"time"

	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/retrypolicy"
)

// RetryConfig parameterizes a retry policy.
type RetryConfig struct {
	MaxAttempts int
	Backoff     time.Duration
	MaxBackoff  time.Duration
	Jitter      time.Duration
}

// DefaultRetry returns a sensible retry policy: 3 attempts with exponential
// backoff up to 5s and 200ms of jitter.
func DefaultRetry() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		Backoff:     200 * time.Millisecond,
		MaxBackoff:  5 * time.Second,
		Jitter:      200 * time.Millisecond,
	}
}

// NewRetry builds a generic retry policy. The error type is parameterised so
// callers preserve their own return types without unsafe casts.
func NewRetry[T any](cfg RetryConfig) retrypolicy.RetryPolicy[T] {
	if cfg.MaxAttempts <= 0 {
		cfg = DefaultRetry()
	}
	return retrypolicy.NewBuilder[T]().
		WithMaxAttempts(cfg.MaxAttempts).
		WithBackoff(cfg.Backoff, cfg.MaxBackoff).
		WithJitter(cfg.Jitter).
		Build()
}

// CircuitConfig parameterizes a circuit breaker.
type CircuitConfig struct {
	FailureThreshold uint
	SuccessThreshold uint
	Delay            time.Duration
}

// DefaultCircuit returns a sensible breaker config:
//
//	open after 5 failures in a row, half-open after 30s, close after 2 successes.
func DefaultCircuit() CircuitConfig {
	return CircuitConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Delay:            30 * time.Second,
	}
}

// NewCircuit builds a typed circuit breaker.
func NewCircuit[T any](cfg CircuitConfig) circuitbreaker.CircuitBreaker[T] {
	if cfg.FailureThreshold == 0 {
		cfg = DefaultCircuit()
	}
	return circuitbreaker.NewBuilder[T]().
		WithFailureThreshold(cfg.FailureThreshold).
		WithSuccessThreshold(cfg.SuccessThreshold).
		WithDelay(cfg.Delay).
		Build()
}

// IsCircuitOpen reports whether err comes from a circuit breaker rejection.
func IsCircuitOpen(err error) bool {
	return errors.Is(err, circuitbreaker.ErrOpen)
}
