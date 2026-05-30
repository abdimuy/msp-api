package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestEstadoRegistro_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   domain.EstadoRegistro
		want bool
	}{
		{domain.EstadoActive, true},
		{domain.EstadoDeleted, true},
		{"borrador", false},
		{"", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.in.IsValid(), "estado=%q", c.in)
	}
}

func TestParseEstadoRegistro(t *testing.T) {
	t.Parallel()
	e, err := domain.ParseEstadoRegistro("active")
	require.NoError(t, err)
	assert.Equal(t, domain.EstadoActive, e)

	_, err = domain.ParseEstadoRegistro("WUT")
	require.ErrorIs(t, err, domain.ErrEstadoRegistroInvalido)
}

func TestEstadoRegistro_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "active", domain.EstadoActive.String())
}

func TestSituacion_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   domain.Situacion
		want bool
	}{
		{domain.SituacionBorrador, true},
		{domain.SituacionRevisada, true},
		{domain.SituacionAprobada, true},
		{domain.SituacionCancelada, true},
		{"aplicada", false},
		{"", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.in.IsValid(), "situacion=%q", c.in)
	}
}

func TestParseSituacion(t *testing.T) {
	t.Parallel()
	s, err := domain.ParseSituacion("revisada")
	require.NoError(t, err)
	assert.Equal(t, domain.SituacionRevisada, s)

	_, err = domain.ParseSituacion("WUT")
	require.ErrorIs(t, err, domain.ErrSituacionInvalida)
}

func TestSincronizacion_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   domain.Sincronizacion
		want bool
	}{
		{domain.SincronizacionPendiente, true},
		{domain.SincronizacionAplicada, true},
		{"borrador", false},
		{"", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.in.IsValid(), "sincronizacion=%q", c.in)
	}
}

func TestParseSincronizacion(t *testing.T) {
	t.Parallel()
	s, err := domain.ParseSincronizacion("aplicada")
	require.NoError(t, err)
	assert.Equal(t, domain.SincronizacionAplicada, s)

	_, err = domain.ParseSincronizacion("WUT")
	require.ErrorIs(t, err, domain.ErrSincronizacionInvalida)
}
