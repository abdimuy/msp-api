//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestAccion_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "responder", domain.AccionResponder.String())
	assert.Equal(t, "escalar", domain.AccionEscalar.String())
}

func TestAccion_Valido(t *testing.T) {
	t.Parallel()
	assert.True(t, domain.AccionResponder.Valido())
	assert.True(t, domain.AccionEscalar.Valido())
	assert.False(t, domain.Accion("").Valido())
	assert.False(t, domain.Accion("otro").Valido())
}

func TestParseAccion_Valid(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"responder", "escalar"} {
		a, err := domain.ParseAccion(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, a.String())
	}
}

func TestParseAccion_Invalid(t *testing.T) {
	t.Parallel()
	a, err := domain.ParseAccion("no_existe")
	require.ErrorIs(t, err, domain.ErrAccionInvalido)
	assert.Empty(t, a.String())
}
