//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package ventfb

import (
	"sync"

	"github.com/nakagami/firebirdsql"

	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// fbEventSource wraps *firebirdsql.FbEvent to satisfy outbound.FbEventSource.
// Each instance owns its own TCP connection; the caller must call Close when done.
type fbEventSource struct {
	fbe *firebirdsql.FbEvent
}

// NewFbEventSource opens a dedicated Firebird event connection for the given DSN
// and returns an outbound.FbEventSource. The connection is separate from the
// regular query pool — Firebird's event protocol uses its own TCP session.
func NewFbEventSource(dsn string) (outbound.FbEventSource, error) {
	fbe, err := firebirdsql.NewFBEvent(dsn)
	if err != nil {
		return nil, err
	}
	return &fbEventSource{fbe: fbe}, nil
}

// Subscribe registers a buffered channel subscription for the named topics.
// It returns the channel, an unsubscribe func, and an error.
// The channel carries outbound.FbEvent values translated from firebirdsql.Event.
//
// Bridge goroutine lifecycle:
//   - errCh: registered via sub.NotifyClose so the driver signals TCP drops /
//     connection errors. The driver only sends on errCh when doClose is called
//     with a non-nil error (i.e. network failure), not on a clean Unsubscribe.
//   - done: closed by the unsubscribe func (clean shutdown path). Using a
//     sync.Once ensures close(done) is idempotent if called more than once.
//
// The bridge goroutine selects on rawCh, errCh, and done so it always exits
// promptly regardless of whether the disconnect was a TCP drop or a deliberate
// unsubscribe. This prevents a goroutine leak on every reconnect cycle.
func (s *fbEventSource) Subscribe(topics []string) (<-chan outbound.FbEvent, func() error, error) {
	rawCh := make(chan firebirdsql.Event, 64)
	sub, err := s.fbe.SubscribeChan(topics, rawCh)
	if err != nil {
		return nil, nil, err
	}

	// errCh receives the driver error on a TCP drop / unexpected close.
	// Buffer size 1 so the driver send never blocks even if bridge already exited.
	errCh := make(chan error, 1)
	sub.NotifyClose(errCh)

	out := make(chan outbound.FbEvent, 64)
	done := make(chan struct{})
	var unsubOnce sync.Once

	// Bridge the raw firebirdsql.Event channel to the port-typed channel.
	// Exits when:
	//   (a) rawCh is closed (driver closed the channel), or
	//   (b) errCh fires (TCP drop or driver error), or
	//   (c) done is closed (deliberate unsubscribe).
	go func() {
		defer close(out)
		for {
			select {
			case ev, ok := <-rawCh:
				if !ok {
					return
				}
				select {
				case out <- outbound.FbEvent{Name: ev.Name, Count: ev.Count}:
				case <-errCh:
					return
				case <-done:
					return
				}
			case <-errCh:
				// Driver signaled a TCP drop or unexpected close — trigger reconnect.
				return
			case <-done:
				return
			}
		}
	}()

	unsubscribe := func() error {
		// Close done first so the bridge goroutine exits before Unsubscribe tears
		// down the underlying connection. Idempotent via sync.Once.
		unsubOnce.Do(func() { close(done) })
		return sub.Unsubscribe()
	}
	return out, unsubscribe, nil
}

// Close tears down the underlying Firebird event connection.
func (s *fbEventSource) Close() error {
	return s.fbe.Close()
}
