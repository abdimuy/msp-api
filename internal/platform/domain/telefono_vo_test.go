package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/domain"
)

func TestNewTelefono_AcceptsValidE164(t *testing.T) {
	t.Parallel()
	cases := []string{
		"+15551234567",     // US, 11 digits
		"+524491234567",    // MX, 12 digits
		"+447911123456",    // UK, 12 digits
		"+11",              // minimum length: country code + 1 subscriber digit
		"+999999999999999", // maximum length: 15 digits after '+'
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			tel, err := domain.NewTelefono(tc)
			require.NoError(t, err)
			assert.Equal(t, tc, tel.Value())
		})
	}
}

func TestNewTelefono_RejectsNonE164(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",                  // required
		"5551234567",        // missing '+'
		"+0123456789",       // country code starts with 0
		"+",                 // no digits
		"+1",                // too short (need at least 2 digits)
		"+1234567890123456", // 16 digits, above max
		"+1-555-123-4567",   // separators
		"(555) 123-4567",    // local format with parens/space/dash
		"+52 449 123 4567",  // spaces
		"+52.449.123.4567",  // dots
		" +15551234567",     // leading whitespace
		"+15551234567 ",     // trailing whitespace
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
	a, _ := domain.NewTelefono("+15551234567")
	b, _ := domain.NewTelefono("+15551234567")
	c, _ := domain.NewTelefono("+15559999999")

	assert.Equal(t, "+15551234567", a.String())
	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))
}

func TestHydrateTelefono_NoValidation(t *testing.T) {
	t.Parallel()
	tel := domain.HydrateTelefono("anything")
	assert.Equal(t, "anything", tel.Value())
}
