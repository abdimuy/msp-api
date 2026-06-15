//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

func TestParseTipoVenta_ValidContado(t *testing.T) {
	t.Parallel()
	got, err := domain.ParseTipoVenta("CONTADO")
	require.NoError(t, err)
	assert.Equal(t, domain.TipoVentaContado, got)
}

func TestParseTipoVenta_ValidCredito(t *testing.T) {
	t.Parallel()
	got, err := domain.ParseTipoVenta("CREDITO")
	require.NoError(t, err)
	assert.Equal(t, domain.TipoVentaCredito, got)
}

func TestParseTipoVenta_InvalidReturnsErrTipoVentaInvalido(t *testing.T) {
	t.Parallel()
	cases := []string{"contado", "credito", "CASH", "", "OTRO"}
	for _, s := range cases {
		s := s
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseTipoVenta(s)
			require.Error(t, err)
			assert.ErrorIs(t, err, domain.ErrTipoVentaInvalido)
		})
	}
}

func TestTipoVenta_IsValid(t *testing.T) {
	t.Parallel()
	assert.True(t, domain.TipoVentaContado.IsValid())
	assert.True(t, domain.TipoVentaCredito.IsValid())
	assert.False(t, domain.TipoVenta("").IsValid())
	assert.False(t, domain.TipoVenta("contado").IsValid())
	assert.False(t, domain.TipoVenta("OTRO").IsValid())
}

func TestTipoVenta_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "CONTADO", domain.TipoVentaContado.String())
	assert.Equal(t, "CREDITO", domain.TipoVentaCredito.String())
}

func TestTipoVenta_EsCredito(t *testing.T) {
	t.Parallel()
	assert.False(t, domain.TipoVentaContado.EsCredito(), "CONTADO.EsCredito() must be false")
	assert.True(t, domain.TipoVentaCredito.EsCredito(), "CREDITO.EsCredito() must be true")
}
