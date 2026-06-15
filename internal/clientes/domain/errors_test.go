//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func TestErrors_ClienteNotFound(t *testing.T) {
	t.Parallel()
	e, ok := apperror.As(domain.ErrClienteNotFound)
	require.True(t, ok, "ErrClienteNotFound must be an *apperror.Error")
	assert.Equal(t, "cliente_not_found", e.Code)
	assert.Equal(t, apperror.KindNotFound, e.Kind)
}

func TestErrors_VentaNotFound(t *testing.T) {
	t.Parallel()
	e, ok := apperror.As(domain.ErrVentaNotFound)
	require.True(t, ok, "ErrVentaNotFound must be an *apperror.Error")
	assert.Equal(t, "venta_not_found", e.Code)
	assert.Equal(t, apperror.KindNotFound, e.Kind)
}

func TestErrors_TipoVentaInvalido(t *testing.T) {
	t.Parallel()
	e, ok := apperror.As(domain.ErrTipoVentaInvalido)
	require.True(t, ok, "ErrTipoVentaInvalido must be an *apperror.Error")
	assert.Equal(t, "tipo_venta_invalido", e.Code)
	assert.Equal(t, apperror.KindValidation, e.Kind)
}
