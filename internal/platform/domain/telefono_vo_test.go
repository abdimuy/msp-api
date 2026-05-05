package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/domain"
)

func TestNewTelefono_StripsSeparators(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{"5551234567", "5551234567"},
		{"(555) 123-4567", "5551234567"},
		{"555.123.4567", "5551234567"},
		{"+52 555 123 4567", "525551234567"},
		{"+1-555-123-4567", "15551234567"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			tel, err := domain.NewTelefono(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.want, tel.Value())
		})
	}
}

func TestNewTelefono_RejectsTooShortOrTooLong(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"123",
		"123456789",      // 9 digits, just below minimum
		"12345678901234", // 14 digits, above max
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
	a, _ := domain.NewTelefono("(555) 123-4567")
	b, _ := domain.NewTelefono("555.123.4567")
	c, _ := domain.NewTelefono("5559999999")

	assert.Equal(t, "5551234567", a.String())
	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))
}

func TestHydrateTelefono_NoValidation(t *testing.T) {
	t.Parallel()
	tel := domain.HydrateTelefono("anything")
	assert.Equal(t, "anything", tel.Value())
}
