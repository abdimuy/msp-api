package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/domain"
)

func TestNewTelefono_CanonicalizesToTenDigits(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"4491234567", "4491234567"},         // 10 bare digits
		{"+524491234567", "4491234567"},      // +52 country code
		{"524491234567", "4491234567"},       // 52 country code, no '+'
		{"+52 449 123 4567", "4491234567"},   // spaces
		{"449-123-4567", "4491234567"},       // dashes
		{"(449) 123 4567", "4491234567"},     // parens + spaces
		{"449.123.4567", "4491234567"},       // dots
		{" 4491234567 ", "4491234567"},       // surrounding whitespace
		{"+52 (449) 123-4567", "4491234567"}, // mixed separators with +52
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			tel, err := domain.NewTelefono(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, tel.Value())
		})
	}
}

func TestNewTelefono_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",              // required
		"12345",         // too few digits
		"449123456",     // 9 digits
		"44912345678",   // 11 digits, not a +52 prefix
		"+15551234567",  // US number: 11 digits after stripping, not MX
		"+447911123456", // UK number: not 10 national digits
		"abcdefghij",    // no digits at all
		"52449123456",   // 52 + 9 digits → 11, invalid
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewTelefono(tc)
			assert.Error(t, err)
		})
	}
}

func TestTelefono_StringAndEquals(t *testing.T) {
	t.Parallel()
	a, _ := domain.NewTelefono("4491234567")
	b, _ := domain.NewTelefono("+524491234567")
	c, _ := domain.NewTelefono("4499999999")

	assert.Equal(t, "4491234567", a.String())
	assert.True(t, a.Equals(b), "+52 prefix and bare 10 digits must be equal")
	assert.False(t, a.Equals(c))
}

func TestHydrateTelefono_NoValidation(t *testing.T) {
	t.Parallel()
	tel := domain.HydrateTelefono("anything")
	assert.Equal(t, "anything", tel.Value())
}
