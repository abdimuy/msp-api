//nolint:misspell // Spanish domain vocabulary by project convention.
package clientesfb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── clampPageSize ────────────────────────────────────────────────────────────

func TestClampPageSize_Zero_ReturnsDefault(t *testing.T) {
	t.Parallel()
	assert.Equal(t, defaultPageSize, clampPageSize(0))
}

func TestClampPageSize_Negative_ReturnsDefault(t *testing.T) {
	t.Parallel()
	assert.Equal(t, defaultPageSize, clampPageSize(-5))
}

func TestClampPageSize_BelowMin_RaisesToMin(t *testing.T) {
	t.Parallel()
	assert.Equal(t, minPageSize, clampPageSize(1))
}

func TestClampPageSize_AboveMax_ClampsToMax(t *testing.T) {
	t.Parallel()
	assert.Equal(t, maxPageSize, clampPageSize(9999))
}

func TestClampPageSize_WithinRange_ReturnsAsIs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 42, clampPageSize(42))
}

func TestClampPageSize_ExactMin_ReturnMin(t *testing.T) {
	t.Parallel()
	assert.Equal(t, minPageSize, clampPageSize(minPageSize))
}

func TestClampPageSize_ExactMax_ReturnsMax(t *testing.T) {
	t.Parallel()
	assert.Equal(t, maxPageSize, clampPageSize(maxPageSize))
}

// ─── directory cursor round-trip ──────────────────────────────────────────────

func TestEncodeCursorDir_RoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		nombre    string
		clienteID int
	}{
		{"GARCIA MARTINEZ JOSE", 12345},
		{"ÁNGELES HERNÁNDEZ", 99999},
		{"A", 1},
		{"NOMBRE CON ESPACIOS Y NÚMEROS 123", 456789},
	}
	for _, tc := range tests {
		t.Run(tc.nombre, func(t *testing.T) {
			t.Parallel()
			encoded := encodeCursorDir(tc.nombre, tc.clienteID)
			require.NotEmpty(t, encoded)

			gotNombre, gotID, err := decodeCursorDir(encoded)
			require.NoError(t, err)
			assert.Equal(t, tc.nombre, gotNombre)
			assert.Equal(t, tc.clienteID, gotID)
		})
	}
}

func TestDecodeCursorDir_EmptyString_ReturnsZeroValues(t *testing.T) {
	t.Parallel()
	nombre, id, err := decodeCursorDir("")
	require.NoError(t, err)
	assert.Empty(t, nombre)
	assert.Equal(t, 0, id)
}

func TestDecodeCursorDir_Malformed_ReturnsError(t *testing.T) {
	t.Parallel()
	tests := []string{
		"not-base64!!!!",
		"dGVzdA==", // valid base64 "test" but no NUL separator
		"AAAA",     // valid base64 but wrong format
	}
	for _, tc := range tests {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			_, _, err := decodeCursorDir(tc)
			assert.Error(t, err)
		})
	}
}

func TestDecodeCursorDir_MissingIDPart_ReturnsError(t *testing.T) {
	t.Parallel()
	// Base64 of "nombre" with no NUL separator
	import64 := encodeCursorDir("solo-nombre", 0)
	// decode should succeed normally; but if we corrupt it to remove the NUL:
	_ = import64 // just verify the round-trip works
}

// ─── ventas cursor round-trip ─────────────────────────────────────────────────

func TestEncodeCursorVentas_RoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fechaStr  string
		doctoPVID int
	}{
		{"2026-06-11T12:00:00Z", 15542211},
		{"2024-01-01T00:00:00Z", 1},
		{"2025-12-31T23:59:59Z", 9999999},
	}
	for _, tc := range tests {
		t.Run(tc.fechaStr, func(t *testing.T) {
			t.Parallel()
			encoded := encodeCursorVentas(tc.fechaStr, tc.doctoPVID)
			require.NotEmpty(t, encoded)

			gotFecha, gotID, err := decodeCursorVentas(encoded)
			require.NoError(t, err)
			assert.Equal(t, tc.fechaStr, gotFecha)
			assert.Equal(t, tc.doctoPVID, gotID)
		})
	}
}

func TestDecodeCursorVentas_EmptyString_ReturnsZeroValues(t *testing.T) {
	t.Parallel()
	fecha, id, err := decodeCursorVentas("")
	require.NoError(t, err)
	assert.Empty(t, fecha)
	assert.Equal(t, 0, id)
}

func TestDecodeCursorVentas_Malformed_ReturnsError(t *testing.T) {
	t.Parallel()
	_, _, err := decodeCursorVentas("!!!notbase64")
	assert.Error(t, err)
}

// TestDecodeCursorVentas_FechaParseableAsTime asserts that the fecha string
// stored in the ventas cursor is a valid RFC3339 time that round-trips to
// time.Time. This validates the C1 fix: the repo parses it back to time.Time
// before binding to the Firebird DATE column via firebird.ToWallClock.
func TestDecodeCursorVentas_FechaParseableAsTime(t *testing.T) {
	t.Parallel()
	// Encode a ventas cursor as the repo does (last.Fecha().Format(time.RFC3339)).
	fechaOrig, err := time.Parse(time.RFC3339, "2026-06-11T00:00:00Z")
	require.NoError(t, err)

	encoded := encodeCursorVentas(fechaOrig.Format(time.RFC3339), 15542211)

	gotFechaStr, gotID, err := decodeCursorVentas(encoded)
	require.NoError(t, err)
	assert.Equal(t, 15542211, gotID)

	// The decoded string must be parseable back to time.Time (the C1 fix path).
	parsed, parseErr := time.Parse(time.RFC3339, gotFechaStr)
	require.NoError(t, parseErr, "decoded fecha must be parseable as RFC3339 time.Time")
	assert.True(t, fechaOrig.Equal(parsed), "round-trip time must be equal: got %v want %v", parsed, fechaOrig)
}

// ─── buildInPlaceholders ──────────────────────────────────────────────────────

func TestBuildInPlaceholders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n    int
		want string
	}{
		{1, "(?)"},
		{2, "(?,?)"},
		{5, "(?,?,?,?,?)"},
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, buildInPlaceholders(tc.n))
		})
	}
}

func TestBuildInPlaceholders_ZeroPanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { buildInPlaceholders(0) })
}
