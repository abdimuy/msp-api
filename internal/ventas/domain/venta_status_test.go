package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestVentaStatus_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   domain.VentaStatus
		want bool
	}{
		{domain.StatusBorrador, true},
		{domain.StatusAprobada, true},
		{domain.StatusCancelada, true},
		{"otro", false},
		{"", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.in.IsValid(), "status=%q", c.in)
	}
}

func TestParseVentaStatus(t *testing.T) {
	t.Parallel()
	s, err := domain.ParseVentaStatus("borrador")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusBorrador, s)

	_, err = domain.ParseVentaStatus("WUT")
	require.Error(t, err)
}

func TestVentaStatus_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "borrador", domain.StatusBorrador.String())
}
