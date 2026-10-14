package clienteshttp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// Contrato §6: pago_recibido_id es "uuid" | null (nunca omitido) y num_fotos
// siempre presente.
func TestToPagoDetalleDTO_PagoRecibidoIDYNumFotos(t *testing.T) {
	t.Parallel()
	conApp := toPagoDetalleDTO(outbound.PagoDetalle{PagoRecibidoID: "7a1f39e4-0000-4000-8000-000000000001", NumFotos: 2})
	b, err := json.Marshal(conApp)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"pago_recibido_id":"7a1f39e4-0000-4000-8000-000000000001"`)
	assert.Contains(t, string(b), `"num_fotos":2`)

	oficina := toPagoDetalleDTO(outbound.PagoDetalle{})
	b, err = json.Marshal(oficina)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"pago_recibido_id":null`)
	assert.Contains(t, string(b), `"num_fotos":0`)
}
