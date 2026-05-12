package firebird //nolint:testpackage // pagination helpers are package-internal

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func TestEncodeDecodeCursor_RoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		t    time.Time
		id   uuid.UUID
	}{
		{"epoch zero", time.Unix(0, 0).UTC(), uuid.MustParse("00000000-0000-0000-0000-000000000001")},
		{"recent", time.Date(2026, 5, 10, 9, 30, 15, 123456789, time.UTC), uuid.New()},
		{"far future", time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC), uuid.New()},
		{
			"local time normalized to UTC",
			time.Date(2026, 1, 1, 12, 0, 0, 0, time.FixedZone("CST", -6*3600)),
			uuid.New(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cursor := encodeCursor(tc.t, tc.id)
			require.NotEmpty(t, cursor)

			gotT, gotID, err := decodeCursor(cursor)
			require.NoError(t, err)
			assert.True(t, gotT.Equal(tc.t.UTC()),
				"expected %s, got %s", tc.t.UTC(), gotT)
			assert.Equal(t, tc.id, gotID)
		})
	}
}

func TestDecodeCursor_Empty_ReturnsZeroValues(t *testing.T) {
	t.Parallel()

	gotT, gotID, err := decodeCursor("")
	require.NoError(t, err)
	assert.True(t, gotT.IsZero())
	assert.Equal(t, uuid.Nil, gotID)
}

func TestDecodeCursor_Malformed_ReturnsValidationError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		{"not base64", "not!base64!"},
		{"base64 but no separator", base64.RawURLEncoding.EncodeToString([]byte("foobar"))},
		{"bad timestamp", base64.RawURLEncoding.EncodeToString([]byte("not-a-time|00000000-0000-0000-0000-000000000001"))},
		{"bad uuid", base64.RawURLEncoding.EncodeToString([]byte("2026-01-01T00:00:00Z|not-a-uuid"))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := decodeCursor(tc.input)
			require.Error(t, err)
			appErr, ok := apperror.As(err)
			require.True(t, ok, "expected apperror.Error, got %T", err)
			assert.Equal(t, "invalid_cursor", appErr.Code)
			assert.Equal(t, apperror.KindValidation, appErr.Kind)
		})
	}
}

// TestMapUniqueViolation_Nil covers the nil-pass-through branch.
func TestMapUniqueViolation_Nil(t *testing.T) {
	t.Parallel()
	replacement := apperror.NewConflict("replacement", "x")
	require.NoError(t, mapUniqueViolation(nil, replacement))
}

// TestMapUniqueViolation_NonAppError covers the !ok branch of
// isUniqueViolation and the "return err" branch of mapUniqueViolation: a
// generic error (not an apperror) passes through unchanged.
func TestMapUniqueViolation_NonAppError(t *testing.T) {
	t.Parallel()
	replacement := apperror.NewConflict("replacement", "x")
	sentinel := errors.New("plain stdlib error")
	got := mapUniqueViolation(sentinel, replacement)
	require.Error(t, got)
	assert.Equal(t, sentinel, got)
}

// TestMapUniqueViolation_OtherAppError covers the "apperror but not unique
// violation" branch: an apperror with a different code is returned as-is.
func TestMapUniqueViolation_OtherAppError(t *testing.T) {
	t.Parallel()
	replacement := apperror.NewConflict("replacement", "x")
	other := apperror.NewInternal("firebird_other", "y")
	got := mapUniqueViolation(other, replacement)
	require.Error(t, got)
	appErr, ok := apperror.As(got)
	require.True(t, ok)
	assert.Equal(t, "firebird_other", appErr.Code)
}

// TestMapUniqueViolation_UniqueRewritesToReplacement covers the
// already-mapped happy path so the test surface around mapUniqueViolation is
// self-contained.
func TestMapUniqueViolation_UniqueRewritesToReplacement(t *testing.T) {
	t.Parallel()
	replacement := apperror.NewConflict("replacement", "x")
	unique := apperror.NewConflict("firebird_unique_violation", "duplicate").WithSource("firebird")
	got := mapUniqueViolation(unique, replacement)
	require.Error(t, got)
	appErr, ok := apperror.As(got)
	require.True(t, ok)
	assert.Equal(t, "replacement", appErr.Code)
}

func TestClampPageSize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    int
		expected int
	}{
		{"zero falls back to default", 0, defaultPageSize},
		{"negative falls back to default", -5, defaultPageSize},
		{"too large clamps to max", maxPageSize + 50, maxPageSize},
		{"in range passes through", 17, 17},
		{"exactly min", minPageSize, minPageSize},
		{"exactly max", maxPageSize, maxPageSize},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, clampPageSize(tc.input))
		})
	}
}
