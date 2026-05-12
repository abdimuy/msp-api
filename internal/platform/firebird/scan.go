package firebird

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// errUnknownTimestampShape is returned by parseTimestamp when the input
// matches none of the accepted timestamp layouts. Wrapped by callers with
// the offending raw value before being surfaced to the user as an apperror.
var errUnknownTimestampShape = errors.New("firebird: unrecognized timestamp string")

// Post-scan helpers for Firebird driver type quirks.
//
// The nakagami/firebirdsql driver returns native Go types for most columns
// (uuid.UUID via Scan, sql.NullBool, etc.), but two areas need normalization
// at the repository boundary:
//
//   - NUMERIC / DECIMAL columns: the driver returns decimal.Decimal when the
//     SQL scale is negative (the common case — NUMERIC(15,2)), and int64
//     when the scale is non-negative. Repos must collapse both shapes into a
//     single decimal.Decimal so the domain never sees the difference.
//
//   - TIMESTAMP columns: Firebird does not store timezone information. The
//     driver returns time.Time in the DSN's configured timezone (defaults to
//     time.Local — i.e. the Go process's local clock). We always normalize
//     to UTC at the boundary so domain code can compare timestamps safely.
//
// Usage:
//
//	var raw any
//	if err := row.Scan(&id, &nombre, &raw); err != nil { ... }
//	monto, err := firebird.ScanDecimal(raw, 2) // for NUMERIC(_,2)

// ScanDecimal normalizes a Firebird driver value to decimal.Decimal.
//
// scale is the SQL scale of the source column (e.g. 2 for NUMERIC(15,2)).
// It is only consulted when the driver returned an int64 — used to recover
// the decimal point's position. For decimal.Decimal / string / []byte values,
// scale is ignored because the precision is already explicit.
//
// Returns an apperror.Error with code "firebird_scan_error" if src is nil or
// of an unexpected type. Use ScanNullDecimal for nullable columns.
func ScanDecimal(src any, scale int) (decimal.Decimal, error) {
	switch v := src.(type) {
	case nil:
		return decimal.Decimal{}, apperror.NewInternal(
			"firebird_scan_error",
			"no se pudo decodificar valor de base de datos").
			WithSource("firebird").
			WithField("reason", "nil value scanned into non-nullable decimal")
	case decimal.Decimal:
		return v, nil
	case int64:
		return decimal.New(v, int32(-scale)), nil
	case float64:
		slog.Warn("firebird: NUMERIC value arrived as float64; precision may degrade",
			"value", v, "scale", scale)
		return decimal.NewFromFloat(v), nil
	case []byte:
		d, err := decimal.NewFromString(string(v))
		if err != nil {
			return decimal.Decimal{}, wrapScanParseError(err, "decimal", string(v))
		}
		return d, nil
	case string:
		d, err := decimal.NewFromString(v)
		if err != nil {
			return decimal.Decimal{}, wrapScanParseError(err, "decimal", v)
		}
		return d, nil
	default:
		return decimal.Decimal{}, apperror.NewInternal(
			"firebird_scan_error",
			"no se pudo decodificar valor de base de datos").
			WithSource("firebird").
			WithField("got_type", fmt.Sprintf("%T", src)).
			WithField("target_type", "decimal.Decimal")
	}
}

// ScanNullDecimal is the nullable counterpart of ScanDecimal. A nil src
// produces an invalid NullDecimal; anything else is delegated to ScanDecimal.
func ScanNullDecimal(src any, scale int) (decimal.NullDecimal, error) {
	if src == nil {
		return decimal.NullDecimal{Valid: false}, nil
	}
	d, err := ScanDecimal(src, scale)
	if err != nil {
		return decimal.NullDecimal{}, err
	}
	return decimal.NullDecimal{Decimal: d, Valid: true}, nil
}

// ScanUTCTime normalizes a Firebird TIMESTAMP value to a UTC time.Time.
//
// Firebird stores timestamps as wall-clock values with no timezone metadata.
// The nakagami/firebirdsql driver returns them as time.Time tagged with the
// DSN-configured timezone (time.Local by default), which means the returned
// instant reflects the server's clock, not UTC. We collapse that ambiguity
// at the repository boundary by always returning UTC.
//
// Accepted inputs: time.Time, []byte (RFC3339 or "2006-01-02 15:04:05"),
// string (same formats). Returns an apperror.Error if src is nil or of an
// unrecognized type.
func ScanUTCTime(src any) (time.Time, error) {
	switch v := src.(type) {
	case nil:
		return time.Time{}, apperror.NewInternal(
			"firebird_scan_error",
			"no se pudo decodificar valor de base de datos").
			WithSource("firebird").
			WithField("reason", "nil value scanned into non-nullable timestamp")
	case time.Time:
		return v.UTC(), nil
	case []byte:
		t, err := parseTimestamp(string(v))
		if err != nil {
			return time.Time{}, wrapScanParseError(err, "timestamp", string(v))
		}
		return t.UTC(), nil
	case string:
		t, err := parseTimestamp(v)
		if err != nil {
			return time.Time{}, wrapScanParseError(err, "timestamp", v)
		}
		return t.UTC(), nil
	default:
		return time.Time{}, apperror.NewInternal(
			"firebird_scan_error",
			"no se pudo decodificar valor de base de datos").
			WithSource("firebird").
			WithField("got_type", fmt.Sprintf("%T", src)).
			WithField("target_type", "time.Time")
	}
}

// ScanNullUTCTime is the nullable counterpart of ScanUTCTime. A nil src
// produces an invalid sql.NullTime; anything else is delegated to ScanUTCTime
// and the resulting time is always UTC when Valid.
func ScanNullUTCTime(src any) (sql.NullTime, error) {
	if src == nil {
		return sql.NullTime{Valid: false}, nil
	}
	t, err := ScanUTCTime(src)
	if err != nil {
		return sql.NullTime{}, err
	}
	return sql.NullTime{Time: t, Valid: true}, nil
}

// parseTimestamp accepts the two timestamp string shapes Firebird tooling
// can emit when bytes leak through the driver: RFC3339 and the SQL standard
// "YYYY-MM-DD HH:MM:SS[.fffffff]" without timezone. Strings without a zone
// are interpreted in time.Local to match the driver's default behavior.
func parseTimestamp(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	layouts := []string{
		"2006-01-02 15:04:05.9999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: %q", errUnknownTimestampShape, s)
}

// wrapScanParseError standardizes the apperror produced when a string/bytes
// payload fails to parse into the target type.
func wrapScanParseError(cause error, targetType, raw string) error {
	return apperror.NewInternal(
		"firebird_scan_error",
		"no se pudo decodificar valor de base de datos").
		WithSource("firebird").
		WithError(cause).
		WithField("target_type", targetType).
		WithField("raw_value", raw)
}
