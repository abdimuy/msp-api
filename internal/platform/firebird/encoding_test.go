package firebird

// White-box package so Win1252 internals are accessible.
// These tests are pure unit tests: no FIREBIRD=1 gate, no TestMain, no DB.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWin1252_Value_Empty verifies that encoding an empty string returns
// []byte(nil) without error.
func TestWin1252_Value_Empty(t *testing.T) {
	t.Parallel()
	v, err := Win1252("").Value()
	require.NoError(t, err)
	assert.Equal(t, []byte(nil), v)
}

// TestWin1252_Value_PlainASCII verifies that pure-ASCII strings round-trip
// as identical bytes (ASCII is a subset of both UTF-8 and Windows-1252).
func TestWin1252_Value_PlainASCII(t *testing.T) {
	t.Parallel()
	v, err := Win1252("hello").Value()
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), v)
}

// TestWin1252_Value_SpanishAccents verifies that a typical Spanish string
// encodes to Windows-1252 bytes that are shorter than the UTF-8 representation
// (because UTF-8 uses two bytes per accented character while Win1252 uses one).
func TestWin1252_Value_SpanishAccents(t *testing.T) {
	t.Parallel()
	input := "Mérida áéíóúñÑ"
	v, err := Win1252(input).Value()
	require.NoError(t, err)
	b, ok := v.([]byte)
	require.True(t, ok, "expected []byte result")
	// Windows-1252 encodes each accented char in 1 byte; UTF-8 uses 2.
	assert.Less(t, len(b), len([]byte(input)),
		"Win1252 encoding must be shorter than UTF-8 for accented chars")
}

// TestWin1252_Value_RejectsEmoji verifies that a character outside the
// Windows-1252 range (emoji is not encodable) produces an error containing
// "cannot encode".
func TestWin1252_Value_RejectsEmoji(t *testing.T) {
	t.Parallel()
	_, err := Win1252("hola 😀").Value()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot encode")
}

// TestWin1252_Scan_FromBytes verifies that feeding Windows-1252 bytes for
// "Mérida" (M=0x4D, é=0xE9, r=0x72, i=0x69, d=0x64, a=0x61) decodes to the
// correctly accented UTF-8 string.
func TestWin1252_Scan_FromBytes(t *testing.T) {
	t.Parallel()
	raw := []byte{0x4D, 0xE9, 0x72, 0x69, 0x64, 0x61} // "Mérida" in Win1252
	var w Win1252
	require.NoError(t, w.Scan(raw))
	assert.Equal(t, Win1252("Mérida"), w)
}

// TestWin1252_Scan_FromNil verifies that a nil src produces an empty string
// with no error.
func TestWin1252_Scan_FromNil(t *testing.T) {
	t.Parallel()
	var w Win1252
	require.NoError(t, w.Scan(nil))
	assert.Equal(t, Win1252(""), w)
}

// TestWin1252_Scan_FromString verifies that the driver may hand a string
// instead of []byte (some Firebird driver versions do this) and the scan
// still decodes correctly.
func TestWin1252_Scan_FromString(t *testing.T) {
	t.Parallel()
	// Build the Win1252 "Mérida" as a Go string with raw bytes (not UTF-8).
	raw := string([]byte{0x4D, 0xE9, 0x72, 0x69, 0x64, 0x61})
	var w Win1252
	require.NoError(t, w.Scan(raw))
	assert.Equal(t, Win1252("Mérida"), w)
}

// TestWin1252_Scan_RejectsWrongType verifies that an unsupported source type
// (e.g. int) returns an error.
func TestWin1252_Scan_RejectsWrongType(t *testing.T) {
	t.Parallel()
	var w Win1252
	err := w.Scan(123)
	require.Error(t, err)
}

// TestRoundTrip_AllSpanishOrthography verifies that the full set of common
// Spanish diacritics and punctuation (including the euro sign, which is a
// Windows-1252 addition over Latin-1) survives an encode→decode round-trip
// without data loss.
func TestRoundTrip_AllSpanishOrthography(t *testing.T) {
	t.Parallel()
	original := "México áéíóúñÑÁÉÍÓÚ ¿¡ €"

	// Encode to Win1252.
	v, err := Win1252(original).Value()
	require.NoError(t, err)
	b, ok := v.([]byte)
	require.True(t, ok)

	// Decode back to UTF-8.
	var decoded Win1252
	require.NoError(t, decoded.Scan(b))
	assert.Equal(t, original, string(decoded), "round-trip must be lossless")
}

// TestEncodeWin1252Ptr_Nil verifies that a nil pointer returns (nil, nil) —
// the SQL NULL representation.
func TestEncodeWin1252Ptr_Nil(t *testing.T) {
	t.Parallel()
	v, err := EncodeWin1252Ptr(nil)
	require.NoError(t, err)
	assert.Nil(t, v)
}

// TestEncodeWin1252Ptr_NonNil verifies that a non-nil pointer encodes the
// pointed-to string exactly as EncodeWin1252 would.
func TestEncodeWin1252Ptr_NonNil(t *testing.T) {
	t.Parallel()
	s := "Guadalajara"
	ptrResult, err := EncodeWin1252Ptr(&s)
	require.NoError(t, err)

	directResult, err := EncodeWin1252(s)
	require.NoError(t, err)

	assert.Equal(t, directResult, ptrResult)
}
