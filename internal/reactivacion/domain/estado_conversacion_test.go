//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestEstadoConversacion_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "contactado", domain.EstadoContactado.String())
	assert.Equal(t, "respondio", domain.EstadoRespondio.String())
	assert.Equal(t, "conversando", domain.EstadoConversando.String())
	assert.Equal(t, "escalado", domain.EstadoEscalado.String())
	assert.Equal(t, "interesado", domain.EstadoInteresado.String())
	assert.Equal(t, "enganche", domain.EstadoEnganche.String())
	assert.Equal(t, "descartado", domain.EstadoDescartado.String())
}

func TestEstadoConversacion_Valido(t *testing.T) {
	t.Parallel()
	validos := []domain.EstadoConversacion{
		domain.EstadoContactado,
		domain.EstadoRespondio,
		domain.EstadoConversando,
		domain.EstadoEscalado,
		domain.EstadoInteresado,
		domain.EstadoEnganche,
		domain.EstadoDescartado,
	}
	for _, e := range validos {
		assert.True(t, e.Valido(), e.String())
	}
	assert.False(t, domain.EstadoConversacion("").Valido())
	assert.False(t, domain.EstadoConversacion("otro").Valido())
}

func TestParseEstadoConversacion_Valid(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"contactado", "respondio", "conversando", "escalado",
		"interesado", "enganche", "descartado",
	} {
		e, err := domain.ParseEstadoConversacion(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, e.String())
	}
}

func TestParseEstadoConversacion_Invalid(t *testing.T) {
	t.Parallel()
	e, err := domain.ParseEstadoConversacion("no_existe")
	require.ErrorIs(t, err, domain.ErrEstadoConversacionInvalido)
	assert.Empty(t, e.String())
}

func TestEstadoConversacion_EsTerminal(t *testing.T) {
	t.Parallel()
	terminales := []domain.EstadoConversacion{domain.EstadoEnganche, domain.EstadoDescartado}
	for _, e := range terminales {
		assert.True(t, e.EsTerminal(), e.String())
	}

	noTerminales := []domain.EstadoConversacion{
		domain.EstadoContactado,
		domain.EstadoRespondio,
		domain.EstadoConversando,
		domain.EstadoEscalado,
		domain.EstadoInteresado,
	}
	for _, e := range noTerminales {
		assert.False(t, e.EsTerminal(), e.String())
	}
}
