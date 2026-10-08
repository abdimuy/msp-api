package outbound

import (
	"time"

	"github.com/google/uuid"
)

// Clock abstracts time.Now for deterministic tests. Implementations return
// UTC, per docs/module-standards/DATETIME_HANDLING.md. Domain code never
// calls time.Now(): the aggregate stamps what the service hands it, and the
// service stamps what this port returns.
type Clock interface {
	// Now returns the current instant in UTC.
	Now() time.Time
}

// ProductionClock is the real-world Clock.
type ProductionClock struct{}

// Now returns time.Now() in UTC.
func (ProductionClock) Now() time.Time { return time.Now().UTC() }

// IDGenerator hands out primary keys. The aggregate generates the UUIDs of
// the entities it creates (CLAUDE.md §1: no generator in the database, ever);
// this port exists for the ids the surrounding infrastructure mints, such as
// the storage key of an evidence blob.
type IDGenerator interface {
	// Nuevo returns a new identifier.
	Nuevo() uuid.UUID
}

// UUIDGenerator is the real-world IDGenerator.
type UUIDGenerator struct{}

// Nuevo returns a fresh v4 UUID.
func (UUIDGenerator) Nuevo() uuid.UUID { return uuid.New() }
