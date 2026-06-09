package firebird_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	idempotencyfb "github.com/abdimuy/msp-api/internal/platform/idempotency/firebird"
)

// ---------------------------------------------------------------------------
// fakePurger — in-memory purger for Janitor unit tests
// ---------------------------------------------------------------------------

// fakePurger satisfies the unexported purger interface via the concrete *Store
// method surface. We use an atomic counter and a buffered channel so tests can
// observe calls without data races.
type fakePurger struct {
	calls    atomic.Int64
	errFirst bool          // return error on the first call, then nil
	errCount atomic.Int64  // number of error calls issued so far
	ch       chan struct{} // signalled after each PurgeExpired call
}

func newFakePurger() *fakePurger {
	return &fakePurger{ch: make(chan struct{}, 64)}
}

func (f *fakePurger) PurgeExpired(_ context.Context, _ time.Time) (int64, error) {
	f.calls.Add(1)
	defer func() { f.ch <- struct{}{} }()

	if f.errFirst && f.errCount.Add(1) == 1 {
		return 0, assert.AnError
	}
	return 1, nil
}

// waitForPurge blocks until a PurgeExpired call completes or ctx is done.
func (f *fakePurger) waitForPurge(ctx context.Context) bool {
	select {
	case <-f.ch:
		return true
	case <-ctx.Done():
		return false
	}
}

// ---------------------------------------------------------------------------
// goleak helper
// ---------------------------------------------------------------------------

// janitorLeakIgnores suppresses goroutines that are process-lifetime and
// unrelated to the code under test (database/sql connection pool threads and
// firebirdsql internal pollers).
func janitorLeakIgnores() []goleak.Option {
	return []goleak.Option{
		goleak.IgnoreTopFunction("database/sql.(*DB).connectionCleaner"),
		goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"),
		goleak.IgnoreTopFunction("github.com/nakagami/firebirdsql.(*FbEvent).run"),
		goleak.IgnoreTopFunction("github.com/nakagami/firebirdsql.newSubscription"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestJanitor_StartStop_NoLeaks verifies that a started Janitor leaves no
// goroutines running after Stop returns.
//
//nolint:paralleltest // goleak.VerifyNone must run after t.Cleanup; not parallel-safe
func TestJanitor_StartStop_NoLeaks(t *testing.T) {
	defer goleak.VerifyNone(t, janitorLeakIgnores()...)

	fp := newFakePurger()
	j := idempotencyfb.NewJanitor(idempotencyfb.JanitorConfig{
		Store:    fp,
		Interval: time.Hour,
	})

	require.NoError(t, j.Start(context.Background()))

	// Wait for the boot purge so we know the goroutine reached the select loop.
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	fp.waitForPurge(waitCtx)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, j.Stop(stopCtx))
}

// TestJanitor_PurgesOnTick verifies that the Janitor fires purgeOnce at boot
// and then again on every ticker interval.
func TestJanitor_PurgesOnTick(t *testing.T) {
	t.Parallel()

	fp := newFakePurger()
	j := idempotencyfb.NewJanitor(idempotencyfb.JanitorConfig{
		Store:    fp,
		Interval: 40 * time.Millisecond,
	})

	require.NoError(t, j.Start(context.Background()))

	// Wait for at least 2 purge signals (boot + ≥1 tick).
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	fp.waitForPurge(waitCtx) // boot
	fp.waitForPurge(waitCtx) // first tick

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, j.Stop(stopCtx))

	assert.GreaterOrEqual(t, fp.calls.Load(), int64(2),
		"janitor must fire purge at boot and at least once on tick")
}

// TestJanitor_StopWithoutStart_IsSafe verifies that Stop on an unstarted Janitor
// returns nil immediately without panicking.
func TestJanitor_StopWithoutStart_IsSafe(t *testing.T) {
	t.Parallel()

	fp := newFakePurger()
	j := idempotencyfb.NewJanitor(idempotencyfb.JanitorConfig{
		Store:    fp,
		Interval: time.Hour,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.NoError(t, j.Stop(ctx))
}

// TestJanitor_StartIsIdempotent verifies that calling Start twice does not
// launch a second goroutine. goleak confirms only one goroutine is running.
//
//nolint:paralleltest // goleak.VerifyNone must run after t.Cleanup; not parallel-safe
func TestJanitor_StartIsIdempotent(t *testing.T) {
	defer goleak.VerifyNone(t, janitorLeakIgnores()...)

	fp := newFakePurger()
	j := idempotencyfb.NewJanitor(idempotencyfb.JanitorConfig{
		Store:    fp,
		Interval: time.Hour,
	})

	ctx := context.Background()
	require.NoError(t, j.Start(ctx))
	require.NoError(t, j.Start(ctx)) // second call must be a no-op

	// Wait for the single boot purge.
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	fp.waitForPurge(waitCtx)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, j.Stop(stopCtx))

	// Only one goroutine → exactly one boot purge.
	assert.Equal(t, int64(1), fp.calls.Load(),
		"idempotent Start must not launch a second goroutine")
}

// TestJanitor_PurgeError_LoopsKeepGoing verifies that a purge error on the
// first call does not stop the janitor from firing on subsequent ticks.
func TestJanitor_PurgeError_LoopsKeepGoing(t *testing.T) {
	t.Parallel()

	fp := newFakePurger()
	fp.errFirst = true // first PurgeExpired call returns an error

	j := idempotencyfb.NewJanitor(idempotencyfb.JanitorConfig{
		Store:    fp,
		Interval: 30 * time.Millisecond,
	})

	require.NoError(t, j.Start(context.Background()))

	// Wait for two purge calls: the first (error) and the second (success).
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	fp.waitForPurge(waitCtx) // boot — returns error
	fp.waitForPurge(waitCtx) // first tick — returns success

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, j.Stop(stopCtx))

	assert.GreaterOrEqual(t, fp.calls.Load(), int64(2),
		"janitor must keep looping after a purge error")
}
