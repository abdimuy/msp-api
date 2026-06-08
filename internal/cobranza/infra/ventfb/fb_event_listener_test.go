//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package ventfb_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"pgregory.net/rapid"

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

// ─── mock changelog repos ─────────────────────────────────────────────────────

type sinceCall struct {
	SinceSeq, Watermark int64
	Limit               int
}

type mockChangelogRepo struct {
	mu         sync.Mutex
	sinceFn    func(ctx context.Context, sinceSeq, watermark int64, limit int) ([]outbound.ChangelogEntry, error)
	delFn      func(ctx context.Context, cutoff time.Time, maxDelete int) (int, error)
	maxFn      func(ctx context.Context, watermark int64) (int64, error)
	sinceCalls []sinceCall
	maxCalls   []int64 // watermark args passed to MaxSeqID
}

func (m *mockChangelogRepo) Since(ctx context.Context, sinceSeq, watermark int64, limit int) ([]outbound.ChangelogEntry, error) {
	m.mu.Lock()
	m.sinceCalls = append(m.sinceCalls, sinceCall{sinceSeq, watermark, limit})
	fn := m.sinceFn
	m.mu.Unlock()
	if fn == nil {
		return nil, nil
	}
	return fn(ctx, sinceSeq, watermark, limit)
}

func (m *mockChangelogRepo) DeleteOlderThan(ctx context.Context, cutoff time.Time, n int) (int, error) {
	m.mu.Lock()
	fn := m.delFn
	m.mu.Unlock()
	if fn == nil {
		return 0, nil
	}
	return fn(ctx, cutoff, n)
}

func (m *mockChangelogRepo) MaxSeqID(ctx context.Context, watermark int64) (int64, error) {
	m.mu.Lock()
	m.maxCalls = append(m.maxCalls, watermark)
	fn := m.maxFn
	m.mu.Unlock()
	if fn == nil {
		return 0, nil
	}
	return fn(ctx, watermark)
}

func (m *mockChangelogRepo) getSinceCalls() []sinceCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]sinceCall, len(m.sinceCalls))
	copy(out, m.sinceCalls)
	return out
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// noProbeInterval disables the watermark-probe loop in tests that do not
// exercise it. This prevents the probe goroutine from firing unexpectedly and
// interfering with goleak or unrelated Since-call counts.
const noProbeInterval = 24 * time.Hour

// newListener builds a FbEventListener wired with a mock source, bus, mock
// changelog repos, a fixed watermark probe, and any extra options.
// The watermark-probe loop interval is set to 24h so it never fires during a
// typical unit test. Tests that explicitly exercise the probe pass their own
// WithWatermarkProbeInterval option or call probeWatermarkOnce directly.
func newListener(src *mockSource, bus *eventbus.Bus, opts ...ventfb.Option) *ventfb.FbEventListener {
	pagos := &mockChangelogRepo{}
	saldos := &mockChangelogRepo{}
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		return ventfb.SentinelNoActiveTx, nil
	})
	allOpts := append([]ventfb.Option{
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval),
	}, opts...)
	return ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil, allOpts...)
}

// newListenerWithRepos is like newListener but also returns the mock repos so
// tests can set their functions and inspect their calls.
func newListenerWithRepos(
	src *mockSource,
	bus *eventbus.Bus,
	opts ...ventfb.Option,
) (*ventfb.FbEventListener, *mockChangelogRepo, *mockChangelogRepo) {
	pagos := &mockChangelogRepo{}
	saldos := &mockChangelogRepo{}
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		return ventfb.SentinelNoActiveTx, nil
	})
	allOpts := append([]ventfb.Option{
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval),
	}, opts...)
	l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil, allOpts...)
	return l, pagos, saldos
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestFbEventListener_Start_Idempotent verifies that calling Start twice does
// not create two goroutines.
//
//nolint:paralleltest // test uses goroutine-based fake clock with shared state.
func TestFbEventListener_Start_Idempotent(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

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
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

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
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	customSrc := &mockSource{}
	// Source emits one pagos_changed event then keeps channel open (no close).
	customSrc.subscribeResponses = []subscribeResponse{
		{events: []outbound.FbEvent{{Name: "pagos_changed", Count: 1}}, closeAfter: false},
	}

	// The pagos repo returns one entry so the listener publishes real IDs.
	pagos := &mockChangelogRepo{
		sinceFn: func(_ context.Context, _, _ int64, _ int) ([]outbound.ChangelogEntry, error) {
			return []outbound.ChangelogEntry{{SeqID: 1, PK: 42}}, nil
		},
	}
	saldos := &mockChangelogRepo{}
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		return ventfb.SentinelNoActiveTx, nil
	})

	l := ventfb.NewFbEventListener(customSrc, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval))

	subCh, unsub := bus.Subscribe("pagos_changed")
	defer unsub()

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
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

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
	l := newListener(
		src, bus,
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
// []int{} signals for both topics before reopening the connection.
//
//nolint:paralleltest // test uses fake clock with shared trigger channel.
func TestFbEventListener_SyntheticPublishAfterReconnect(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

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
	l := newListener(
		src, bus,
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
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

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

	pagos := &mockChangelogRepo{}
	saldos := &mockChangelogRepo{}
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		return ventfb.SentinelNoActiveTx, nil
	})
	l := ventfb.NewFbEventListener(customSrc, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval))

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
	goleak.VerifyNone(t, probeLeakIgnores()...)
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
	l := newListener(
		src, bus,
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
	goleak.VerifyNone(t, probeLeakIgnores()...)
}

// TestFbEventListener_BackoffCapAtLastEntry verifies that once the attempt
// counter exceeds the backoff schedule length, the cap (last entry) is reused.
//
//nolint:paralleltest // test uses fake clock with shared trigger channel; goleak called manually.
func TestFbEventListener_BackoffCapAtLastEntry(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

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
	l := newListener(
		src, bus,
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
	goleak.VerifyNone(t, probeLeakIgnores()...)
}

// ─── Changelog-path tests ─────────────────────────────────────────────────────

// TestListener_OnPostEvent_QueriesChangelogWithLastSeen verifies that on a
// pagos_changed event the listener calls Since(0, watermark, 500), publishes
// the returned PKs, and advances lastSeenSeq to 20.
//
//nolint:paralleltest
func TestListener_OnPostEvent_QueriesChangelogWithLastSeen(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{
				events:     []outbound.FbEvent{{Name: "pagos_changed", Count: 1}},
				closeAfter: false,
			},
		},
	}

	const wm = int64(9999)
	var sinceCalled sync.Once
	published := make(chan []int, 1)

	pagos := &mockChangelogRepo{
		sinceFn: func(_ context.Context, sinceSeq, watermark int64, limit int) ([]outbound.ChangelogEntry, error) {
			_ = sinceSeq
			_ = watermark
			_ = limit
			sinceCalled.Do(func() {
				published <- nil // signal that Since was called; we'll validate via sinceCalls
			})
			return []outbound.ChangelogEntry{
				{SeqID: 10, PK: 101},
				{SeqID: 20, PK: 202},
			}, nil
		},
	}
	saldos := &mockChangelogRepo{}
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) { return wm, nil })

	subCh, unsub := bus.Subscribe("pagos_changed")
	defer unsub()

	l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval))

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// Wait for the bus publish.
	var ids []int
	select {
	case ids = <-subCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected pagos_changed publish within 500ms")
	}

	assert.ElementsMatch(t, []int{101, 202}, ids)

	// Verify Since was called with sinceSeq=0 (initial cursor after MaxSeqID=0),
	// watermark=wm, limit=500.
	calls := pagos.getSinceCalls()
	require.Len(t, calls, 1, "Since must be called exactly once per event")
	assert.Equal(t, int64(0), calls[0].SinceSeq, "sinceSeq must start at 0 (MaxSeqID returned 0)")
	assert.Equal(t, wm, calls[0].Watermark, "watermark must be from probe")
	assert.Equal(t, 500, calls[0].Limit, "limit must be 500")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestListener_AdvancesLastSeenToMaxReturned verifies that the cursor advances
// to max(SeqID) of returned entries, not beyond.
//
//nolint:paralleltest
func TestListener_AdvancesLastSeenToMaxReturned(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	// Send two events so we can see the cursor advance on the second call.
	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{
				events: []outbound.FbEvent{
					{Name: "pagos_changed", Count: 1},
					{Name: "pagos_changed", Count: 1},
				},
				closeAfter: false,
			},
		},
	}

	callNum := 0
	var mu sync.Mutex
	var secondSinceSeq int64

	pagos := &mockChangelogRepo{
		sinceFn: func(_ context.Context, sinceSeq, _ int64, _ int) ([]outbound.ChangelogEntry, error) {
			mu.Lock()
			n := callNum
			callNum++
			mu.Unlock()
			if n == 0 {
				return []outbound.ChangelogEntry{
					{SeqID: 10, PK: 1},
					{SeqID: 20, PK: 2},
					{SeqID: 30, PK: 3},
				}, nil
			}
			// Second call: capture sinceSeq to assert it advanced to 30.
			mu.Lock()
			secondSinceSeq = sinceSeq
			mu.Unlock()
			return nil, nil
		},
	}
	saldos := &mockChangelogRepo{}
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		return ventfb.SentinelNoActiveTx, nil
	})

	subCh, unsub := bus.Subscribe("pagos_changed")
	defer unsub()

	l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval))

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// Drain first publish.
	select {
	case <-subCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first publish not received")
	}

	// Wait until the second Since call is made.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return callNum >= 2
	}, 2*time.Second, 5*time.Millisecond, "second Since call not made")

	mu.Lock()
	got := secondSinceSeq
	mu.Unlock()
	assert.Equal(t, int64(30), got, "cursor must advance to max SeqID=30 after first event")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestListener_EmptyChangelog_DoesNotPublish verifies that when Since returns
// no entries, the listener does not publish anything to the bus.
//
//nolint:paralleltest
func TestListener_EmptyChangelog_DoesNotPublish(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{
				events:     []outbound.FbEvent{{Name: "pagos_changed", Count: 1}},
				closeAfter: false,
			},
		},
	}

	pagos := &mockChangelogRepo{
		sinceFn: func(_ context.Context, _, _ int64, _ int) ([]outbound.ChangelogEntry, error) {
			return nil, nil // empty — nothing committed yet
		},
	}
	saldos := &mockChangelogRepo{}
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		return ventfb.SentinelNoActiveTx, nil
	})

	subCh, unsub := bus.Subscribe("pagos_changed")
	defer unsub()

	l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval))

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// Wait briefly; Since is called but returns empty, so no publish expected.
	// Give the listener a moment to process the event.
	require.Eventually(t, func() bool {
		return len(pagos.getSinceCalls()) > 0
	}, 500*time.Millisecond, 5*time.Millisecond, "Since not called")

	// Channel must be empty.
	select {
	case ids := <-subCh:
		t.Fatalf("unexpected publish when changelog empty: got %v", ids)
	default:
		// correct — nothing published
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestListener_WatermarkFailure_PublishesEmptyAsWakeup verifies that when the
// watermark probe returns an error, the listener publishes []int{} so
// subscribers cursor-sync.
//
//nolint:paralleltest
func TestListener_WatermarkFailure_PublishesEmptyAsWakeup(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{
				events:     []outbound.FbEvent{{Name: "pagos_changed", Count: 1}},
				closeAfter: false,
			},
		},
	}

	errWatermark := errors.New("watermark unavailable")
	// Start must succeed (MaxSeqID call on init), so we return an error only
	// after startup. Use a counter to distinguish init probe from event probe.
	probeCallCount := 0
	var probeMu sync.Mutex
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		probeMu.Lock()
		n := probeCallCount
		probeCallCount++
		probeMu.Unlock()
		if n >= 2 { // 0=init pagos MaxSeqID, 1=init saldos MaxSeqID, 2+=event probe
			return 0, errWatermark
		}
		return ventfb.SentinelNoActiveTx, nil
	})

	// MaxSeqID is called at init time, not through watermarkProbe directly.
	// So pagos/saldos repo MaxSeqID calls use the probe indirectly only on
	// the first Start call. We need the actual probe to fail on the event.
	//
	// Simplify: have probe fail immediately after init (probeCallCount >= 1
	// since watermarkProbe is called once in Start for the watermark, then
	// pagos.MaxSeqID and saldos.MaxSeqID are called separately).
	//
	// Re-design: use a simpler approach — probe returns error always but
	// we bypass it for MaxSeqID by using a maxFn on the repos.
	pagos := &mockChangelogRepo{
		maxFn: func(_ context.Context, _ int64) (int64, error) { return 0, nil },
	}
	saldos := &mockChangelogRepo{
		maxFn: func(_ context.Context, _ int64) (int64, error) { return 0, nil },
	}

	// Separate probe: always error so the event handling always triggers wakeup.
	errProbe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		probeMu.Lock()
		n := probeCallCount
		probeCallCount++
		probeMu.Unlock()
		if n == 0 {
			// First call is in Start() for cursor initialization.
			return ventfb.SentinelNoActiveTx, nil
		}
		return 0, errWatermark
	})
	_ = probe // discard the earlier probe definition

	subCh, unsub := bus.Subscribe("pagos_changed")
	defer unsub()

	l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(errProbe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval))

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// The listener should publish empty wakeup.
	select {
	case ids := <-subCh:
		assert.Empty(t, ids, "watermark failure must publish empty []int{} wakeup")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected empty wakeup publish within 500ms on watermark failure")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestListener_ChangelogFailure_PublishesEmpty verifies that when Since returns
// an error, the listener publishes []int{} as a wakeup.
//
//nolint:paralleltest
func TestListener_ChangelogFailure_PublishesEmpty(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{
				events:     []outbound.FbEvent{{Name: "pagos_changed", Count: 1}},
				closeAfter: false,
			},
		},
	}

	pagos := &mockChangelogRepo{
		sinceFn: func(_ context.Context, _, _ int64, _ int) ([]outbound.ChangelogEntry, error) {
			return nil, errors.New("DB connection lost")
		},
	}
	saldos := &mockChangelogRepo{}
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		return ventfb.SentinelNoActiveTx, nil
	})

	subCh, unsub := bus.Subscribe("pagos_changed")
	defer unsub()

	l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval))

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	select {
	case ids := <-subCh:
		assert.Empty(t, ids, "changelog failure must publish empty []int{} wakeup")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected empty wakeup publish within 500ms on changelog failure")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestListener_Start_InitializesLastSeenFromMaxSeqID verifies that after Start,
// the listener's cursor starts at MaxSeqID for the given watermark, so history
// is not replayed on reconnect.
//
//nolint:paralleltest
func TestListener_Start_InitializesLastSeenFromMaxSeqID(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	// Send one event immediately after subscribing so we can observe the sinceSeq.
	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{
				events:     []outbound.FbEvent{{Name: "pagos_changed", Count: 1}},
				closeAfter: false,
			},
		},
	}

	const initMax = int64(42)
	var capturedSinceSeq int64
	var mu sync.Mutex

	pagos := &mockChangelogRepo{
		maxFn: func(_ context.Context, _ int64) (int64, error) {
			return initMax, nil
		},
		sinceFn: func(_ context.Context, sinceSeq, _ int64, _ int) ([]outbound.ChangelogEntry, error) {
			mu.Lock()
			capturedSinceSeq = sinceSeq
			mu.Unlock()
			return nil, nil // return empty so no publish
		},
	}
	saldos := &mockChangelogRepo{
		maxFn: func(_ context.Context, _ int64) (int64, error) { return 0, nil },
	}
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		return ventfb.SentinelNoActiveTx, nil
	})

	l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval))

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// Wait for the Since call to happen.
	require.Eventually(t, func() bool {
		return len(pagos.getSinceCalls()) > 0
	}, 500*time.Millisecond, 5*time.Millisecond, "Since not called")

	mu.Lock()
	got := capturedSinceSeq
	mu.Unlock()

	assert.Equal(t, initMax, got,
		"sinceSeq on first event must equal MaxSeqID value from Start")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestListener_Start_MaxSeqIDFailure_ReturnsError verifies that Start returns
// an error when MaxSeqID fails, so fx treats it as a startup failure.
//
//nolint:paralleltest
func TestListener_Start_MaxSeqIDFailure_ReturnsError(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	src := &mockSource{}

	pagos := &mockChangelogRepo{
		maxFn: func(_ context.Context, _ int64) (int64, error) {
			return 0, errors.New("Firebird unreachable")
		},
	}
	saldos := &mockChangelogRepo{}
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		return ventfb.SentinelNoActiveTx, nil
	})

	l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval))

	err := l.Start(context.Background())
	require.Error(t, err, "Start must return error when MaxSeqID fails")
	assert.Contains(t, err.Error(), "max_seq_id", "error should mention what failed")
}

// TestListener_ReconnectSyntheticPublish_IsEmpty verifies that the synthetic
// publish on reconnect carries []int{} (empty, not nil and not real IDs),
// which signals subscribers to cursor-sync.
//
//nolint:paralleltest
func TestListener_ReconnectSyntheticPublish_IsEmpty(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{events: nil, closeAfter: true},  // disconnect triggers reconnect path
			{events: nil, closeAfter: false}, // stable on second attempt
		},
	}

	fc := newFakeClock()
	defer fc.stop()
	l := newListener(
		src, bus,
		ventfb.WithClock(fc),
		ventfb.WithBackoffSchedule([]time.Duration{1 * time.Millisecond}),
	)

	pagosCh, unsubPagos := bus.Subscribe("pagos_changed")
	saldosCh, unsubSaldos := bus.Subscribe("saldos_changed")
	defer unsubPagos()
	defer unsubSaldos()

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	require.Eventually(t, func() bool {
		return len(fc.durations()) > 0
	}, 2*time.Second, 5*time.Millisecond, "backoff not reached")
	fc.tick()

	// Receive the pagos synthetic publish and assert it's empty (not nil).
	var pagosPayload []int
	require.Eventually(t, func() bool {
		select {
		case pagosPayload = <-pagosCh:
			return true
		default:
			return false
		}
	}, 2*time.Second, 5*time.Millisecond, "pagos synthetic publish not received")
	assert.NotNil(t, pagosPayload, "synthetic publish must be []int{} not nil")
	assert.Empty(t, pagosPayload, "synthetic publish must be empty []int{}")

	// Same for saldos.
	var saldosPayload []int
	require.Eventually(t, func() bool {
		select {
		case saldosPayload = <-saldosCh:
			return true
		default:
			return false
		}
	}, 2*time.Second, 5*time.Millisecond, "saldos synthetic publish not received")
	assert.NotNil(t, saldosPayload, "synthetic publish must be []int{} not nil")
	assert.Empty(t, saldosPayload, "synthetic publish must be empty []int{}")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestListener_HonorsLimit500 verifies that the listener calls Since once per
// event (with limit=500), even when 500 entries are returned. It does not loop
// inside handleEvent to drain to completion — staying responsive is the priority;
// the next event or cursor sync will pick up the rest.
//
//nolint:paralleltest
func TestListener_HonorsLimit500(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{
				events:     []outbound.FbEvent{{Name: "pagos_changed", Count: 1}},
				closeAfter: false,
			},
		},
	}

	// Build 500 entries.
	entries500 := make([]outbound.ChangelogEntry, 500)
	for i := range 500 {
		entries500[i] = outbound.ChangelogEntry{SeqID: int64(i + 1), PK: i + 1}
	}

	callCount := 0
	var mu sync.Mutex
	pagos := &mockChangelogRepo{
		sinceFn: func(_ context.Context, _, _ int64, _ int) ([]outbound.ChangelogEntry, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			return entries500, nil
		},
	}
	saldos := &mockChangelogRepo{}
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		return ventfb.SentinelNoActiveTx, nil
	})

	subCh, unsub := bus.Subscribe("pagos_changed")
	defer unsub()

	l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval))

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// Wait for the publish.
	select {
	case ids := <-subCh:
		assert.Len(t, ids, 500, "all 500 IDs must be published")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected publish within 500ms")
	}

	// Give a brief window to confirm no extra Since call was made.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	n := callCount
	mu.Unlock()
	assert.Equal(t, 1, n, "Since must be called exactly once per event (no looping to drain)")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// ─── Property tests ───────────────────────────────────────────────────────────

// TestProperty_Listener_LastSeenMonotonic verifies that lastSeenSeq is
// non-decreasing over a rapid sequence of events with varying changelog
// responses.
//
//nolint:paralleltest
func TestProperty_Listener_LastSeenMonotonic(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	rapid.Check(t, func(rt *rapid.T) {
		bus := eventbus.New()
		defer bus.Close()

		// Generate a list of SEQ_ID sequences to return for N events.
		n := rapid.IntRange(2, 10).Draw(rt, "n_events")

		// Keep track of what Since returns on each call to assert monotonicity.
		var mu sync.Mutex
		type callResult struct {
			sinceSeq    int64
			maxReturned int64
		}
		var results []callResult

		callIdx := 0
		// For each event, return a random set of entries with ascending SeqIDs.
		eventEntries := make([][]outbound.ChangelogEntry, n)
		for i := range n {
			count := rapid.IntRange(0, 5).Draw(rt, "entry_count")
			entries := make([]outbound.ChangelogEntry, count)
			for j := range count {
				entries[j] = outbound.ChangelogEntry{
					SeqID: int64(i*10 + j + 1),
					PK:    i*10 + j + 1,
				}
			}
			eventEntries[i] = entries
		}

		pagos := &mockChangelogRepo{
			sinceFn: func(_ context.Context, sinceSeq, _ int64, _ int) ([]outbound.ChangelogEntry, error) {
				mu.Lock()
				idx := callIdx
				callIdx++
				mu.Unlock()
				if idx >= len(eventEntries) {
					return nil, nil
				}
				entries := eventEntries[idx]
				var maxSeq int64
				for _, e := range entries {
					if e.SeqID > maxSeq {
						maxSeq = e.SeqID
					}
				}
				mu.Lock()
				results = append(results, callResult{sinceSeq, maxSeq})
				mu.Unlock()
				return entries, nil
			},
		}
		saldos := &mockChangelogRepo{}
		probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
			return ventfb.SentinelNoActiveTx, nil
		})

		events := make([]outbound.FbEvent, n)
		for i := range n {
			events[i] = outbound.FbEvent{Name: "pagos_changed", Count: 1}
		}

		src := &mockSource{
			subscribeResponses: []subscribeResponse{
				{events: events, closeAfter: false},
			},
		}

		subCh, unsub := bus.Subscribe("pagos_changed")
		defer unsub()

		l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
			ventfb.WithWatermarkProbe(probe),
			ventfb.WithWatermarkProbeInterval(noProbeInterval))

		ctx := context.Background()
		require.NoError(t, l.Start(ctx))

		// Drain all expected publishes (only non-empty ones trigger publishes).
		nonEmpty := 0
		for _, ee := range eventEntries {
			if len(ee) > 0 {
				nonEmpty++
			}
		}
		for range nonEmpty {
			select {
			case <-subCh:
			case <-time.After(500 * time.Millisecond):
				// The listener might still be processing; that's ok for this test.
				goto done
			}
		}
	done:

		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		require.NoError(t, l.Stop(stopCtx))

		// Assert lastSeenSeq is non-decreasing over the call sequence.
		mu.Lock()
		defer mu.Unlock()
		for i := 1; i < len(results); i++ {
			if results[i].sinceSeq < results[i-1].sinceSeq {
				rt.Fatalf("lastSeenSeq decreased at call %d: %d -> %d",
					i, results[i-1].sinceSeq, results[i].sinceSeq)
			}
		}
	})
}

// TestProperty_Listener_NoIdLoss verifies the cursor-advancement invariant:
// for each pair of consecutive Since calls (i, i+1), sinceSeq[i+1] equals
// max(SeqID) returned by call i. This guarantees zero cursor gap — every
// committed row with SeqID in (sinceSeq[i], max(SeqID)[i]] will be visible
// to the next Since(sinceSeq[i+1], ...) call.
//
// The bus uses latest-wins coalescing so individual IDs may be lost in transit
// (that is by design — subscribers use cursor sync to recover). The correct
// invariant is at the cursor level, not the bus payload level.
//
//nolint:paralleltest
func TestProperty_Listener_NoIdLoss(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	rapid.Check(t, func(rt *rapid.T) {
		bus := eventbus.New()
		defer bus.Close()

		// Use n+1 events so we can observe sinceSeq on call N+1 and verify it
		// equals max(SeqID) from call N.
		n := rapid.IntRange(2, 6).Draw(rt, "n_events")

		var mu sync.Mutex
		// allEntries[i] is what call i returns.
		allEntries := make([][]outbound.ChangelogEntry, n)
		for i := range n {
			count := rapid.IntRange(1, 4).Draw(rt, "count")
			entries := make([]outbound.ChangelogEntry, count)
			for j := range count {
				entries[j] = outbound.ChangelogEntry{
					SeqID: int64(i*100 + j + 1),
					PK:    i*100 + j + 1,
				}
			}
			allEntries[i] = entries
		}

		type callRecord struct {
			sinceSeq int64
			returned []outbound.ChangelogEntry
		}
		var callRecords []callRecord

		callIdx := 0
		doneCh := make(chan struct{})
		pagos := &mockChangelogRepo{
			sinceFn: func(_ context.Context, sinceSeq, _ int64, _ int) ([]outbound.ChangelogEntry, error) {
				mu.Lock()
				idx := callIdx
				callIdx++
				mu.Unlock()

				var entries []outbound.ChangelogEntry
				if idx < len(allEntries) {
					entries = allEntries[idx]
				}

				mu.Lock()
				callRecords = append(callRecords, callRecord{sinceSeq, entries})
				remaining := n - callIdx
				mu.Unlock()

				if remaining <= 0 {
					select {
					case <-doneCh:
					default:
						close(doneCh)
					}
				}
				return entries, nil
			},
		}
		saldos := &mockChangelogRepo{}
		probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
			return ventfb.SentinelNoActiveTx, nil
		})

		events := make([]outbound.FbEvent, n)
		for i := range n {
			events[i] = outbound.FbEvent{Name: "pagos_changed", Count: 1}
		}

		src := &mockSource{
			subscribeResponses: []subscribeResponse{
				{events: events, closeAfter: false},
			},
		}

		l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
			ventfb.WithWatermarkProbe(probe),
			ventfb.WithWatermarkProbeInterval(noProbeInterval))

		ctx := context.Background()
		require.NoError(t, l.Start(ctx))

		select {
		case <-doneCh:
		case <-time.After(2 * time.Second):
		}
		time.Sleep(20 * time.Millisecond)

		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		require.NoError(t, l.Stop(stopCtx))

		mu.Lock()
		records := make([]callRecord, len(callRecords))
		copy(records, callRecords)
		mu.Unlock()

		// Need at least 2 calls to verify the invariant between calls.
		if len(records) < 2 {
			return
		}

		// For each call i (0-based), if call i returned non-empty entries,
		// then sinceSeq of call i+1 must equal max(SeqID from call i).
		for i := 0; i+1 < len(records); i++ {
			if len(records[i].returned) == 0 {
				// Empty return → cursor didn't advance; sinceSeq[i+1] == sinceSeq[i].
				if records[i+1].sinceSeq != records[i].sinceSeq {
					rt.Fatalf("cursor moved on empty batch: call[%d] sinceSeq=%d but call[%d] sinceSeq=%d",
						i, records[i].sinceSeq, i+1, records[i+1].sinceSeq)
				}
				continue
			}
			var batchMax int64
			for _, e := range records[i].returned {
				if e.SeqID > batchMax {
					batchMax = e.SeqID
				}
			}
			if records[i+1].sinceSeq != batchMax {
				rt.Fatalf("cursor gap at call[%d→%d]: sinceSeq=%d but expected max SeqID=%d from prev batch",
					i, i+1, records[i+1].sinceSeq, batchMax)
			}
		}
	})
}

// ─── Watermark probe tests ─────────────────────────────────────────────────────

// probeLeakIgnores returns goleak options that filter out background
// goroutines spawned by integration tests in this package (the shared
// firebird.Pool and the firebirdsql event-subscription used by
// requireFBEventReachable). The goroutines persist for the lifetime of the
// test binary by design — the pool is sync.Once-cached, and the FbEvent
// subscription writer keeps a goroutine pumping the wire — so unit tests
// that run after an integration test see them as "leaked" without these
// ignores. They are unrelated to the listener under test.
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

// newListenerForProbe builds a listener with both pagos/saldos repos exposed
// and a controllable watermark probe. The probe goroutine interval is set to
// 24h so the ticker never fires during the test; tests call probeWatermarkOnce
// directly via the exported ProbeWatermarkOnce method.
//
// The probe is used by Start once for the init watermark, so the probe function
// must succeed on that first call. Tests use an atomic counter to distinguish
// the Start probe call (count=0) from explicit ProbeWatermarkOnce calls
// (count>=1).
func newListenerForProbe(
	bus *eventbus.Bus,
	probeFn func(ctx context.Context) (int64, error),
) (*ventfb.FbEventListener, *mockChangelogRepo, *mockChangelogRepo) {
	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{events: nil, closeAfter: false},
		},
	}
	pagos := &mockChangelogRepo{}
	saldos := &mockChangelogRepo{}
	probe := ventfb.WatermarkProbe(probeFn)
	l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(noProbeInterval))
	return l, pagos, saldos
}

// TestProbeLoop_FiresHandleEventOnWatermarkAdvance verifies that probeWatermarkOnce
// calls handleEvent (via Since) for both topics when the watermark strictly
// advances between ticks.
//
//nolint:paralleltest
func TestProbeLoop_FiresHandleEventOnWatermarkAdvance(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	// probeCallCount tracks total probe invocations (including the Start init call).
	// Call 0 = Start init, call 1 = first ProbeWatermarkOnce, call 2 = second.
	var probeCallCount int64
	l, pagos, saldos := newListenerForProbe(bus, func(_ context.Context) (int64, error) {
		count := atomic.AddInt64(&probeCallCount, 1) - 1 // 0-indexed
		switch count {
		case 0:
			// Start init call — return a valid watermark.
			return ventfb.SentinelNoActiveTx, nil
		case 1:
			// First ProbeWatermarkOnce — advance from 0 (lastObservedWatermark) to 100.
			return 100, nil
		default:
			// Second ProbeWatermarkOnce — advance from 100 to 150.
			return 150, nil
		}
	})

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// First explicit tick: watermark 100 > 0 — fires Since for both topics.
	l.ProbeWatermarkOnce(ctx)
	sinceCallsAfterFirst := len(pagos.getSinceCalls()) + len(saldos.getSinceCalls())
	assert.Equal(t, 2, sinceCallsAfterFirst,
		"first probe tick (advance 0->100) must call Since for both pagos and saldos")

	// Second explicit tick: watermark 150 > 100 — fires Since again for both topics.
	l.ProbeWatermarkOnce(ctx)
	sinceCallsAfterSecond := len(pagos.getSinceCalls()) + len(saldos.getSinceCalls())
	assert.Equal(t, 4, sinceCallsAfterSecond,
		"second probe tick (advance 100->150) must call Since for both topics again")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestProbeLoop_SkipsWhenWatermarkUnchanged verifies that probeWatermarkOnce does
// NOT call Since when the watermark returns the same value on consecutive ticks.
//
//nolint:paralleltest
func TestProbeLoop_SkipsWhenWatermarkUnchanged(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	var probeCallCount int64
	l, pagos, saldos := newListenerForProbe(bus, func(_ context.Context) (int64, error) {
		count := atomic.AddInt64(&probeCallCount, 1) - 1
		if count == 0 {
			// Start init call — return valid watermark.
			return ventfb.SentinelNoActiveTx, nil
		}
		return 100, nil // all probe ticks return 100
	})

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// First explicit tick: 100 > 0 — Since IS called for both topics.
	l.ProbeWatermarkOnce(ctx)
	afterFirst := len(pagos.getSinceCalls()) + len(saldos.getSinceCalls())
	assert.Equal(t, 2, afterFirst, "first tick (advance 0->100) must probe both topics")

	// Second explicit tick: 100 == 100 — Since must NOT be called again.
	l.ProbeWatermarkOnce(ctx)
	afterSecond := len(pagos.getSinceCalls()) + len(saldos.getSinceCalls())
	assert.Equal(t, 2, afterSecond,
		"second tick with unchanged watermark (100->100) must not call Since")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestProbeLoop_SkipsWhenWatermarkRetreats verifies that a retreating watermark
// (should never happen in practice but defensive) does NOT trigger handleEvent.
//
//nolint:paralleltest
func TestProbeLoop_SkipsWhenWatermarkRetreats(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	var probeCallCount int64
	l, pagos, saldos := newListenerForProbe(bus, func(_ context.Context) (int64, error) {
		count := atomic.AddInt64(&probeCallCount, 1) - 1
		switch count {
		case 0:
			// Start init call.
			return ventfb.SentinelNoActiveTx, nil
		case 1:
			// First ProbeWatermarkOnce — advance 0 -> 100.
			return 100, nil
		default:
			// Second ProbeWatermarkOnce — retreat 100 -> 50.
			return 50, nil
		}
	})

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// First tick: advance 0 -> 100 — Since IS called.
	l.ProbeWatermarkOnce(ctx)
	afterFirst := len(pagos.getSinceCalls()) + len(saldos.getSinceCalls())
	assert.Equal(t, 2, afterFirst, "first tick (advance 0->100) must call Since for both topics")

	// Second tick: retreat 100 -> 50 — Since must NOT be called.
	l.ProbeWatermarkOnce(ctx)
	afterSecond := len(pagos.getSinceCalls()) + len(saldos.getSinceCalls())
	assert.Equal(t, 2, afterSecond,
		"second tick (retreat 100->50) must not call Since")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestProbeLoop_ProbeFailureIsNonFatal verifies that a probe error is logged and
// skipped — the listener does not crash and the next successful tick works.
//
//nolint:paralleltest
func TestProbeLoop_ProbeFailureIsNonFatal(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	errFirebird := errors.New("MON$TRANSACTIONS unavailable")
	var probeCallCount int64
	l, pagos, saldos := newListenerForProbe(bus, func(_ context.Context) (int64, error) {
		count := atomic.AddInt64(&probeCallCount, 1) - 1
		switch count {
		case 0:
			// Start init call — must succeed so Start() doesn't return an error.
			return ventfb.SentinelNoActiveTx, nil
		case 1:
			// First ProbeWatermarkOnce — return an error.
			return 0, errFirebird
		default:
			// Second ProbeWatermarkOnce — success, watermark advanced.
			return 200, nil
		}
	})

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// First probe tick: error — Since must NOT be called.
	l.ProbeWatermarkOnce(ctx)
	afterError := len(pagos.getSinceCalls()) + len(saldos.getSinceCalls())
	assert.Equal(t, 0, afterError,
		"probe error must not call Since")

	// Second probe tick: success, watermark 200 > 0 (lastObservedWatermark was not
	// updated on the error tick, so it's still 0) — Since IS called for both topics.
	l.ProbeWatermarkOnce(ctx)
	afterSuccess := len(pagos.getSinceCalls()) + len(saldos.getSinceCalls())
	assert.Equal(t, 2, afterSuccess,
		"after probe error, next successful tick with new watermark must call Since")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestStart_SpawnsBothLoops verifies that Start spawns both the main event loop
// (evidenced by Subscribe being called) and the watermark-probe loop (evidenced
// by MinActiveTransactionID being called within the probe interval window).
//
// This test uses a short probe interval (10ms) and a buffered probe counter to
// detect that the probe goroutine actually fired at least once.
//
//nolint:paralleltest
func TestStart_SpawnsBothLoops(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	// stable source: never closes, so subscribe is called once and stays open.
	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{events: nil, closeAfter: false},
		},
	}

	probeCalls := make(chan struct{}, 64)
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		select {
		case probeCalls <- struct{}{}:
		default:
		}
		return ventfb.SentinelNoActiveTx, nil
	})

	pagos := &mockChangelogRepo{}
	saldos := &mockChangelogRepo{}
	l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(10*time.Millisecond))

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// Assert main loop subscribed.
	require.Eventually(t, func() bool {
		return src.subscribeCallCount() >= 1
	}, 2*time.Second, 5*time.Millisecond, "main loop must Subscribe within 2s")

	// Assert probe loop fired (probe goroutine must call watermarkProbe within
	// a few ticks of the 10ms interval; give it 500ms generous window).
	// Start itself calls the probe once — drain it before waiting for loop calls.
	for {
		select {
		case <-probeCalls:
			continue
		default:
		}
		break
	}
	require.Eventually(t, func() bool {
		select {
		case <-probeCalls:
			return true
		default:
			return false
		}
	}, 500*time.Millisecond, 5*time.Millisecond,
		"probe loop must call watermarkProbe at least once within 500ms")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
}

// TestStop_WaitsForBothLoops verifies that Stop blocks until both the event loop
// and the probe loop have exited, and that no goroutines leak afterward.
// goleak.VerifyNone is the authoritative check here.
//
//nolint:paralleltest
func TestStop_WaitsForBothLoops(t *testing.T) {
	defer goleak.VerifyNone(t, probeLeakIgnores()...)

	bus := eventbus.New()
	defer bus.Close()

	src := &mockSource{
		subscribeResponses: []subscribeResponse{
			{events: nil, closeAfter: false},
		},
	}
	probe := ventfb.WatermarkProbe(func(_ context.Context) (int64, error) {
		return ventfb.SentinelNoActiveTx, nil
	})
	pagos := &mockChangelogRepo{}
	saldos := &mockChangelogRepo{}

	l := ventfb.NewFbEventListener(src, bus, nil, pagos, saldos, nil,
		ventfb.WithWatermarkProbe(probe),
		ventfb.WithWatermarkProbeInterval(10*time.Millisecond))

	ctx := context.Background()
	require.NoError(t, l.Start(ctx))

	// Give both loops time to start.
	require.Eventually(t, func() bool {
		return src.subscribeCallCount() >= 1
	}, 2*time.Second, 5*time.Millisecond, "main loop must Subscribe")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, l.Stop(stopCtx))
	// goleak.VerifyNone at deferred call confirms no goroutine leak.
}
