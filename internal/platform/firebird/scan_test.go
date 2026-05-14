package firebird_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─────────────────────────── ScanDecimal ────────────────────────────────────

func TestScanDecimal_AcceptedTypes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		src   any
		scale int
		want  decimal.Decimal
	}{
		{
			name:  "decimal_passthrough",
			src:   decimal.RequireFromString("123.45"),
			scale: 2,
			want:  decimal.RequireFromString("123.45"),
		},
		{
			name:  "int64_shift_by_scale",
			src:   int64(12345),
			scale: 2,
			want:  decimal.RequireFromString("123.45"),
		},
		{
			name:  "int64_scale_zero",
			src:   int64(42),
			scale: 0,
			want:  decimal.NewFromInt(42),
		},
		{
			name:  "int64_negative",
			src:   int64(-9999),
			scale: 2,
			want:  decimal.RequireFromString("-99.99"),
		},
		{
			name:  "string_parses",
			src:   "987.65",
			scale: 2,
			want:  decimal.RequireFromString("987.65"),
		},
		{
			name:  "bytes_parses",
			src:   []byte("100.00"),
			scale: 2,
			want:  decimal.RequireFromString("100.00"),
		},
		{
			name:  "float64_best_effort",
			src:   float64(3.14),
			scale: 2,
			want:  decimal.NewFromFloat(3.14),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := firebird.ScanDecimal(tc.src, tc.scale)
			require.NoError(t, err)
			assert.True(t, tc.want.Equal(got),
				"want %s, got %s", tc.want.String(), got.String())
		})
	}
}

func TestScanDecimal_RejectsNil(t *testing.T) {
	t.Parallel()
	_, err := firebird.ScanDecimal(nil, 2)
	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok, "must be apperror")
	assert.Equal(t, "firebird_scan_error", appErr.Code)
	assert.Equal(t, apperror.KindInternal, appErr.Kind)
}

func TestScanDecimal_RejectsUnknownType(t *testing.T) {
	t.Parallel()
	_, err := firebird.ScanDecimal(struct{}{}, 2)
	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_scan_error", appErr.Code)
	assert.Contains(t, appErr.Fields["got_type"], "struct {}")
}

func TestScanDecimal_RejectsUnparseableString(t *testing.T) {
	t.Parallel()
	_, err := firebird.ScanDecimal("not-a-number", 2)
	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_scan_error", appErr.Code)
}

// Property: any int64 with a non-negative scale must round-trip back into a
// decimal whose value equals decimal.New(raw, -scale). This is the contract
// the Firebird driver imposes when the column is NUMERIC(_, scale>0).
func TestScanDecimal_Int64Roundtrip_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.Int64().Draw(rt, "raw")
		scale := rapid.IntRange(0, 9).Draw(rt, "scale")

		got, err := firebird.ScanDecimal(raw, scale)
		require.NoError(rt, err)

		want := decimal.New(raw, int32(-scale))
		assert.True(rt, want.Equal(got),
			"raw=%d scale=%d want=%s got=%s", raw, scale, want, got)
	})
}

// ─────────────────────────── ScanNullDecimal ────────────────────────────────

func TestScanNullDecimal_Nil(t *testing.T) {
	t.Parallel()
	got, err := firebird.ScanNullDecimal(nil, 2)
	require.NoError(t, err)
	assert.False(t, got.Valid)
}

func TestScanNullDecimal_NonNil(t *testing.T) {
	t.Parallel()
	got, err := firebird.ScanNullDecimal(int64(15000), 2)
	require.NoError(t, err)
	assert.True(t, got.Valid)
	assert.True(t, decimal.RequireFromString("150.00").Equal(got.Decimal))
}

func TestScanNullDecimal_PropagatesError(t *testing.T) {
	t.Parallel()
	_, err := firebird.ScanNullDecimal(struct{}{}, 2)
	require.Error(t, err)
}

// ─────────────────────────── ScanUTCTime ────────────────────────────────────

func TestScanUTCTime_FromTimeTime(t *testing.T) {
	t.Parallel()
	// A wall-clock value tagged with America/Mexico_City must come back as
	// the same instant expressed in UTC.
	mx, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	src := time.Date(2026, 3, 15, 10, 30, 0, 0, mx)

	got, err := firebird.ScanUTCTime(src)
	require.NoError(t, err)
	assert.Equal(t, time.UTC, got.Location())
	assert.True(t, src.Equal(got), "instant must be preserved")
}

func TestScanUTCTime_FromString_NoZone(t *testing.T) {
	t.Parallel()
	got, err := firebird.ScanUTCTime("2026-03-15 10:30:00")
	require.NoError(t, err)
	assert.Equal(t, time.UTC, got.Location())
	// String without zone is interpreted as time.Local and converted to UTC.
	want := time.Date(2026, 3, 15, 10, 30, 0, 0, time.Local).UTC()
	assert.True(t, want.Equal(got))
}

func TestScanUTCTime_FromString_RFC3339(t *testing.T) {
	t.Parallel()
	got, err := firebird.ScanUTCTime("2026-03-15T10:30:00Z")
	require.NoError(t, err)
	assert.Equal(t, time.UTC, got.Location())
	assert.True(t, time.Date(2026, 3, 15, 10, 30, 0, 0, time.UTC).Equal(got))
}

func TestScanUTCTime_FromBytes(t *testing.T) {
	t.Parallel()
	got, err := firebird.ScanUTCTime([]byte("2026-03-15 10:30:00.123456"))
	require.NoError(t, err)
	assert.Equal(t, time.UTC, got.Location())
}

func TestScanUTCTime_RejectsNil(t *testing.T) {
	t.Parallel()
	_, err := firebird.ScanUTCTime(nil)
	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_scan_error", appErr.Code)
}

func TestScanUTCTime_RejectsBadString(t *testing.T) {
	t.Parallel()
	_, err := firebird.ScanUTCTime("not a timestamp")
	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_scan_error", appErr.Code)
}

func TestScanUTCTime_RejectsUnknownType(t *testing.T) {
	t.Parallel()
	_, err := firebird.ScanUTCTime(42)
	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebird_scan_error", appErr.Code)
}

// Property: ScanUTCTime treats the input as a naked wall-clock written by
// Firebird/Microsip in BusinessTZ (America/Mexico_City), and returns the
// equivalent UTC instant. The input's Location is ignored — only its
// wall-clock fields matter.
func TestScanUTCTime_AlwaysUTC_Property(t *testing.T) {
	t.Parallel()
	businessTZ := firebird.BusinessTZ()
	locations := []*time.Location{time.UTC, time.Local, businessTZ}
	for _, name := range []string{"Europe/Madrid", "Asia/Tokyo"} {
		if loc, err := time.LoadLocation(name); err == nil {
			locations = append(locations, loc)
		}
	}

	rapid.Check(t, func(rt *rapid.T) {
		year := rapid.IntRange(1990, 2100).Draw(rt, "year")
		month := rapid.IntRange(1, 12).Draw(rt, "month")
		day := rapid.IntRange(1, 28).Draw(rt, "day")
		hour := rapid.IntRange(0, 23).Draw(rt, "hour")
		minute := rapid.IntRange(0, 59).Draw(rt, "minute")
		sec := rapid.IntRange(0, 59).Draw(rt, "sec")
		locIdx := rapid.IntRange(0, len(locations)-1).Draw(rt, "loc")

		src := time.Date(year, time.Month(month), day, hour, minute, sec, 0, locations[locIdx])
		got, err := firebird.ScanUTCTime(src)
		require.NoError(rt, err)
		assert.Equal(rt, time.UTC, got.Location())

		// Expected: re-stamp the wall-clock fields with BusinessTZ, then
		// convert to UTC.
		expected := time.Date(
			src.Year(), src.Month(), src.Day(),
			src.Hour(), src.Minute(), src.Second(), src.Nanosecond(),
			businessTZ,
		).UTC()
		assert.True(rt, expected.Equal(got),
			"expected wall-clock in BusinessTZ as UTC: in=%s expected=%s got=%s", src, expected, got)
	})
}

// ─────────────────────────── ScanNullUTCTime ────────────────────────────────

func TestScanNullUTCTime_Nil(t *testing.T) {
	t.Parallel()
	got, err := firebird.ScanNullUTCTime(nil)
	require.NoError(t, err)
	assert.Equal(t, sql.NullTime{Valid: false}, got)
}

func TestScanNullUTCTime_NonNil_UTC(t *testing.T) {
	t.Parallel()
	// Wall-clock 10:30 stamped with America/Mexico_City IS the BusinessTZ —
	// the round-trip preserves the instant exactly.
	mx, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	src := time.Date(2026, 3, 15, 10, 30, 0, 0, mx)

	got, err := firebird.ScanNullUTCTime(src)
	require.NoError(t, err)
	assert.True(t, got.Valid)
	assert.Equal(t, time.UTC, got.Time.Location())
	assert.True(t, src.Equal(got.Time))
}

func TestScanNullUTCTime_PropagatesError(t *testing.T) {
	t.Parallel()
	_, err := firebird.ScanNullUTCTime("not a timestamp")
	require.Error(t, err)
}
