//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

// Tests for Service.validateVendedorUsuarios — pre-INSERT existence check
// that prevents unknown vendedor usuario_ids from surfacing as a generic
// 409 firebird_fk_violation. See domain.ErrVendedorUsuarioNoEncontrado.

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestCrearVenta_VendedorUsuario_HappyPath wires a checker that recognizes
// the vendedor on the input. CrearVenta must succeed and the checker must
// have been consulted exactly once.
func TestCrearVenta_VendedorUsuario_HappyPath(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	in := validContadoInput()
	knownID := in.Vendedores[0].UsuarioID

	checker := newFakeUsuarioChecker(knownID)
	h.svc = ventasapp.NewService(h.ventas, nil, checker, h.storage, h.clock, h.outbox, h.imageProc, nil, nil, nil, nil)

	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 1, checker.calls, "checker must be consulted exactly once")
	assert.Equal(t, 1, h.ventas.SaveCalls, "repo.Save must be invoked on success")
}

// TestCrearVenta_VendedorUsuario_Missing pins the contract: an unknown
// usuario_id causes CrearVenta to fail with ErrVendedorUsuarioNoEncontrado
// BEFORE any repo write, and the missing ids ride on the apperror so HTTP
// callers can name the offender.
func TestCrearVenta_VendedorUsuario_Missing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	in := validContadoInput()
	// known set is empty → the vendedor's usuario_id will be reported missing.

	checker := newFakeUsuarioChecker()
	h.svc = ventasapp.NewService(h.ventas, nil, checker, h.storage, h.clock, h.outbox, h.imageProc, nil, nil, nil, nil)

	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.ErrorIs(t, err, domain.ErrVendedorUsuarioNoEncontrado)
	assert.Equal(t, 1, checker.calls)
	assert.Zero(t, h.ventas.SaveCalls,
		"validation failure must short-circuit before any repo write")
}

// TestCrearVenta_VendedorUsuario_CheckerError pins the same contract as
// validateClienteID: when the checker itself errors (e.g. MSP_USUARIOS is
// unreachable), the underlying error surfaces unwrapped — never conflated
// with ErrVendedorUsuarioNoEncontrado, which would lie about the cause.
func TestCrearVenta_VendedorUsuario_CheckerError(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	in := validContadoInput()

	dbDown := errors.New("msp_usuarios unreachable")
	checker := &fakeUsuarioChecker{err: dbDown}
	h.svc = ventasapp.NewService(h.ventas, nil, checker, h.storage, h.clock, h.outbox, h.imageProc, nil, nil, nil, nil)

	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.ErrorIs(t, err, dbDown,
		"checker errors must surface unwrapped, not be conflated with ErrVendedorUsuarioNoEncontrado")
	require.NotErrorIs(t, err, domain.ErrVendedorUsuarioNoEncontrado)
	assert.Zero(t, h.ventas.SaveCalls)
}

// TestCrearVenta_VendedorUsuario_NilChecker preserves the in-memory test
// path: a Service built with nil usuarios checker silently skips the
// validation. Documented in NewService.
func TestCrearVenta_VendedorUsuario_NilChecker(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	// h.svc is already constructed with nil usuarios in newHarness.

	in := validContadoInput()
	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)
}
