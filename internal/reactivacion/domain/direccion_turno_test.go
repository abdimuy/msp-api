//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestDireccionTurno_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "entrante", domain.DireccionEntrante.String())
	assert.Equal(t, "saliente", domain.DireccionSaliente.String())
}

func TestDireccionTurno_Valido(t *testing.T) {
	t.Parallel()
	assert.True(t, domain.DireccionEntrante.Valido())
	assert.True(t, domain.DireccionSaliente.Valido())
	assert.False(t, domain.DireccionTurno("").Valido())
	assert.False(t, domain.DireccionTurno("otro").Valido())
}

func TestParseDireccionTurno_Valid(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"entrante", "saliente"} {
		d, err := domain.ParseDireccionTurno(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, d.String())
	}
}

func TestParseDireccionTurno_Invalid(t *testing.T) {
	t.Parallel()
	d, err := domain.ParseDireccionTurno("no_existe")
	require.ErrorIs(t, err, domain.ErrDireccionTurnoInvalido)
	assert.Empty(t, d.String())
}
