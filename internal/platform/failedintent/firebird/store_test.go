package firebird

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// TestClampPageSize exercises every branch of the page-size clamping helper.
func TestClampPageSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{"zero returns default", 0, defaultPageSize},
		{"negative returns default", -5, defaultPageSize},
		{"one is valid", 1, 1},
		{"exact max is valid", maxPageSize, maxPageSize},
		{"above max is clamped", maxPageSize + 1, maxPageSize},
		{"well above max is clamped", 999, maxPageSize},
		{"mid-range is unchanged", 50, 50},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, clampPageSize(tc.input))
		})
	}
}

// TestTruncatedToChar covers both bool → 'S'/'N' conversions.
func TestTruncatedToChar(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "S", truncatedToChar(true))
	assert.Equal(t, "N", truncatedToChar(false))
}

// TestCharToTruncated covers both directions plus whitespace-trimming for
// CHAR right-padding.
func TestCharToTruncated(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
	}{
		{"S", true},
		{"S  ", true},   // CHAR right-pad
		{"  S  ", true}, // leading and trailing spaces
		{"N", false},
		{"N  ", false}, // CHAR right-pad
		{"", false},
		{"X", false}, // anything else is false
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, charToTruncated(tc.input))
		})
	}
}

// TestNullableString verifies empty → nil, non-empty → the value.
func TestNullableString(t *testing.T) {
	t.Parallel()
	assert.Nil(t, nullableString(""))
	assert.Equal(t, any("hello"), nullableString("hello"))
	assert.Equal(t, any(" "), nullableString(" ")) // single space is non-empty
}

// TestNullableUUID verifies nil pointer → nil, non-nil pointer → string form.
func TestNullableUUID(t *testing.T) {
	t.Parallel()
	assert.Nil(t, nullableUUID(nil))
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	got := nullableUUID(&id)
	assert.Equal(t, any("00000000-0000-0000-0000-000000000001"), got)
}

// TestNullableTime verifies nil pointer → nil, non-nil pointer → wall-clock value.
func TestNullableTime(t *testing.T) {
	t.Parallel()
	assert.Nil(t, nullableTime(nil))

	ts := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	got := nullableTime(&ts)
	assert.NotNil(t, got)
	// Must be a time.Time in the business timezone (wall-clock conversion).
	gotTime, ok := got.(time.Time)
	assert.True(t, ok, "nullableTime must return time.Time")
	assert.False(t, gotTime.IsZero())
}

// TestBuildListQuery_NoClauses verifies the baseline query has no WHERE clause.
func TestBuildListQuery_NoClauses(t *testing.T) {
	t.Parallel()
	q := buildListQuery(nil)
	assert.Contains(t, q, "SELECT FIRST ?")
	assert.Contains(t, q, "FROM MSP_FAILED_INTENTS")
	assert.NotContains(t, q, "WHERE")
	assert.Contains(t, q, "ORDER BY RECEIVED_AT DESC, ID DESC")
}

// TestBuildListQuery_WithClauses verifies WHERE clauses are joined with AND.
func TestBuildListQuery_WithClauses(t *testing.T) {
	t.Parallel()
	q := buildListQuery([]string{"STATUS = ?", "USUARIO_ID = ?"})
	assert.Contains(t, q, "WHERE STATUS = ? AND USUARIO_ID = ?")
}
