package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/domain"
)

func TestNewRFC_AcceptsValidFisica(t *testing.T) {
	t.Parallel()
	r, err := domain.NewRFC("ABCD850101AB1")
	require.NoError(t, err)
	assert.Equal(t, "ABCD850101AB1", r.Value())
	assert.True(t, r.IsFisica())
	assert.False(t, r.IsMoral())
}

func TestNewRFC_AcceptsValidMoral(t *testing.T) {
	t.Parallel()
	r, err := domain.NewRFC("ABC850101AB1")
	require.NoError(t, err)
	assert.Equal(t, "ABC850101AB1", r.Value())
	assert.True(t, r.IsMoral())
	assert.False(t, r.IsFisica())
}

func TestNewRFC_NormalizesInput(t *testing.T) {
	t.Parallel()
	r, err := domain.NewRFC("  abcd850101ab1  ")
	require.NoError(t, err)
	assert.Equal(t, "ABCD850101AB1", r.Value())
}

func TestNewRFC_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"   ",
		"ABCD",
		"ABCD850101",
		"ABCD850101A",    // homoclave too short
		"ABCD850101AB12", // homoclave too long
		"AB850101AB1",    // 2-letter prefix invalid
		"ABCDE850101AB1", // 5-letter prefix invalid
		"ABCD850101AB!",  // invalid char in homoclave
		"ABCDXX0101AB1",  // letters in date
	}
	for _, s := range cases {
		t.Run("invalid_"+s, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewRFC(s)
			assert.Error(t, err, "expected %q to be rejected", s)
		})
	}
}

func TestRFC_String(t *testing.T) {
	t.Parallel()
	r, _ := domain.NewRFC("ABCD850101AB1")
	assert.Equal(t, "ABCD850101AB1", r.String())
}

func TestRFC_Equals(t *testing.T) {
	t.Parallel()
	a, _ := domain.NewRFC("ABCD850101AB1")
	b, _ := domain.NewRFC("abcd850101ab1") // case-insensitive on input
	c, _ := domain.NewRFC("WXYZ850101AB1")

	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))
}

func TestHydrateRFC_NoValidation(t *testing.T) {
	t.Parallel()
	// Hydrate accepts anything (used by repo only).
	r := domain.HydrateRFC(strings.Repeat("X", 5))
	assert.Equal(t, "XXXXX", r.Value())
}
