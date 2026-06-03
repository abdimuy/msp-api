//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package ventfb

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// Topic names published to the in-process event bus.
const (
	topicPagos  = "pagos_changed"
	topicSaldos = "saldos_changed"
)

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

// FbEventListener bridges Firebird POST_EVENT notifications to the in-process
// eventbus.Bus. It subscribes to "pagos_changed" and "saldos_changed" topics
// and fan-outs signals to all SSE handlers.
//
// On connection failure the listener waits an exponential backoff interval,
// publishes synthetic signals for both topics (so SSE subscribers re-sync
// from the digest endpoint during the outage window), then reopens the
// connection.
//
// Lifecycle methods Start and Stop mirror PagoRetryWorker exactly.
type FbEventListener struct {
	source  outbound.FbEventSource
	bus     *eventbus.Bus
	logger  *slog.Logger
	clock   listenerClock
	backoff []time.Duration

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewFbEventListener builds a listener. source must be constructed before calling
// this; bus is the shared in-process event bus wired at the composition root.
func NewFbEventListener(
	source outbound.FbEventSource,
	bus *eventbus.Bus,
	logger *slog.Logger,
	opts ...Option,
) *FbEventListener {
	if logger == nil {
		logger = slog.Default()
	}
	l := &FbEventListener{
		source:  source,
		bus:     bus,
		logger:  logger,
		clock:   realListenerClock{},
		backoff: defaultBackoffSchedule,
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

// Start spins up the listener goroutine. Idempotent: a second Start while
// already running is a no-op.
//
// NOTA: el ctx que pasa fx a OnStart tiene un timeout (default 15s) y se
// cancela cuando la fase de startup termina, no cuando la app baja. Si
// derivamos `loopCtx` de él, el loop muere exactamente a los 15s y deja
// de recibir POST_EVENT sin loggear nada (drain regresa por ctx.Done y
// el chequeo `if ctx.Err() != nil { return }` sale silencioso). El loop
// se ata a context.Background() y solo se cancela por l.cancel() en Stop().
func (l *FbEventListener) Start(_ context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running {
		return nil
	}
	loopCtx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.done = make(chan struct{})
	l.running = true
	go l.loop(loopCtx)
	return nil
}

// Stop signals the listener to stop and waits for the goroutine to exit.
// Idempotent.
func (l *FbEventListener) Stop(ctx context.Context) error {
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return nil
	}
	l.cancel()
	done := l.done
	l.running = false
	l.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loop is the main goroutine. It opens the event source, subscribes, and
// forwards events to the bus. On error or channel close it reconnects with
// exponential backoff, publishing synthetic signals before each reconnect
// attempt so SSE subscribers can re-sync.
func (l *FbEventListener) loop(ctx context.Context) {
	defer close(l.done)
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
			// Synthetic publish before next attempt.
			// commit 6 replaces these nil-ids publishes with a Since-call
			// against the changelog repo so real IDs are carried.
			l.bus.Publish(topicPagos, nil)
			l.bus.Publish(topicSaldos, nil)
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
			// Synthetic publish before reopening.
			// commit 6 replaces these nil-ids publishes with a Since-call
			// against the changelog repo so real IDs are carried.
			l.bus.Publish(topicPagos, nil)
			l.bus.Publish(topicSaldos, nil)
			attempt = min(attempt+1, len(l.backoff)-1)
		}
	}
}

// drain reads from eventCh and publishes each event name to the bus until
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
			l.bus.Publish(ev.Name, nil) // commit 6 replaces this with a Since-call against the changelog repo.
			l.logger.DebugContext(
				ctx, "fb_event_listener.event_received",
				slog.String("name", ev.Name),
				slog.Int("count", ev.Count),
			)
		}
	}
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
