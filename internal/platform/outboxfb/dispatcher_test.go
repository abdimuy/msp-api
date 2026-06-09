package outboxfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
)

// probeLeakIgnores returns goleak options that suppress goroutines spawned by
// the shared Firebird pool and firebirdsql event-subscription infrastructure.
// These goroutines are process-lifetime and unrelated to the code under test.
func probeLeakIgnores() []goleak.Option {
	return []goleak.Option{
		// Shared *sql.DB lifecycle goroutines.
		goleak.IgnoreTopFunction("database/sql.(*DB).connectionCleaner"),
		goleak.IgnoreTopFunction("database/sql.(*DB).connectionOpener"),
		// firebirdsql event-subscription background workers.
		goleak.IgnoreTopFunction("github.com/nakagami/firebirdsql.(*FbEvent).run"),
		goleak.IgnoreTopFunction("github.com/nakagami/firebirdsql.newSubscription"),
		// Wire-channel read blocks on socket recv while the event subscription is alive.
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	}
}

// TestDispatcher_StartStop_NoEvents verifies that Start launches a goroutine
// and Stop cleanly waits for it to exit with no goroutine leaks.
//
//nolint:paralleltest
func TestDispatcher_StartStop_NoEvents(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	// nil pool is safe here because no tick will reach the DB — the dispatcher
	// just loops on the ticker and the stop channel.
	reg := outboxfb.NewHandlerRegistry()
	d := outboxfb.NewDispatcher(nil, reg, outboxfb.DispatcherConfig{
		PollInterval: 50 * time.Millisecond,
	})

	require.NoError(t, d.Start(context.Background()))

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, d.Stop(stopCtx))
}

// TestDispatcher_StopWithoutStart_IsSafe confirms that calling Stop on a
// dispatcher that was never started returns nil immediately without panicking.
func TestDispatcher_StopWithoutStart_IsSafe(t *testing.T) {
	t.Parallel()

	reg := outboxfb.NewHandlerRegistry()
	d := outboxfb.NewDispatcher(nil, reg, outboxfb.DispatcherConfig{})

	stopCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Calling Stop on a never-started Dispatcher must return nil immediately.
	require.NoError(t, d.Stop(stopCtx), "Stop on a never-started dispatcher must return nil")
}

// TestDispatcher_StopRespectsDeadline verifies that Stop returns ctx.Err()
// when the passed context is already cancelled before doneCh closes.
func TestDispatcher_StopRespectsDeadline(t *testing.T) {
	t.Parallel()

	reg := outboxfb.NewHandlerRegistry()
	d := outboxfb.NewDispatcher(nil, reg, outboxfb.DispatcherConfig{
		// Very long poll so the goroutine is parked on the ticker select.
		PollInterval: 24 * time.Hour,
	})

	require.NoError(t, d.Start(context.Background()))

	// Pass an already-cancelled context to Stop.
	alreadyCancelled, cancel := context.WithCancel(context.Background())
	cancel()

	err := d.Stop(alreadyCancelled)
	require.ErrorIs(t, err, context.Canceled,
		"Stop must return ctx.Err() when the stop deadline is exceeded")

	// Drain the goroutine after the test so we don't leak.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cleanupCancel()
	_ = d.Stop(cleanupCtx)
}
