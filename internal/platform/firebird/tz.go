package firebird

import (
	"sync"
	"time"
)

// businessTZName is the canonical timezone of every business operation in
// msp-api. Microsip stores TIMESTAMP columns as naked wall-clock values in
// this zone; our code must read and write through that lens so the rows we
// produce match what Microsip's UI shows.
//
// Hardcoded rather than env-configurable: changing the value silently would
// reinterpret every historical row in the dev/prod database.
const businessTZName = "America/Mexico_City"

// businessTZ is the cached *time.Location for businessTZName.
var (
	businessTZOnce sync.Once
	businessTZ     *time.Location
)

// BusinessTZ returns the time.Location every Firebird timestamp is
// interpreted in. The location is loaded lazily on first call and cached for
// the process lifetime.
//
// Panics if the embedded zoneinfo database does not know the location — that
// would indicate a broken Go runtime, not a recoverable error, and we prefer
// to fail loudly at startup.
func BusinessTZ() *time.Location {
	businessTZOnce.Do(func() {
		loc, err := time.LoadLocation(businessTZName)
		if err != nil {
			panic("firebird: cannot load business timezone " + businessTZName + ": " + err.Error())
		}
		businessTZ = loc
	})
	return businessTZ
}

// ToWallClock prepares a time.Time for INSERT/UPDATE against a Firebird
// TIMESTAMP column. It returns a time.Time whose wall-clock fields (year,
// month, day, hour, minute, second, nanosecond) reflect t expressed in the
// business timezone. The driver writes the wall-clock fields verbatim, so
// the resulting row matches what Microsip stores for the same instant.
//
// Example: t = 2026-05-13 12:00 UTC → returned time has wall-clock
// 2026-05-13 06:00 (CDMX = UTC-6).
func ToWallClock(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.In(BusinessTZ())
}

// FromWallClock is the inverse of ToWallClock: given a time.Time whose
// wall-clock fields represent a moment in the business timezone (which is
// how the driver hands rows back to us — as naked wall-clock, technically
// stamped with the process's time.Local), it returns the equivalent UTC
// instant.
//
// Use only on values that came directly out of the Firebird driver. Inputs
// already in UTC pass through unchanged when the wall-clock matches.
func FromWallClock(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	// Re-stamp the wall-clock with BusinessTZ regardless of t's original
	// zone (the driver may have used time.Local).
	loc := BusinessTZ()
	rewired := time.Date(
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond(),
		loc,
	)
	return rewired.UTC()
}
