//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestEstadoMensaje_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "encolado", domain.EstadoEncolado.String())
	assert.Equal(t, "enviado", domain.EstadoEnviado.String())
	assert.Equal(t, "fallido", domain.EstadoFallido.String())
	assert.Equal(t, "bloqueado", domain.EstadoBloqueado.String())
}

func TestEstadoMensaje_Valido(t *testing.T) {
	t.Parallel()
	assert.True(t, domain.EstadoEncolado.Valido())
	assert.True(t, domain.EstadoEnviado.Valido())
	assert.True(t, domain.EstadoFallido.Valido())
	assert.True(t, domain.EstadoBloqueado.Valido())
	assert.False(t, domain.EstadoMensaje("").Valido())
	assert.False(t, domain.EstadoMensaje("otro").Valido())
}

func TestParseEstadoMensaje_Valid(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"encolado", "enviado", "fallido", "bloqueado"} {
		e, err := domain.ParseEstadoMensaje(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, e.String())
	}
}

func TestParseEstadoMensaje_Invalid(t *testing.T) {
	t.Parallel()
	e, err := domain.ParseEstadoMensaje("no_existe")
	require.ErrorIs(t, err, domain.ErrEstadoMensajeInvalido)
	assert.Empty(t, e.String())
}

func TestSenderKind_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "simulado", domain.SenderSimulado.String())
	assert.Equal(t, "real", domain.SenderReal.String())
}

func TestSenderKind_Valido(t *testing.T) {
	t.Parallel()
	assert.True(t, domain.SenderSimulado.Valido())
	assert.True(t, domain.SenderReal.Valido())
	assert.False(t, domain.SenderKind("").Valido())
	assert.False(t, domain.SenderKind("otro").Valido())
}

func TestParseSenderKind_Valid(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"simulado", "real"} {
		s, err := domain.ParseSenderKind(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, s.String())
	}
}

func TestParseSenderKind_Invalid(t *testing.T) {
	t.Parallel()
	s, err := domain.ParseSenderKind("no_existe")
	require.ErrorIs(t, err, domain.ErrSenderKindInvalido)
	assert.Empty(t, s.String())
}
