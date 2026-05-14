package firebird_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

func TestBusinessTZ_IsAmericaMexicoCity(t *testing.T) {
	t.Parallel()
	loc := firebird.BusinessTZ()
	require.NotNil(t, loc)
	assert.Equal(t, "America/Mexico_City", loc.String())
}

func TestToWallClock_ShiftsUTCToCDMX(t *testing.T) {
	t.Parallel()
	// 2026-05-13 18:00:00 UTC == 2026-05-13 12:00:00 in CDMX (UTC-6).
	utc := time.Date(2026, 5, 13, 18, 0, 0, 0, time.UTC)
	wall := firebird.ToWallClock(utc)
	assert.Equal(t, 2026, wall.Year())
	assert.Equal(t, time.May, wall.Month())
	assert.Equal(t, 13, wall.Day())
	assert.Equal(t, 12, wall.Hour())
	assert.Equal(t, 0, wall.Minute())
}

func TestToWallClock_ZeroPassesThrough(t *testing.T) {
	t.Parallel()
	assert.True(t, firebird.ToWallClock(time.Time{}).IsZero())
}

func TestFromWallClock_RestoresUTC(t *testing.T) {
	t.Parallel()
	// Driver-style time: wall-clock 12:00 stamped with whatever Location.
	// FromWallClock must reinterpret as CDMX and return UTC == 18:00.
	wallLikeFromDriver := time.Date(2026, 5, 13, 12, 0, 0, 0, time.Local)
	got := firebird.FromWallClock(wallLikeFromDriver)
	assert.Equal(t, 2026, got.Year())
	assert.Equal(t, time.May, got.Month())
	assert.Equal(t, 13, got.Day())
	assert.Equal(t, 18, got.Hour())
	assert.Equal(t, time.UTC, got.Location())
}

func TestRoundTrip_ToThenFrom_IsIdentity(t *testing.T) {
	t.Parallel()
	cases := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 13, 12, 34, 56, 789_000_000, time.UTC),
		time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
		// DST boundary in CDMX is irrelevant (no DST since 2022), but
		// check anyway with a date around the historical change.
		time.Date(2026, 4, 5, 7, 30, 0, 0, time.UTC),
	}
	for _, in := range cases {
		t.Run(in.Format(time.RFC3339), func(t *testing.T) {
			t.Parallel()
			wall := firebird.ToWallClock(in)
			back := firebird.FromWallClock(wall)
			assert.True(t, in.Equal(back),
				"round-trip failed: in=%s wall=%s back=%s", in, wall, back)
		})
	}
}

func TestFromWallClock_ZeroPassesThrough(t *testing.T) {
	t.Parallel()
	assert.True(t, firebird.FromWallClock(time.Time{}).IsZero())
}
