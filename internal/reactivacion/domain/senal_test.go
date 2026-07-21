//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestSenal_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "senal_compra", domain.SenalCompra.String())
	assert.Equal(t, "deuda", domain.SenalDeuda.String())
	assert.Equal(t, "confianza_baja", domain.SenalConfianzaBaja.String())
	assert.Equal(t, "pide_humano", domain.SenalPideHumano.String())
	assert.Equal(t, "enojo_loop", domain.SenalEnojoLoop.String())
	assert.Equal(t, "fuera_allowlist", domain.SenalFueraAllowlist.String())
}

func TestSenal_Valido(t *testing.T) {
	t.Parallel()
	validos := []domain.Senal{
		domain.SenalCompra,
		domain.SenalDeuda,
		domain.SenalConfianzaBaja,
		domain.SenalPideHumano,
		domain.SenalEnojoLoop,
		domain.SenalFueraAllowlist,
	}
	for _, s := range validos {
		assert.True(t, s.Valido(), s.String())
	}
	assert.False(t, domain.Senal("").Valido())
	assert.False(t, domain.Senal("otro").Valido())
}

func TestParseSenal_Valid(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"senal_compra", "deuda", "confianza_baja",
		"pide_humano", "enojo_loop", "fuera_allowlist",
	} {
		s, err := domain.ParseSenal(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, s.String())
	}
}

func TestParseSenal_Invalid(t *testing.T) {
	t.Parallel()
	s, err := domain.ParseSenal("no_existe")
	require.ErrorIs(t, err, domain.ErrSenalInvalido)
	assert.Empty(t, s.String())
}
