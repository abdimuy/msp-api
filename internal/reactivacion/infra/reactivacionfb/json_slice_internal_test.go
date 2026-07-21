//nolint:misspell // Spanish domain vocabulary by project convention.
package reactivacionfb

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONSliceArg_EmptySliceSerializesToEmptyArrayNeverNull(t *testing.T) {
	t.Parallel()

	got, err := jsonSliceArg([]string{})
	require.NoError(t, err)
	assert.Equal(t, "[]", got)
}

func TestJSONSliceArg_NilSliceSerializesToEmptyArray(t *testing.T) {
	t.Parallel()

	got, err := jsonSliceArg(nil)
	require.NoError(t, err)
	assert.Equal(t, "[]", got)
}

func TestJSONSliceArg_RoundTripsValues(t *testing.T) {
	t.Parallel()

	in := []string{"deuda", "senal_compra"}
	marshaled, err := jsonSliceArg(in)
	require.NoError(t, err)

	got, err := scanJSONSlice(sql.NullString{String: marshaled, Valid: true})
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

func TestScanJSONSlice_NullYieldsEmptySlice(t *testing.T) {
	t.Parallel()

	got, err := scanJSONSlice(sql.NullString{Valid: false})
	require.NoError(t, err)
	assert.Equal(t, []string{}, got)
}

func TestScanJSONSlice_EmptyStringYieldsEmptySlice(t *testing.T) {
	t.Parallel()

	got, err := scanJSONSlice(sql.NullString{String: "", Valid: true})
	require.NoError(t, err)
	assert.Equal(t, []string{}, got)
}

func TestScanJSONSlice_MalformedJSONReturnsError(t *testing.T) {
	t.Parallel()

	_, err := scanJSONSlice(sql.NullString{String: "not json", Valid: true})
	assert.Error(t, err)
}
