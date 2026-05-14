//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestActualizarCliente_Happy(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)

	out, err := h.svc.ActualizarCliente(t.Context(), ventasapp.ActualizarClienteInput{
		VentaID:       *id,
		ClienteNombre: "Cliente Corregido",
	}, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, "Cliente Corregido", out.Cliente().Nombre().Value())
}

func TestActualizarCliente_ClienteIDInvalido(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	h.svc = ventasapp.NewService(h.ventas, newFakeClienteChecker(false), h.storage, h.clock, h.outbox, h.imageProc, nil)

	cid := 999
	_, err := h.svc.ActualizarCliente(t.Context(), ventasapp.ActualizarClienteInput{
		VentaID:       *id,
		ClienteID:     &cid,
		ClienteNombre: "X",
	}, uuid.New())
	require.ErrorIs(t, err, domain.ErrClienteIDInvalido)
}

func TestActualizarCliente_ClienteIDValido(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	h.svc = ventasapp.NewService(h.ventas, newFakeClienteChecker(false, 42), h.storage, h.clock, h.outbox, h.imageProc, nil)

	cid := 42
	out, err := h.svc.ActualizarCliente(t.Context(), ventasapp.ActualizarClienteInput{
		VentaID:       *id,
		ClienteID:     &cid,
		ClienteNombre: "OK",
	}, uuid.New())
	require.NoError(t, err)
	require.NotNil(t, out.ClienteID())
	assert.Equal(t, 42, *out.ClienteID())
}
