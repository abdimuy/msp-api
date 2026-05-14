//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
)

func TestReemplazarVendedores_Happy(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	out, err := h.svc.ReemplazarVendedores(t.Context(), ventasapp.ReemplazarVendedoresInput{
		VentaID: *id,
		Vendedores: []ventasapp.CrearVentaVendedorInput{{
			ID: uuid.New(), UsuarioID: uuid.New(), Email: "n@x.com", Nombre: "Nuevo",
		}},
	}, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 1, out.VendedoresCount())
}
