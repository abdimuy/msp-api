//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestClienteSnapshot_NewWithReferencia(t *testing.T) {
	t.Parallel()
	nombre, err := domain.NewNombreCliente("María García López")
	require.NoError(t, err)
	ref := "CASA AZUL ESQUINA"
	snap, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{
		Nombre:     nombre,
		Referencia: &ref,
	})
	require.NoError(t, err)
	require.NotNil(t, snap.Referencia())
	assert.Equal(t, "CASA AZUL ESQUINA", *snap.Referencia())
}

func TestClienteSnapshot_NewReferenciaTooLong(t *testing.T) {
	t.Parallel()
	nombre, err := domain.NewNombreCliente("Ana Rodríguez")
	require.NoError(t, err)
	// 100 runes — one more than the allowed max (99).
	long := strings.Repeat("A", 100)
	_, err = domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{
		Nombre:     nombre,
		Referencia: &long,
	})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "cliente_referencia_demasiado_larga", ae.Code)
}

func TestClienteSnapshot_NewReferenciaTrimmed(t *testing.T) {
	t.Parallel()
	nombre, err := domain.NewNombreCliente("Luis Hernández")
	require.NoError(t, err)
	ref := "  casa verde frente  "
	snap, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{
		Nombre:     nombre,
		Referencia: &ref,
	})
	require.NoError(t, err)
	require.NotNil(t, snap.Referencia())
	// Referencia is trimmed and folded to ALL CAPS (Microsip convention).
	assert.Equal(t, "CASA VERDE FRENTE", *snap.Referencia())
}

func TestClienteSnapshot_NewReferenciaBlankIsNil(t *testing.T) {
	t.Parallel()
	nombre, err := domain.NewNombreCliente("Pedro Ramírez")
	require.NoError(t, err)
	blank := "   "
	snap, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{
		Nombre:     nombre,
		Referencia: &blank,
	})
	require.NoError(t, err)
	assert.Nil(t, snap.Referencia(), "blank referencia must normalize to nil")
}

func TestClienteSnapshot_NewReferenciaExactly99Runes(t *testing.T) {
	t.Parallel()
	nombre, err := domain.NewNombreCliente("Carmen Flores")
	require.NoError(t, err)
	exact := strings.Repeat("B", 99)
	snap, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{
		Nombre:     nombre,
		Referencia: &exact,
	})
	require.NoError(t, err)
	require.NotNil(t, snap.Referencia())
	assert.Equal(t, exact, *snap.Referencia())
}
