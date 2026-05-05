package pagination_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/platform/pagination"
)

func TestFromRequest_Defaults(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/items", nil)
	p, err := pagination.FromRequest(r)
	require.NoError(t, err)
	assert.Empty(t, p.After)
	assert.Equal(t, pagination.DefaultLimit, p.Limit)
}

func TestFromRequest_AcceptsValidParams(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/items?after=abc&limit=25", nil)
	p, err := pagination.FromRequest(r)
	require.NoError(t, err)
	assert.Equal(t, "abc", p.After)
	assert.Equal(t, 25, p.Limit)
}

func TestFromRequest_RejectsBadLimit(t *testing.T) {
	t.Parallel()
	cases := []string{"abc", "-1", "0", "9999"}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/items?limit="+raw, nil)
			_, err := pagination.FromRequest(r)
			assert.Error(t, err)
		})
	}
}

func TestEncodeDecodeCursor_RoundTrip(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	original := pagination.Cursor{
		Schema:    1,
		UpdatedAt: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		ID:        &id,
	}
	encoded, err := pagination.EncodeCursor(original)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)

	decoded, err := pagination.DecodeCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, original.Schema, decoded.Schema)
	assert.True(t, original.UpdatedAt.Equal(decoded.UpdatedAt))
	require.NotNil(t, decoded.ID)
	assert.Equal(t, id, *decoded.ID)
}

func TestEncodeCursor_DefaultsSchemaTo1(t *testing.T) {
	t.Parallel()
	encoded, err := pagination.EncodeCursor(pagination.Cursor{}) // Schema=0
	require.NoError(t, err)

	decoded, err := pagination.DecodeCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, 1, decoded.Schema)
}

func TestDecodeCursor_EmptyReturnsZero(t *testing.T) {
	t.Parallel()
	c, err := pagination.DecodeCursor("")
	require.NoError(t, err)
	assert.Equal(t, pagination.Cursor{}, c)
}

func TestDecodeCursor_GarbageRejected(t *testing.T) {
	t.Parallel()
	cases := []string{"!!!not-base64!!!", "Zm9vYmFy"} // second is "foobar" which isn't JSON
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			_, err := pagination.DecodeCursor(c)
			assert.Error(t, err)
		})
	}
}

// Property-based: any valid Cursor we encode must round-trip.
func TestCursor_RoundTripProperty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		schema := rapid.IntRange(1, 100).Draw(rt, "schema")
		offset := rapid.IntRange(0, 1_000_000).Draw(rt, "offset")
		hasID := rapid.Bool().Draw(rt, "hasID")

		c := pagination.Cursor{Schema: schema, Offset: &offset}
		if hasID {
			id := uuid.New()
			c.ID = &id
		}

		encoded, err := pagination.EncodeCursor(c)
		require.NoError(rt, err)
		decoded, err := pagination.DecodeCursor(encoded)
		require.NoError(rt, err)
		assert.Equal(rt, c.Schema, decoded.Schema)
		require.NotNil(rt, decoded.Offset)
		assert.Equal(rt, *c.Offset, *decoded.Offset)
	})
}
