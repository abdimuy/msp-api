package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestNewMontoCLV(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "zero is valid", input: "0", want: "0"},
		{name: "positive integer", input: "1000", want: "1000"},
		{name: "positive decimal", input: "1225.94", want: "1225.94"},
		{name: "very small positive", input: "0.01", want: "0.01"},
		{name: "large value", input: "999999.99", want: "999999.99"},
		{name: "negative is invalid", input: "-0.01", wantErr: domain.ErrMontoCLVNegativo},
		{name: "large negative is invalid", input: "-5000", wantErr: domain.ErrMontoCLVNegativo},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := decimal.RequireFromString(tc.input)
			got, err := domain.NewMontoCLV(v)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
				assert.True(t, got.Decimal().IsZero(), "zero-value MontoCLV on error must have Decimal()==0")
				return
			}

			require.NoError(t, err)
			assert.True(t, decimal.RequireFromString(tc.want).Equal(got.Decimal()),
				"got %s, want %s", got.Decimal(), tc.want)
		})
	}
}

func TestMontoCLV_ZeroValue(t *testing.T) {
	t.Parallel()

	var m domain.MontoCLV
	assert.True(t, m.Decimal().IsZero(), "zero-value MontoCLV must have Decimal()==0")
}
