package ventfb

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClampPageSize_Bounds exercises every branch of clampPageSize without
// touching a database.
func TestClampPageSize_Bounds(t *testing.T) {
	t.Parallel()
	assert.Equal(t, defaultPageSize, clampPageSize(0))
	assert.Equal(t, defaultPageSize, clampPageSize(-5))
	assert.Equal(t, maxPageSize, clampPageSize(maxPageSize+50))
	assert.Equal(t, 42, clampPageSize(42))
}

// TestDecodeCursor_RoundTrip exercises the happy path.
func TestDecodeCursor_RoundTrip(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	now := time.Date(2026, 5, 11, 9, 30, 0, 0, time.UTC)
	encoded := encodeCursor(now, id)
	gotT, gotID, err := decodeCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, now, gotT)
	assert.Equal(t, id, gotID)
}

// TestDecodeCursor_EmptyIsFirstPage validates that an empty cursor decodes
// to zero values with no error — signaling "first page".
func TestDecodeCursor_EmptyIsFirstPage(t *testing.T) {
	t.Parallel()
	gotT, gotID, err := decodeCursor("")
	require.NoError(t, err)
	assert.True(t, gotT.IsZero())
	assert.Equal(t, uuid.Nil, gotID)
}

// TestDecodeCursor_InvalidBase64 hits the base64-decode error path.
func TestDecodeCursor_InvalidBase64(t *testing.T) {
	t.Parallel()
	_, _, err := decodeCursor("!!!not-base64!!!")
	require.Error(t, err)
}

// TestDecodeCursor_MissingSeparator hits the SplitN branch.
func TestDecodeCursor_MissingSeparator(t *testing.T) {
	t.Parallel()
	bad := base64.RawURLEncoding.EncodeToString([]byte("no-separator-here"))
	_, _, err := decodeCursor(bad)
	require.Error(t, err)
}

// TestDecodeCursor_BadTime hits the time.Parse error path.
func TestDecodeCursor_BadTime(t *testing.T) {
	t.Parallel()
	bad := base64.RawURLEncoding.EncodeToString([]byte("not-a-time|" + uuid.NewString()))
	_, _, err := decodeCursor(bad)
	require.Error(t, err)
}

// TestDecodeCursor_BadUUID hits the uuid.Parse error path.
func TestDecodeCursor_BadUUID(t *testing.T) {
	t.Parallel()
	bad := base64.RawURLEncoding.EncodeToString([]byte("2026-05-11T09:30:00Z|not-a-uuid"))
	_, _, err := decodeCursor(bad)
	require.Error(t, err)
}
