//nolint:misspell // ventas vocabulary is Spanish (cliente, activo, baja) per project convention.
package app_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestAplicarVenta_ReactivarCliente_FeatureON_Credito_Llama verifies that,
// with the feature enabled, applying a CREDITO venta calls
// ReactivarSiEnBaja exactly once with the venta's Microsip cliente_id.
func TestAplicarVenta_ReactivarCliente_FeatureON_Credito_Llama(t *testing.T) {
	t.Parallel()
	h, _, writer, clienteWriter := newAplicarHarness(t)
	h.svc.WithReactivarCliente(true)
	clienteWriter.ReactivarResult = true
	id := seedAprobadaCredito(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, 1, writer.callsCount())
	assert.Equal(t, 1, clienteWriter.reactivarCallsCount(), "ReactivarSiEnBaja must be called once for a CREDITO venta")
	assert.Equal(t, 47913, clienteWriter.LastReactivarCliente, "must pass the venta's Microsip cliente_id")
}

// TestAplicarVenta_ReactivarCliente_FeatureOFF_NoLlama verifies that with the
// flag off (the default), AplicarVenta never calls ReactivarSiEnBaja even for
// a CREDITO venta.
func TestAplicarVenta_ReactivarCliente_FeatureOFF_NoLlama(t *testing.T) {
	t.Parallel()
	h, _, writer, clienteWriter := newAplicarHarness(t)
	// Flag left at default (false) — WithReactivarCliente never called.
	id := seedAprobadaCredito(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.NoError(t, err)
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
	assert.Equal(t, 1, writer.callsCount())
	assert.Equal(t, 0, clienteWriter.reactivarCallsCount(), "ReactivarSiEnBaja must NOT be called when the flag is off")
}

// TestAplicarVenta_ReactivarCliente_Contado_NoLlama verifies that a CONTADO
// venta never triggers reactivation, even with the flag on.
func TestAplicarVenta_ReactivarCliente_Contado_NoLlama(t *testing.T) {
	t.Parallel()
	h, _, writer, clienteWriter := newAplicarHarness(t)
	h.svc.WithReactivarCliente(true)
	id := seedAprobadaContado(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.NoError(t, err)
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
	assert.Equal(t, 1, writer.callsCount())
	assert.Equal(t, 0, clienteWriter.reactivarCallsCount(), "ReactivarSiEnBaja must NOT be called for CONTADO ventas")
}

// TestAplicarVenta_ReactivarCliente_ClienteYaActivo_NoOp verifies that when
// the writer reports reactivado=false (cliente was not 'B'), AplicarVenta
// still succeeds — the reactivation call is a harmless no-op.
func TestAplicarVenta_ReactivarCliente_ClienteYaActivo_NoOp(t *testing.T) {
	t.Parallel()
	h, _, writer, clienteWriter := newAplicarHarness(t)
	h.svc.WithReactivarCliente(true)
	clienteWriter.ReactivarResult = false // cliente was already 'A' — guard did not fire.
	id := seedAprobadaCredito(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.NoError(t, err)
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
	assert.Equal(t, 1, writer.callsCount())
	assert.Equal(t, 1, clienteWriter.reactivarCallsCount(), "the call is still attempted")
}

// TestAplicarVenta_ReactivarCliente_WriterError_RollsBack verifies that an
// error from ReactivarSiEnBaja propagates and the venta is NOT marked
// aplicada — the reactivation runs inside the same transaction as the rest
// of AplicarVenta, so a failure there must roll back the whole apply.
func TestAplicarVenta_ReactivarCliente_WriterError_RollsBack(t *testing.T) {
	t.Parallel()
	h, _, writer, clienteWriter := newAplicarHarness(t)
	h.svc.WithReactivarCliente(true)
	clienteWriter.ReactivarErr = errors.New("microsip reactivar cliente: firebird connection refused")
	id := seedAprobadaCredito(t, h)

	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.Error(t, err)
	assert.Equal(t, 1, writer.callsCount(), "writer already ran before the reactivation step failed")
	assert.Equal(t, 1, clienteWriter.reactivarCallsCount())

	stored, findErr := h.svc.ObtenerVenta(t.Context(), id)
	require.NoError(t, findErr)
	assert.Equal(t, domain.SincronizacionPendiente, stored.Sincronizacion(),
		"venta must not be aplicada when the reactivation step fails")
}
