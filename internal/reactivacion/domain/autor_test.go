//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestAutor_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "cliente", domain.AutorCliente.String())
	assert.Equal(t, "ia", domain.AutorIA.String())
	assert.Equal(t, "humano", domain.AutorHumano.String())
}

func TestAutor_Valido(t *testing.T) {
	t.Parallel()
	assert.True(t, domain.AutorCliente.Valido())
	assert.True(t, domain.AutorIA.Valido())
	assert.True(t, domain.AutorHumano.Valido())
	assert.False(t, domain.Autor("").Valido())
	assert.False(t, domain.Autor("otro").Valido())
}

func TestParseAutor_Valid(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"cliente", "ia", "humano"} {
		a, err := domain.ParseAutor(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, a.String())
	}
}

func TestParseAutor_Invalid(t *testing.T) {
	t.Parallel()
	a, err := domain.ParseAutor("no_existe")
	require.ErrorIs(t, err, domain.ErrAutorInvalido)
	assert.Empty(t, a.String())
}
