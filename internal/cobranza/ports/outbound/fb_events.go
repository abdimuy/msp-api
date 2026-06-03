// Package outbound declares the interfaces the cobranza module needs from the
// outside world. Implementations live in internal/cobranza/infra/* and are
// wired together at composition root via fx providers.
package outbound

// FbEvent represents a single Firebird POST_EVENT notification.
// Name is the event name (e.g. "pagos_changed"); Count is the number of
// times the event was posted since the last acknowledgement (Firebird
// coalesces events within a transaction, so Count may be > 1).
type FbEvent struct {
	Name  string
	Count int
}

// FbEventSource abstracts the Firebird event subscription mechanism.
// The real implementation wraps *firebirdsql.FbEvent; tests inject a mock.
//
// Subscribe opens a buffered channel subscription for the named topics and
// returns the channel, an unsubscribe function, and any error. The caller
// must call unsubscribe when done and must close the underlying source via
// Close when no longer needed.
//
// The channel is closed when the underlying connection breaks, signalling
// the consumer to reconnect.
type FbEventSource interface {
	// Subscribe starts receiving events for the given topic names.
	// Returns a receive-only channel, an unsubscribe func, and an error.
	Subscribe(topics []string) (<-chan FbEvent, func() error, error)
	// Close tears down the underlying connection.
	Close() error
}
