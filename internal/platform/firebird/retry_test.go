package firebird

// White-box package so we can construct a zero-valued *Pool without needing
// a live Firebird connection. ExecRetry delegates entirely to the retry policy
// and the provided fn; it never touches *sql.DB directly.
// These tests are pure unit tests: no FIREBIRD=1 gate, no TestMain.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// zeroPool returns a *Pool whose *sql.DB is nil. This is safe for ExecRetry
// because that method never calls any *sql.DB methods — it only invokes fn.
func zeroPool() *Pool {
	return &Pool{}
}

// transientErr builds a mapped lock-conflict apperror that IsTransient returns
// true for, simulating a retryable Firebird error.
func transientErr() error {
	return apperror.NewConflict("firebird_lock_conflict",
		"operación bloqueada, intente de nuevo").
		WithSource("firebird")
}

// nonTransientErr builds a mapped unique-violation apperror that IsTransient
// returns false for.
func nonTransientErr() error {
	return apperror.NewConflict("firebird_unique_violation",
		"registro duplicado").
		WithSource("firebird")
}

func TestExecRetry_NoErrorReturnsNil(t *testing.T) {
	t.Parallel()

	pool := zeroPool()
	var calls atomic.Int64

	err := pool.ExecRetry(context.Background(), func(_ context.Context) error {
		calls.Add(1)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), calls.Load(), "fn must be called exactly once on success")
}

func TestExecRetry_RetriesTransient(t *testing.T) {
	t.Parallel()

	// Use a very short backoff so the test finishes quickly.
	// ExecRetry uses DefaultRetry (3 attempts, 200ms backoff) internally,
	// but since we're forcing the fn to succeed on the 3rd call the total
	// wait is 2 * ~200ms ≈ 400ms — acceptable in a short test.
	pool := zeroPool()
	var calls atomic.Int64

	err := pool.ExecRetry(context.Background(), func(_ context.Context) error {
		n := calls.Add(1)
		if n < 3 {
			return transientErr()
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, int64(3), calls.Load(), "fn must be retried until it succeeds on the 3rd call")
}

func TestExecRetry_SurfacesNonTransient(t *testing.T) {
	t.Parallel()

	pool := zeroPool()
	var calls atomic.Int64
	want := nonTransientErr()

	err := pool.ExecRetry(context.Background(), func(_ context.Context) error {
		calls.Add(1)
		return want
	})

	require.Error(t, err)
	assert.Equal(t, int64(1), calls.Load(), "fn must not be retried for non-transient errors")

	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_unique_violation", appErr.Code)
}

func TestExecRetry_RespectsMaxAttempts(t *testing.T) {
	t.Parallel()

	// DefaultRetry has MaxAttempts=3. The fn always returns a transient error
	// so ExecRetry must stop after 3 calls and return a transient error.
	pool := zeroPool()
	var calls atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := pool.ExecRetry(ctx, func(_ context.Context) error {
		calls.Add(1)
		return transientErr()
	})

	require.Error(t, err)
	assert.Equal(t, int64(3), calls.Load(), "fn must be called exactly MaxAttempts (3) times")
	assert.True(t, IsTransient(err), "returned error must still be transient after exhausting retries")
}
