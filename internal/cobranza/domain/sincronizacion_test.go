package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

func TestSincronizacion_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		s     domain.Sincronizacion
		valid bool
	}{
		{"pendiente", domain.SincronizacionPendiente, true},
		{"aplicada", domain.SincronizacionAplicada, true},
		{"unknown_string", domain.Sincronizacion("cancelada"), false},
		{"empty_string", domain.Sincronizacion(""), false},
		{"uppercase", domain.Sincronizacion("PENDIENTE"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.valid, tc.s.IsValid())
		})
	}
}

func TestParseSincronizacion_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  domain.Sincronizacion
	}{
		{"pendiente", domain.SincronizacionPendiente},
		{"aplicada", domain.SincronizacionAplicada},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseSincronizacion(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseSincronizacion_Invalid(t *testing.T) {
	t.Parallel()

	invalids := []string{"", "PENDIENTE", "cancelada", "aplicado", "  pendiente"}

	for _, in := range invalids {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseSincronizacion(in)
			assert.ErrorIs(t, err, domain.ErrSincronizacionInvalida)
		})
	}
}

func TestSincronizacion_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "pendiente", domain.SincronizacionPendiente.String())
	assert.Equal(t, "aplicada", domain.SincronizacionAplicada.String())
}
