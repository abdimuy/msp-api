package firebird

import (
	"context"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/retrypolicy"

	"github.com/abdimuy/msp-api/internal/platform/reliability"
)

// ExecRetry runs fn under the default retry policy, retrying ONLY when the
// returned error is transient (lock conflict, connection drop, IO error).
// Non-transient errors are returned immediately without retries. Use this
// around writes that may race with other writers; reads typically don't need it.
//
// The returned error is the mapped apperror — callers don't need to call
// MapError themselves if fn already does.
func (p *Pool) ExecRetry(ctx context.Context, fn func(ctx context.Context) error) error {
	cfg := reliability.DefaultRetry()
	policy := retrypolicy.NewBuilder[any]().
		WithMaxAttempts(cfg.MaxAttempts).
		WithBackoff(cfg.Backoff, cfg.MaxBackoff).
		WithJitter(cfg.Jitter).
		HandleIf(func(_ any, err error) bool { return IsTransient(err) }).
		Build()

	_, err := failsafe.With[any](policy).
		WithContext(ctx).
		GetWithExecution(func(_ failsafe.Execution[any]) (any, error) {
			return nil, fn(ctx)
		})
	return err
}
