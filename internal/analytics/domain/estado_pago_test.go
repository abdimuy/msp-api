package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestParseEstadoPago(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    domain.EstadoPago
		wantErr error
	}{
		{"SIN_CREDITO is valid", "SIN_CREDITO", domain.EstadoPagoSinCredito, nil},
		{"LIQUIDADO is valid", "LIQUIDADO", domain.EstadoPagoLiquidado, nil},
		{"AL_CORRIENTE is valid", "AL_CORRIENTE", domain.EstadoPagoAlCorriente, nil},
		{"ATRASADO is valid", "ATRASADO", domain.EstadoPagoAtrasado, nil},
		{"MOROSO is valid", "MOROSO", domain.EstadoPagoMoroso, nil},
		{"empty string is invalid", "", "", domain.ErrEstadoPagoInvalido},
		{"lowercase is invalid", "moroso", "", domain.ErrEstadoPagoInvalido},
		{"unknown value is invalid", "BUENO", "", domain.ErrEstadoPagoInvalido},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseEstadoPago(tc.input)
			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
				assert.Empty(t, string(got))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEstadoPagoIsValid(t *testing.T) {
	t.Parallel()

	valid := []domain.EstadoPago{
		domain.EstadoPagoSinCredito,
		domain.EstadoPagoLiquidado,
		domain.EstadoPagoAlCorriente,
		domain.EstadoPagoAtrasado,
		domain.EstadoPagoMoroso,
	}
	for _, ep := range valid {
		ep := ep
		t.Run(string(ep)+"_is_valid", func(t *testing.T) {
			t.Parallel()
			assert.True(t, ep.IsValid())
		})
	}

	invalid := []domain.EstadoPago{"", "moroso", "UNKNOWN"}
	for _, ep := range invalid {
		ep := ep
		t.Run(string(ep)+"_is_invalid", func(t *testing.T) {
			t.Parallel()
			assert.False(t, ep.IsValid())
		})
	}
}

func TestEstadoPagoString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ep   domain.EstadoPago
		want string
	}{
		{domain.EstadoPagoSinCredito, "SIN_CREDITO"},
		{domain.EstadoPagoLiquidado, "LIQUIDADO"},
		{domain.EstadoPagoAlCorriente, "AL_CORRIENTE"},
		{domain.EstadoPagoAtrasado, "ATRASADO"},
		{domain.EstadoPagoMoroso, "MOROSO"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.ep.String())
		})
	}
}
