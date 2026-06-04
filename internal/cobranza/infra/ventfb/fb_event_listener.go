//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package ventfb

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Topic names published to the in-process event bus.
const (
	topicPagos  = "pagos_changed"
	topicSaldos = "saldos_changed"
)

// defaultWatermarkProbeInterval is how often the probe goroutine checks whether
// the Firebird watermark (MinActiveTransactionID) has advanced. This closes the
// gap where a long-running Microsip GUI transaction blocks the watermark and its
// eventual commit does not emit POST_EVENT — without this probe the listener
// would stay asleep with pending rows until some unrelated POST_EVENT arrives.
const defaultWatermarkProbeInterval = 5 * time.Second

// defaultBackoffSchedule is the exponential backoff sequence used between
// reconnect attempts. After the last entry the final value is reused (cap 30s).
var defaultBackoffSchedule = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	30 * time.Second,
}

// listenerClock is a minimal clock abstraction for the listener so that tests
// can inject a fake without affecting the broader outbound.Clock interface.
type listenerClock interface {
	After(d time.Duration) <-chan time.Time
}

// realListenerClock wraps the standard library.
type realListenerClock struct{}

// After delegates to time.After.
func (realListenerClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// WatermarkProbe is a function that returns the current MinActiveTransactionID.
// Injected as a field so tests can substitute a fake without needing a real
// *firebird.Pool. The default probe calls MinActiveTransactionID(ctx, pool).
type WatermarkProbe func(ctx context.Context) (int64, error)

// Option configures a FbEventListener.
type Option func(*FbEventListener)

// WithClock injects a custom clock for backoff timing. Intended for tests.
func WithClock(c listenerClock) Option {
	return func(l *FbEventListener) { l.clock = c }
}

// WithBackoffSchedule replaces the default exponential backoff schedule.
// The last entry is reused as the cap. Intended for tests.
func WithBackoffSchedule(schedule []time.Duration) Option {
	return func(l *FbEventListener) { l.backoff = schedule }
}

// WithWatermarkProbe injects a custom watermark probe function. Intended for
// tests that do not have a real *firebird.Pool available.
func WithWatermarkProbe(p WatermarkProbe) Option {
	return func(l *FbEventListener) { l.watermarkProbe = p }
}

// WithWatermarkProbeInterval overrides the interval between watermark-advance
// probes. Pass a very large duration (e.g. 24*time.Hour) in tests that do not
// exercise the probe loop so it never fires during the test window and does not
// interfere with goleak. Pass 0 to disable the probe loop entirely.
func WithWatermarkProbeInterval(d time.Duration) Option {
	return func(l *FbEventListener) { l.watermarkProbeInterval = d }
}

// FbEventListener bridges Firebird POST_EVENT notifications to the in-process
// eventbus.Bus. It subscribes to "pagos_changed" and "saldos_changed" topics
// and fan-outs signals to all SSE handlers.
//
// On each received FbEvent, the listener queries the changelog using
// MinActiveTransactionID as a watermark, collects new entries since the last
// seen SEQ_ID, publishes the IDs to the bus, and advances the cursor.
//
// On connection failure the listener waits an exponential backoff interval,
// publishes synthetic []int{} signals for both topics (so SSE subscribers
// cursor-sync during the outage window), then reopens the connection.
//
// A secondary watermark-probe goroutine runs every watermarkProbeInterval and
// synthesizes handleEvent calls when the watermark strictly advances. This
// closes the gap where a long-running Microsip GUI transaction blocks the
// watermark: when that transaction finally commits, Firebird does NOT emit
// POST_EVENT, so without the probe the listener would stay asleep until some
// unrelated POST_EVENT happened to arrive.
//
// Lifecycle methods Start and Stop mirror PagoRetryWorker exactly.
type FbEventListener struct {
	source                 outbound.FbEventSource
	bus                    *eventbus.Bus
	pool                   *firebird.Pool
	pagosChangelog         outbound.PagosChangelogRepo
	saldosChangelog        outbound.SaldosChangelogRepo
	logger                 *slog.Logger
	clock                  listenerClock
	backoff                []time.Duration
	watermarkProbe         WatermarkProbe
	watermarkProbeInterval time.Duration

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// lastSeenSeq holds the highest SEQ_ID we have published per topic.
	// Reset on Stop; re-initialized on every Start from MaxSeqID(watermark).
	// Access serialized by lastSeenMu (separate from the lifecycle mu to avoid
	// contention between Start/Stop and the drain hot path).
	//
	// lastObservedWatermark is the watermark seen by the most recent probe tick.
	// Used to detect strict advances in watermarkProbeLoop. Protected by
	// lastSeenMu (cheap to colocate; both fields move at the same cadence).
	lastSeenMu            sync.Mutex
	lastSeenSeq           map[string]int64
	lastObservedWatermark int64
}

// NewFbEventListener builds a listener. source must be constructed before
// calling this; bus is the shared in-process event bus wired at the
// composition root. pool, pagosChangelog, and saldosChangelog must all be
// non-nil; Start returns an error if the changelog repos are unavailable.
func NewFbEventListener(
	source outbound.FbEventSource,
	bus *eventbus.Bus,
	pool *firebird.Pool,
	pagosChangelog outbound.PagosChangelogRepo,
	saldosChangelog outbound.SaldosChangelogRepo,
	logger *slog.Logger,
	opts ...Option,
) *FbEventListener {
	if logger == nil {
		logger = slog.Default()
	}
	l := &FbEventListener{
		source:                 source,
		bus:                    bus,
		pool:                   pool,
		pagosChangelog:         pagosChangelog,
		saldosChangelog:        saldosChangelog,
		logger:                 logger,
		clock:                  realListenerClock{},
		backoff:                defaultBackoffSchedule,
		watermarkProbeInterval: defaultWatermarkProbeInterval,
	}
	// Default watermark probe uses the real MinActiveTransactionID function.
	l.watermarkProbe = func(ctx context.Context) (int64, error) {
		return MinActiveTransactionID(ctx, l.pool)
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

// Start spins up the listener goroutine. Idempotent: a second Start while
// already running is a no-op.
//
// On every Start the cursor is re-initialized from MaxSeqID(watermark) so
// historical entries are never replayed. If the MaxSeqID probe fails, Start
// returns an error — a working listener requires a queryable changelog, and
// fx treats that error as a startup failure.
//
// NOTA: el ctx que pasa fx a OnStart tiene un timeout (default 15s) y se
// cancela cuando la fase de startup termina, no cuando la app baja. Si
// derivamos `loopCtx` de él, el loop muere exactamente a los 15s y deja
// de recibir POST_EVENT sin loggear nada. El loop se ata a
// context.Background() y solo se cancela por l.cancel() en Stop().
func (l *FbEventListener) Start(_ context.Context) error { //nolint:contextcheck // intentional: fresh ctx for long-lived goroutines
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running {
		return nil
	}

	// Initialize lastSeenSeq from the changelog. Use a fresh short ctx so
	// a slow Firebird doesn't block app startup forever.
	initCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	watermark, err := l.watermarkProbe(initCtx) //nolint:contextcheck // intentional: fresh init ctx
	if err != nil {
		return fmt.Errorf("listener start: watermark probe: %w", err)
	}

	pagosMax, err := l.pagosChangelog.MaxSeqID(initCtx, watermark) //nolint:contextcheck // initCtx is intentional: bounded startup probe
	if err != nil {
		return fmt.Errorf("listener start: pagos max_seq_id: %w", err)
	}
	saldosMax, err := l.saldosChangelog.MaxSeqID(initCtx, watermark) //nolint:contextcheck // same as above
	if err != nil {
		return fmt.Errorf("listener start: saldos max_seq_id: %w", err)
	}

	l.lastSeenMu.Lock()
	l.lastSeenSeq = map[string]int64{
		topicPagos:  pagosMax,
		topicSaldos: saldosMax,
	}
	l.lastSeenMu.Unlock()

	l.logger.InfoContext(initCtx, "fb_event_listener.initialized_cursor", //nolint:contextcheck // same as above
		slog.Int64("pagos_last_seen_seq", pagosMax),
		slog.Int64("saldos_last_seen_seq", saldosMax),
		slog.Int64("watermark", watermark),
	)

	// loopCtx is intentionally derived from Background(): the loop goroutine
	// outlives the fx OnStart context (which is cancelled after startup).
	// The loop is only cancelled by l.cancel() in Stop().
	loopCtx, cancel2 := context.WithCancel(context.Background()) //nolint:contextcheck,gosec // intentional — loop outlives Start ctx; cancel2 is stored in l.cancel and called by Stop
	l.cancel = cancel2
	l.running = true

	// Spawn the main event loop and the watermark-probe loop. Both share loopCtx
	// and both call wg.Done() when they exit. Stop() waits on the WaitGroup.
	l.wg.Add(1)
	go func() { //nolint:contextcheck // loopCtx is the long-lived context; this is correct by design
		defer l.wg.Done()
		l.loop(loopCtx)
	}()

	if l.watermarkProbeInterval > 0 {
		l.wg.Add(1)
		go func() { //nolint:contextcheck // loopCtx is the long-lived context; this is correct by design
			defer l.wg.Done()
			l.watermarkProbeLoop(loopCtx)
		}()
	}
	return nil
}

// Stop signals the listener to stop and waits for all goroutines to exit.
// Idempotent. Blocks until both the event loop and the watermark-probe loop
// have exited, or until ctx is cancelled.
func (l *FbEventListener) Stop(ctx context.Context) error {
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return nil
	}
	l.cancel()
	l.running = false
	l.mu.Unlock()

	done := make(chan struct{})
	go func() { l.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loop is the main goroutine. It opens the event source, subscribes, and
// forwards events to the bus. On error or channel close it reconnects with
// exponential backoff, publishing synthetic []int{} signals before each
// reconnect attempt so SSE subscribers can cursor-sync.
func (l *FbEventListener) loop(ctx context.Context) {
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		// Attempt to subscribe.
		eventCh, unsubscribe, err := l.source.Subscribe([]string{topicPagos, topicSaldos})
		if err != nil {
			l.logger.WarnContext(
				ctx, "fb_event_listener.subscribe_failed",
				slog.Int("attempt", attempt),
				slog.String("error", err.Error()),
			)
			if !l.waitBackoff(ctx, attempt) {
				return
			}
			// Synthetic wakeup before next attempt: empty IDs signals subscribers
			// to cursor-sync and catch up any rows missed during the outage.
			l.bus.Publish(topicPagos, []int{})
			l.bus.Publish(topicSaldos, []int{})
			attempt = min(attempt+1, len(l.backoff)-1)
			continue
		}

		l.logger.InfoContext(ctx, "fb_event_listener.subscribed", slog.Int("attempt", attempt))
		attempt = 0 // reset on successful subscribe

		// Drain the event channel until it closes or ctx is cancelled.
		closed := l.drain(ctx, eventCh)

		// Unsubscribe and close regardless of reason.
		if unsubErr := unsubscribe(); unsubErr != nil {
			l.logger.WarnContext(
				ctx, "fb_event_listener.unsubscribe_failed",
				slog.String("error", unsubErr.Error()),
			)
		}

		if ctx.Err() != nil {
			return
		}

		if closed {
			// Channel was closed by the driver — reconnect path.
			l.logger.WarnContext(ctx, "fb_event_listener.channel_closed_reconnecting")
			if !l.waitBackoff(ctx, attempt) {
				return
			}
			// Synthetic wakeup: empty IDs tells subscribers to cursor-sync so they
			// catch up any rows missed while the connection was down.
			l.bus.Publish(topicPagos, []int{})
			l.bus.Publish(topicSaldos, []int{})
			attempt = min(attempt+1, len(l.backoff)-1)
		}
	}
}

// drain reads from eventCh and calls handleEvent for each received event until
// the context is cancelled (returns false) or the channel is closed (returns
// true indicating reconnect is needed).
func (l *FbEventListener) drain(ctx context.Context, eventCh <-chan outbound.FbEvent) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case ev, ok := <-eventCh:
			if !ok {
				return true // channel closed — driver dropped the connection
			}
			l.handleEvent(ctx, ev)
		}
	}
}

// handleEvent queries the changelog for new entries since lastSeenSeq, capped
// at the current watermark, publishes the IDs to the bus, and advances the
// cursor. Errors are logged and result in a []int{} wakeup publish so
// subscribers cursor-sync to recover. The bus subscriber's cursor sync round
// is the authoritative safety net for any missed entries.
func (l *FbEventListener) handleEvent(ctx context.Context, ev outbound.FbEvent) {
	var sinceFunc func(context.Context, int64, int64, int) ([]outbound.ChangelogEntry, error)
	switch ev.Name {
	case topicPagos:
		sinceFunc = l.pagosChangelog.Since
	case topicSaldos:
		sinceFunc = l.saldosChangelog.Since
	default:
		l.logger.WarnContext(ctx, "fb_event_listener.unknown_topic",
			slog.String("name", ev.Name))
		return
	}

	watermark, err := l.watermarkProbe(ctx)
	if err != nil {
		l.logger.WarnContext(ctx, "fb_event_listener.watermark_failed",
			slog.String("topic", ev.Name),
			slog.String("error", err.Error()))
		// Defensive wakeup: signal subscribers so they cursor-sync.
		l.bus.Publish(ev.Name, []int{})
		return
	}

	l.lastSeenMu.Lock()
	sinceSeq := l.lastSeenSeq[ev.Name]
	l.lastSeenMu.Unlock()

	entries, err := sinceFunc(ctx, sinceSeq, watermark, 500)
	if err != nil {
		l.logger.WarnContext(ctx, "fb_event_listener.changelog_query_failed",
			slog.String("topic", ev.Name),
			slog.String("error", err.Error()))
		l.bus.Publish(ev.Name, []int{})
		return
	}

	if len(entries) == 0 {
		// No new rows visible under the watermark — either the event was for
		// rows still in flight (TX_ID >= watermark) or for rows already seen.
		// Publishing nothing is correct; the subscriber's cursor sync round
		// will catch up when the watermark advances.
		l.logger.DebugContext(ctx, "fb_event_listener.no_new_entries",
			slog.String("topic", ev.Name),
			slog.Int64("since_seq", sinceSeq),
			slog.Int64("watermark", watermark))
		return
	}

	ids := make([]int, len(entries))
	// IMPORTANT: advance lastSeenSeq to max(returned.SeqID) — never beyond.
	// If entries have SEQ_IDs [10,20,30] but 40,50 are hidden by the watermark
	// (in-flight transactions), advancing to 30 is correct. On the next event,
	// when those transactions commit and the watermark advances, Since(30,...) will
	// return them. Advancing beyond 30 would silently skip them forever.
	maxSeq := sinceSeq
	for i, e := range entries {
		ids[i] = e.PK
		if e.SeqID > maxSeq {
			maxSeq = e.SeqID
		}
	}

	l.bus.Publish(ev.Name, ids)

	l.lastSeenMu.Lock()
	l.lastSeenSeq[ev.Name] = maxSeq
	l.lastSeenMu.Unlock()

	l.logger.DebugContext(ctx, "fb_event_listener.published_ids",
		slog.String("topic", ev.Name),
		slog.Int("count", len(ids)),
		slog.Int64("from_seq", sinceSeq),
		slog.Int64("to_seq", maxSeq),
		slog.Int64("watermark", watermark),
		slog.Int("fb_event_count", ev.Count),
	)
}

// waitBackoff sleeps for the backoff duration at the given attempt index,
// returning true if the sleep completed or false if ctx was cancelled.
func (l *FbEventListener) waitBackoff(ctx context.Context, attempt int) bool {
	idx := attempt
	if idx >= len(l.backoff) {
		idx = len(l.backoff) - 1
	}
	d := l.backoff[idx]
	select {
	case <-l.clock.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// watermarkProbeLoop runs in parallel to loop(). Every watermarkProbeInterval
// it calls probeWatermarkOnce to detect watermark advances caused by long-running
// Microsip GUI transactions finally committing. Firebird does NOT emit POST_EVENT
// when MON$TRANSACTIONS state changes, so without this goroutine the listener
// would stay asleep with pending changelog rows until some unrelated POST_EVENT
// happened to arrive.
//
// Cost in steady state: one MIN(MON$TRANSACTION_ID) query per interval.
// Negligible (microseconds). The probe fires handleEvent only when the watermark
// strictly advances, so there are no spurious publishes in steady state.
func (l *FbEventListener) watermarkProbeLoop(ctx context.Context) {
	ticker := time.NewTicker(l.watermarkProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.probeWatermarkOnce(ctx)
		}
	}
}

// ProbeWatermarkOnce is the exported entry-point for unit tests that want to
// drive probeWatermarkOnce deterministically without a real ticker. In
// production code only watermarkProbeLoop calls probeWatermarkOnce.
func (l *FbEventListener) ProbeWatermarkOnce(ctx context.Context) { l.probeWatermarkOnce(ctx) }

// probeWatermarkOnce checks the current watermark and, if it strictly advanced
// from the last observed value, synthesizes a handleEvent call for each topic.
//
// Extracted from watermarkProbeLoop so unit tests can drive it deterministically
// without a real ticker.
//
// First-tick sentinel: lastObservedWatermark is initialized to 0 (Go zero-value).
// On the first call (prev=0), the condition watermark > prev is true for any
// real watermark value, so we always do one sanity probe at startup. This is
// harmless — handleEvent's Since(lastSeenSeq, watermark) won't publish anything
// if the cursor is already up to date (which it is: Start initialized it via
// MaxSeqID). Subsequent ticks only fire when watermark strictly advances.
func (l *FbEventListener) probeWatermarkOnce(ctx context.Context) {
	watermark, err := l.watermarkProbe(ctx)
	if err != nil {
		l.logger.WarnContext(ctx, "fb_event_listener.watermark_probe_failed",
			slog.String("error", err.Error()))
		return
	}

	l.lastSeenMu.Lock()
	prev := l.lastObservedWatermark
	advanced := watermark > prev
	if advanced {
		l.lastObservedWatermark = watermark
	}
	l.lastSeenMu.Unlock()

	if !advanced {
		// Watermark did not advance — no long-running tx committed in this window.
		// The push channel handles all normal cases; nothing to do.
		return
	}

	l.logger.DebugContext(ctx, "fb_event_listener.watermark_advanced",
		slog.Int64("prev", prev),
		slog.Int64("current", watermark))

	for _, topic := range []string{topicPagos, topicSaldos} {
		l.handleEvent(ctx, outbound.FbEvent{Name: topic, Count: 0})
	}
}
