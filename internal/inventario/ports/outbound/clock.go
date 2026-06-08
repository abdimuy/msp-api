// Package outbound declares the interfaces the inventario module needs from
// the outside world. Implementations live in internal/inventario/infra/* and
// are wired together at the composition root.
package outbound

import "time"

// Clock returns the current wall-clock time. Services depend on this port
// instead of calling time.Now() directly, so tests can substitute a fixed
// or controllable clock.
type Clock interface {
	Now() time.Time
}

// ProductionClock is the real-world implementation of Clock. It always
// returns UTC so timestamps inserted into the database are normalized at
// the source.
type ProductionClock struct{}

// Now returns the current wall-clock time in UTC.
func (ProductionClock) Now() time.Time { return time.Now().UTC() }
