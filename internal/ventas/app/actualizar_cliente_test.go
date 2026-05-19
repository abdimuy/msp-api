//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

import (
	"errors"
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
	h.svc = ventasapp.NewService(h.ventas, newFakeClienteChecker(false), nil, h.storage, h.clock, h.outbox, h.imageProc, nil)

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
	h.svc = ventasapp.NewService(h.ventas, newFakeClienteChecker(false, 42), nil, h.storage, h.clock, h.outbox, h.imageProc, nil)

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

// TestActualizarCliente_UpdateClienteRepoError_PropagatesAndDrainsNoEvents
// pins the failure-path event contract: when the repo's UpdateCliente errors
// after the aggregate has been mutated in memory, the error surfaces AND no
// outbox event is enqueued — events drain only when the persistence step
// succeeds, so a downstream consumer never sees a "ClienteActualizado" event
// for a row that did not actually change.
func TestActualizarCliente_UpdateClienteRepoError_PropagatesAndDrainsNoEvents(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)

	boom := errors.New("repo update exploded")
	h.ventas.UpdateErr = boom

	_, err := h.svc.ActualizarCliente(t.Context(), ventasapp.ActualizarClienteInput{
		VentaID:       *id,
		ClienteNombre: "Mutado pero no persistido",
	}, uuid.New())
	require.ErrorIs(t, err, boom)
	assert.Empty(t, h.outbox.snapshot(),
		"no outbox event must escape when persistence failed — events are post-commit only")
}
