//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

// Tests for Service.validateClienteID — exercised indirectly through
// ActualizarCliente which is the canonical entry point that validates the
// cliente_id link before mutating the aggregate.

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestService_ValidateClienteID_CheckerError pins the contract: when the
// cliente checker errors (e.g. Microsip's CLIENTES table is unreachable),
// the underlying error surfaces unwrapped — it must NOT be misclassified as
// ErrClienteIDInvalido, which would tell the caller "the id is bad" when in
// reality "we couldn't tell if the id is bad".
func TestService_ValidateClienteID_CheckerError(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)

	dbDown := errors.New("microsip clientes unreachable")
	checker := &fakeClienteChecker{err: dbDown}
	h.svc = ventasapp.NewService(h.ventas, checker, nil, h.storage, h.clock, h.outbox, h.imageProc, nil, nil, nil)

	cid := 42
	_, err := h.svc.ActualizarCliente(t.Context(), ventasapp.ActualizarClienteInput{
		VentaID:       *id,
		ClienteID:     &cid,
		ClienteNombre: "X",
	}, uuid.New())
	require.ErrorIs(t, err, dbDown,
		"checker errors must surface unwrapped, not be conflated with ErrClienteIDInvalido")
	require.NotErrorIs(t, err, domain.ErrClienteIDInvalido)
	assert.Zero(t, h.ventas.UpdateCalls,
		"validation failure must short-circuit before any repo write")
}
