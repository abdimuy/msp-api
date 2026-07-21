//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestResultadoDecision_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "propuesto", domain.ResultadoPropuesto.String())
	assert.Equal(t, "aprobado", domain.ResultadoAprobado.String())
	assert.Equal(t, "editado", domain.ResultadoEditado.String())
	assert.Equal(t, "escalado", domain.ResultadoEscalado.String())
}

func TestResultadoDecision_Valido(t *testing.T) {
	t.Parallel()
	validos := []domain.ResultadoDecision{
		domain.ResultadoPropuesto,
		domain.ResultadoAprobado,
		domain.ResultadoEditado,
		domain.ResultadoEscalado,
	}
	for _, r := range validos {
		assert.True(t, r.Valido(), r.String())
	}
	assert.False(t, domain.ResultadoDecision("").Valido())
	assert.False(t, domain.ResultadoDecision("otro").Valido())
}

func TestParseResultadoDecision_Valid(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"propuesto", "aprobado", "editado", "escalado"} {
		r, err := domain.ParseResultadoDecision(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, r.String())
	}
}

func TestParseResultadoDecision_Invalid(t *testing.T) {
	t.Parallel()
	r, err := domain.ParseResultadoDecision("no_existe")
	require.ErrorIs(t, err, domain.ErrResultadoDecisionInvalido)
	assert.Empty(t, r.String())
}
