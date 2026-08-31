package canalsqlite

import (
	"time"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// timeLayout is RFC3339Nano: nanosecond-precision, UTC-rendered (via
// formatUTC always calling .UTC() first, so the offset always encodes as
// "Z"). SQLite has no native date/time type, so every instant this package
// persists lives in a TEXT column in exactly this shape — a deliberate
// deviation from firebird.ToWallClock/ScanUTCTime, see schema.sql's header
// comment.
const timeLayout = time.RFC3339Nano

// formatUTC renders t as RFC3339Nano in UTC, ready to bind into a TEXT
// column. time.Format never drops a nanosecond that was actually set — it
// only omits trailing zeros in the fractional part — so this is lossless.
func formatUTC(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// parseUTC parses a TEXT column written by formatUTC back into a UTC
// time.Time. The explicit .UTC() call (rather than trusting the parsed
// location) is what makes the round trip provably stay UTC regardless of
// how the offset was spelled in the column.
func parseUTC(column, raw string) (time.Time, error) {
	t, err := time.Parse(timeLayout, raw)
	if err != nil {
		return time.Time{}, apperror.NewInternal(
			"canal_mailbox_timestamp_invalid",
			"la fecha almacenada en el buzón no es válida",
		).WithSource("canalsqlite").WithError(err).WithField("column", column).WithField("raw_value", raw)
	}
	return t.UTC(), nil
}
