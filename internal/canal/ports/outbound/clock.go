// Package outbound declares the interfaces the canal module needs from the
// outside world: a durable mailbox for inbound WhatsApp messages, a way to
// forward them to the store's on-premise server, and a clock. Implementations
// live in internal/canal/infra/* and are wired together at composition root
// via fx providers (internal/canal/module.go, Task 7).
package outbound

import "time"

// Clock returns the current wall-clock time. Services depend on this port
// instead of calling time.Now() directly, so tests can substitute a fixed
// or controllable clock and so canal/domain never touches the wall clock
// itself.
type Clock interface {
	Now() time.Time
}

// ProductionClock is the real-world implementation of Clock. It always
// returns UTC so timestamps inserted into the mailbox are normalized at the
// source.
type ProductionClock struct{}

// Now returns the current wall-clock time in UTC.
func (ProductionClock) Now() time.Time { return time.Now().UTC() }
