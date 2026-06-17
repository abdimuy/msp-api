package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestNewScoreCredito(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   int
		want    int
		wantErr error
	}{
		{name: "zero is valid", input: 0, want: 0},
		{name: "100 is valid", input: 100, want: 100},
		{name: "50 is valid", input: 50, want: 50},
		{name: "1 is valid", input: 1, want: 1},
		{name: "99 is valid", input: 99, want: 99},
		{name: "negative is invalid", input: -1, wantErr: domain.ErrScoreCreditoFueraDeRango},
		{name: "101 is invalid", input: 101, wantErr: domain.ErrScoreCreditoFueraDeRango},
		{name: "large negative is invalid", input: -100, wantErr: domain.ErrScoreCreditoFueraDeRango},
		{name: "large positive is invalid", input: 1000, wantErr: domain.ErrScoreCreditoFueraDeRango},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.NewScoreCredito(tc.input)

			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
				assert.Equal(t, domain.ScoreCredito{}, got)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Int())
		})
	}
}

func TestScoreCreditoString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{50, "50"},
		{100, "100"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()

			s, err := domain.NewScoreCredito(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.want, s.String())
		})
	}
}
