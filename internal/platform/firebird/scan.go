package firebird

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
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
	case int32:
		// Firebird stores NUMERIC(p,s) with p ≤ 9 as a 32-bit integer; the
		// driver hands those back as int32 rather than int64. Promote and
		// recover the decimal point via scale.
		return decimal.New(int64(v), int32(-scale)), nil
	case int16:
		// NUMERIC(p,s) with p ≤ 4 lands as int16 — same promotion path.
		return decimal.New(int64(v), int32(-scale)), nil
	case *big.Int:
		// nakagami/firebirdsql emits *big.Int for SUM/AVG aggregates over
		// NUMERIC columns (and for NUMERIC values that exceed int64 range).
		// The big.Int carries the unscaled value; scale recovers the decimal
		// point, identical to the int64 case.
		if v == nil {
			return decimal.Decimal{}, apperror.NewInternal(
				"firebird_scan_error",
				"no se pudo decodificar valor de base de datos").
				WithSource("firebird").
				WithField("reason", "nil *big.Int scanned into non-nullable decimal")
		}
		return decimal.NewFromBigInt(v, int32(-scale)), nil
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
// Firebird stores timestamps as naked wall-clock values; Microsip writes
// them in the business timezone (see BusinessTZ — America/Mexico_City). The
// nakagami/firebirdsql driver hands the wall-clock back tagged with
// time.Local by default, which corrupts the instant on any process whose
// local TZ is not BusinessTZ. We collapse that ambiguity at the repository
// boundary by reinterpreting the wall-clock in BusinessTZ and returning the
// equivalent UTC instant.
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
		// The driver hands a wall-clock value stamped with time.Local — the
		// instant it claims is wrong. Reinterpret the wall-clock as
		// BusinessTZ and return the corresponding UTC moment.
		return FromWallClock(v), nil
	case []byte:
		return scanTimestampString(string(v))
	case string:
		return scanTimestampString(v)
	default:
		return time.Time{}, apperror.NewInternal(
			"firebird_scan_error",
			"no se pudo decodificar valor de base de datos").
			WithSource("firebird").
			WithField("got_type", fmt.Sprintf("%T", src)).
			WithField("target_type", "time.Time")
	}
}

// scanTimestampString parses a Firebird timestamp string. RFC3339 strings
// carry explicit TZ info and are honored as-is (.UTC() to normalize). Naked
// "YYYY-MM-DD HH:MM:SS[.fffffff]" strings have no zone, are interpreted as
// wall-clock in BusinessTZ, and returned in UTC.
func scanTimestampString(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	t, err := parseTimestamp(s)
	if err != nil {
		return time.Time{}, wrapScanParseError(err, "timestamp", s)
	}
	return FromWallClock(t), nil
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

// parseTimestamp parses the SQL-standard zone-less timestamp shapes Firebird
// can emit through the driver. Returned time.Time is stamped with BusinessTZ
// since the input had no explicit zone. RFC3339 handling lives in
// scanTimestampString to keep that path TZ-honoring.
func parseTimestamp(s string) (time.Time, error) {
	layouts := []string{
		"2006-01-02 15:04:05.9999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, BusinessTZ()); err == nil {
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
