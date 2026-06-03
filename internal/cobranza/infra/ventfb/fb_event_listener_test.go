//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package ventfb_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// ─── fake clock ──────────────────────────────────────────────────────────────

// fakeClock allows tests to control backoff timing without real sleeps.
// It uses a context to ensure spawned goroutines exit cleanly at test end.
type fakeClock struct {
	mu      sync.Mutex
	trigger chan struct{} // buffered; each send unblocks one After call
	ticked  []time.Duration
	// done is closed by the test to unblock any still-waiting After goroutines.
	done chan struct{}
}

func newFakeClock() *fakeClock {
	return &fakeClock{
		trigger: make(chan struct{}, 64),
		done:    make(chan struct{}),
	}
}

// After records the duration and returns a channel that fires when tick() is
// called or when the clock is stopped (via stop()).
func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	f.ticked = append(f.ticked, d)
	f.mu.Unlock()

	ch := make(chan time.Time, 1)
	go func() {
		select {
		case <-f.trigger:
			ch <- time.Now()
		case <-f.done:
			// Test is finishing; unblock without firing the timer.
			ch <- time.Now()
		}
	}()
	return ch
}

// tick unblocks one pending After call.
func (f *fakeClock) tick() { f.trigger <- struct{}{} }

// stop unblocks all pending After goroutines so goleak is satisfied.
func (f *fakeClock) stop() {
	select {
	case <-f.done:
	default:
		close(f.done)
	}
}

// durations returns a snapshot of recorded durations.
func (f *fakeClock) durations() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]time.Duration, len(f.ticked))
	copy(out, f.ticked)
	return out
}

// ─── mock FbEventSource ──────────────────────────────────────────────────────

// mockSource is a controllable FbEventSource for unit tests.
type mockSource struct {
	mu sync.Mutex

	// subscribeResponses determines what Subscribe returns on the N-th call.
	// After the slice is exhausted, every call blocks indefinitely (simulate
	// a stable connection with no events).
	subscribeResponses []subscribeResponse

	// callCount tracks how many times Subscribe was called.
	callCount int

	// closed tracks whether Close was called.
	closed bool

	// unsubCalled tracks whether the unsubscribe func was called.
	unsubCalled int
}

type subscribeResponse struct {
	// If err is non-nil, Subscribe returns that error immediately.
	err error
	// events to push into the channel before closing it (nil = never close).
	events []outbound.FbEvent
	// closeAfter: if true, the channel is closed after events are drained.
	closeAfter bool
}

func (m *mockSource) Subscribe(topics []string) (<-chan outbound.FbEvent, func() error, error) {
	m.mu.Lock()
	idx := m.callCount
	m.callCount++
	var resp subscribeResponse
	if idx < len(m.subscribeResponses) {
		resp = m.subscribeResponses[idx]
	}
	m.mu.Unlock()

	if resp.err != nil {
		return nil, nil, resp.err
	}

	ch := make(chan outbound.FbEvent, max(len(resp.events)+1, 4))

	// Push events.
	for _, ev := range resp.events {
		ch <- ev
	}
	if resp.closeAfter {
		close(ch)
	}

	unsubscribe := func() error {
		m.mu.Lock()
		m.unsubCalled++
		m.mu.Unlock()
		return nil
	}
	return ch, unsubscribe, nil
}

func (m *mockSource) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockSource) subscribeCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func (m *mockSource) unsubscribeCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unsubCalled
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// newListener builds a FbEventListener wired with a mock source, bus, and options.
func newListener(src *mockSource, bus *eventbus.Bus, opts ...ventfb.Option) *ventfb.FbEventListener {
	return ventfb.NewFbEventListener(src, bus, nil, opts...)
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestFbEventListener_Start_Idempotent verifies that calling Start twice does
// not create two goroutines.
//
//nolint:paralleltest // test uses goroutine-based fake clock with shared state.
func TestFbEventListener_Start_Idempotent(t *testing.T) {
	defer goleak.VerifyNone(t)

	bus := eventbus.New()
	defer bus.Close()

	// Source that never closes the channel (stable connection).
	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{events: nil, closeAfter: false},
		},
	}
	l := newListener(src, bus)

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))
	require.NoError(t, l.Start(ctx)) // second call must be no-op

	// Only one Subscribe call should have happened.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, src.subscribeCallCount(), "Start must be idempotent")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestFbEventListener_Stop_WithoutStart verifies that Stop on a never-started
// listener is a safe no-op.
//
//nolint:paralleltest // test uses shared goroutine-leak checker.
func TestFbEventListener_Stop_WithoutStart(t *testing.T) {
	defer goleak.VerifyNone(t)

	bus := eventbus.New()
	defer bus.Close()

	src := &mockSource{}
	l := newListener(src, bus)

	ctx := context.Background()
	require.NoError(t, l.Stop(ctx))
}

// TestFbEventListener_PublishesToBus verifies that an event emitted by the
// mock source is forwarded to the bus subscriber within 100ms.
//
//nolint:paralleltest // test uses shared mock state.
func TestFbEventListener_PublishesToBus(t *testing.T) {
	defer goleak.VerifyNone(t)

	bus := eventbus.New()
	defer bus.Close()

	// Source emits one pagos_changed event then keeps channel open (no close).
	// We use closeAfter=false so the loop stays running and doesn't reconnect.
	// We send one event and then the channel stays open.
	eventCh := make(chan outbound.FbEvent, 4)
	eventCh <- outbound.FbEvent{Name: "pagos_changed", Count: 1}

	customSrc := &mockSource{}
	// Override Subscribe to return our channel.
	customSrc.subscribeResponses = []subscribeResponse{
		{events: []outbound.FbEvent{{Name: "pagos_changed", Count: 1}}, closeAfter: false},
	}

	subCh, unsub := bus.Subscribe("pagos_changed")
	defer unsub()

	l := newListener(customSrc, bus)
	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	select {
	case <-subCh:
		// signal received — pass
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected pagos_changed signal on bus within 100ms")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestFbEventListener_ReconnectBackoff verifies the backoff schedule advances
// correctly when Subscribe fails repeatedly.
//
//nolint:paralleltest // test uses fake clock with shared trigger channel.
func TestFbEventListener_ReconnectBackoff(t *testing.T) {
	defer goleak.VerifyNone(t)

	bus := eventbus.New()
	defer bus.Close()

	errSubscribe := assert.AnError

	// Fail first 3 subscribes, then succeed with a never-closing channel.
	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{err: errSubscribe},
			{err: errSubscribe},
			{err: errSubscribe},
			{events: nil, closeAfter: false}, // stable on 4th attempt
		},
	}

	fc := newFakeClock()
	defer fc.stop()
	schedule := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	l := newListener(src, bus,
		ventfb.WithClock(fc),
		ventfb.WithBackoffSchedule(schedule),
	)

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// Advance through the 3 backoff waits.
	for i := range 3 {
		// Wait until the listener is blocking on After.
		require.Eventually(t, func() bool {
			return len(fc.durations()) > i
		}, 2*time.Second, 5*time.Millisecond, "backoff %d not reached", i)
		fc.tick()
	}

	// Allow the 4th subscribe (success) to happen.
	require.Eventually(t, func() bool {
		return src.subscribeCallCount() >= 4
	}, 2*time.Second, 5*time.Millisecond, "4th subscribe not reached")

	// Verify backoff schedule was respected.
	durations := fc.durations()
	require.GreaterOrEqual(t, len(durations), 3)
	assert.Equal(t, 1*time.Second, durations[0], "attempt 0 backoff")
	assert.Equal(t, 2*time.Second, durations[1], "attempt 1 backoff")
	assert.Equal(t, 4*time.Second, durations[2], "attempt 2 backoff")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestFbEventListener_SyntheticPublishAfterReconnect verifies that when the
// event channel is closed (driver disconnect), the listener publishes synthetic
// signals for both topics before reopening the connection.
//
//nolint:paralleltest // test uses fake clock with shared trigger channel.
func TestFbEventListener_SyntheticPublishAfterReconnect(t *testing.T) {
	defer goleak.VerifyNone(t)

	bus := eventbus.New()
	defer bus.Close()

	// First subscribe: closeAfter=true simulates driver dropping the channel.
	// Second subscribe: stable (never closes) so the listener doesn't loop again.
	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{events: nil, closeAfter: true},  // disconnect
			{events: nil, closeAfter: false}, // reconnected
		},
	}

	fc := newFakeClock()
	defer fc.stop()
	l := newListener(src, bus,
		ventfb.WithClock(fc),
		ventfb.WithBackoffSchedule([]time.Duration{1 * time.Millisecond}),
	)

	pagosCh, unsubPagos := bus.Subscribe("pagos_changed")
	saldosCh, unsubSaldos := bus.Subscribe("saldos_changed")
	defer unsubPagos()
	defer unsubSaldos()

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// Trigger the backoff after the disconnect.
	require.Eventually(t, func() bool {
		return len(fc.durations()) > 0
	}, 2*time.Second, 5*time.Millisecond, "backoff not reached after disconnect")
	fc.tick()

	// Synthetic signals should arrive on both channels.
	recvPagos := func() bool {
		select {
		case <-pagosCh:
			return true
		default:
			return false
		}
	}
	recvSaldos := func() bool {
		select {
		case <-saldosCh:
			return true
		default:
			return false
		}
	}

	assert.Eventually(t, recvPagos, 2*time.Second, 5*time.Millisecond,
		"expected pagos_changed synthetic signal after reconnect")
	assert.Eventually(t, recvSaldos, 2*time.Second, 5*time.Millisecond,
		"expected saldos_changed synthetic signal after reconnect")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestFbEventListener_StopGracefully verifies that Start then Stop results in
// a clean shutdown: unsubscribe was called and no goroutines leak.
//
//nolint:paralleltest // test uses shared mock state.
func TestFbEventListener_StopGracefully(t *testing.T) {
	defer goleak.VerifyNone(t)

	bus := eventbus.New()
	defer bus.Close()

	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{events: nil, closeAfter: false},
		},
	}

	l := newListener(src, bus)
	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// Give goroutine time to subscribe.
	require.Eventually(t, func() bool {
		return src.subscribeCallCount() >= 1
	}, 2*time.Second, 5*time.Millisecond, "subscribe not called")

	stopCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))

	// Unsubscribe should have been called.
	assert.Equal(t, 1, src.unsubscribeCallCount(), "unsubscribe must be called on Stop")
}

// TestFbEventListener_Stop_DeadlineExceeded verifies that Stop returns the
// context error when the shutdown deadline is exceeded before the goroutine exits.
//
//nolint:paralleltest // test controls goroutine lifecycle explicitly.
func TestFbEventListener_Stop_DeadlineExceeded(t *testing.T) {
	// NOTE: goleak is NOT deferred here because we intentionally leave the
	// goroutine running until after the assertion. We clean it up explicitly.

	bus := eventbus.New()
	defer bus.Close()

	// blockCh is never closed during the test — it simulates a Subscribe call
	// that never produces events and never closes, keeping the loop goroutine
	// alive indefinitely inside drain().
	blockCh := make(chan outbound.FbEvent)
	customSrc := &blockingSource{ch: blockCh}

	l := ventfb.NewFbEventListener(customSrc, bus, nil)

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// Give goroutine time to reach drain().
	time.Sleep(20 * time.Millisecond)

	// Stop with an already-cancelled context so it times out immediately.
	alreadyCancelled, cancel := context.WithCancel(context.Background())
	cancel() // cancel before passing to Stop
	err := l.Stop(alreadyCancelled)
	require.ErrorIs(t, err, context.Canceled, "Stop must return context error when deadline exceeded")

	// Clean up the goroutine: close blockCh so drain() unblocks and the goroutine exits.
	close(blockCh)
	time.Sleep(50 * time.Millisecond)
	goleak.VerifyNone(t)
}

// blockingSource is a minimal FbEventSource that returns a channel the test
// controls. Subscribe never returns an error; Close is a no-op.
type blockingSource struct {
	ch chan outbound.FbEvent
}

func (b *blockingSource) Subscribe(_ []string) (<-chan outbound.FbEvent, func() error, error) {
	return b.ch, func() error { return nil }, nil
}

func (b *blockingSource) Close() error { return nil }

// TestFbEventListener_CtxCancelDuringBackoff verifies that when the context is
// cancelled while the listener is waiting in backoff, the loop exits cleanly
// without publishing synthetic events (ctx already done).
//
// This test uses a fake clock so we can verify the ctx.Done branch of
// waitBackoff is exercised. The fake clock's stop() is called explicitly
// before goleak to unblock any pending After goroutine.
//
//nolint:paralleltest // test uses fake clock with shared state; goleak called manually.
func TestFbEventListener_CtxCancelDuringBackoff(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	// Fail subscribe so the listener enters backoff, then never recover —
	// the test cancels ctx before the backoff fires.
	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{err: assert.AnError},
		},
	}

	fc := newFakeClock()
	l := newListener(src, bus,
		ventfb.WithClock(fc),
		ventfb.WithBackoffSchedule([]time.Duration{10 * time.Second}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, l.Start(ctx))

	// Wait until the listener is blocking on After.
	require.Eventually(t, func() bool {
		return len(fc.durations()) > 0
	}, 2*time.Second, 5*time.Millisecond, "listener did not reach backoff")

	// Cancel the context — unblocks waitBackoff's ctx.Done branch.
	cancel()

	// Wait for the listener goroutine to exit cleanly.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	require.NoError(t, l.Stop(stopCtx))

	// Stop the fake clock to unblock the pending After goroutine, then verify
	// no leaks. We yield briefly so the goroutine has time to exit.
	fc.stop()
	time.Sleep(20 * time.Millisecond)
	goleak.VerifyNone(t)
}

// TestFbEventListener_BackoffCapAtLastEntry verifies that once the attempt
// counter exceeds the backoff schedule length, the cap (last entry) is reused.
//
//nolint:paralleltest // test uses fake clock with shared trigger channel; goleak called manually.
func TestFbEventListener_BackoffCapAtLastEntry(t *testing.T) {
	defer goleak.VerifyNone(t)

	bus := eventbus.New()
	defer bus.Close()

	errSub := assert.AnError
	// Fail 4 subscribes (more than the 2-entry schedule) then succeed.
	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{err: errSub},
			{err: errSub},
			{err: errSub},
			{err: errSub},
			{events: nil, closeAfter: false},
		},
	}

	fc := newFakeClock()
	schedule := []time.Duration{1 * time.Second, 5 * time.Second} // cap is 5s
	l := newListener(src, bus,
		ventfb.WithClock(fc),
		ventfb.WithBackoffSchedule(schedule),
	)

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// Advance through 4 backoff waits.
	for i := range 4 {
		require.Eventually(t, func() bool {
			return len(fc.durations()) > i
		}, 2*time.Second, 5*time.Millisecond, "backoff %d not reached", i)
		fc.tick()
	}

	require.Eventually(t, func() bool {
		return src.subscribeCallCount() >= 5
	}, 2*time.Second, 5*time.Millisecond)

	durations := fc.durations()
	require.GreaterOrEqual(t, len(durations), 4)
	assert.Equal(t, 1*time.Second, durations[0], "attempt 0: first entry")
	assert.Equal(t, 5*time.Second, durations[1], "attempt 1: cap entry")
	assert.Equal(t, 5*time.Second, durations[2], "attempt 2: cap reused")
	assert.Equal(t, 5*time.Second, durations[3], "attempt 3: cap reused")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))

	fc.stop()
	goleak.VerifyNone(t)
}
