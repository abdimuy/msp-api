//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestSegmento_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "recien_liquidado", domain.SegmentoRecienLiquidado.String())
	assert.Equal(t, "por_liquidar_hueco", domain.SegmentoPorLiquidarHueco.String())
}

func TestSegmento_Valido(t *testing.T) {
	t.Parallel()
	assert.True(t, domain.SegmentoRecienLiquidado.Valido())
	assert.True(t, domain.SegmentoPorLiquidarHueco.Valido())
	assert.False(t, domain.Segmento("").Valido())
	assert.False(t, domain.Segmento("otro").Valido())
}

func TestParseSegmento_Valid(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"recien_liquidado", "por_liquidar_hueco"} {
		s, err := domain.ParseSegmento(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, s.String())
	}
}

func TestParseSegmento_Invalid(t *testing.T) {
	t.Parallel()
	s, err := domain.ParseSegmento("no_existe")
	require.ErrorIs(t, err, domain.ErrSegmentoInvalido)
	assert.Empty(t, s.String())
}
