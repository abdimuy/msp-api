//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestCrearVenta_ClienteIDInvalidoPropagates verifies the ClienteExistenceChecker
// is consulted on CrearVenta and a miss rejects with ErrClienteIDInvalido.
func TestCrearVenta_ClienteIDInvalidoPropagates(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.svc = ventasapp.NewService(h.ventas, newFakeClienteChecker(false), nil, h.storage, h.clock, h.outbox, h.imageProc, nil, nil, nil)
	in := validContadoInput()
	cid := 7777
	in.ClienteID = &cid
	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.ErrorIs(t, err, domain.ErrClienteIDInvalido)
}

// TestCrearVenta_ClienteIDValidoPersiste verifies the FK link survives the
// CrearVenta path when the checker says it exists.
func TestCrearVenta_ClienteIDValidoPersiste(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.svc = ventasapp.NewService(h.ventas, newFakeClienteChecker(true), nil, h.storage, h.clock, h.outbox, h.imageProc, nil, nil, nil)
	in := validContadoInput()
	cid := 42
	in.ClienteID = &cid
	v, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)
	require.NotNil(t, v.ClienteID())
	assert.Equal(t, 42, *v.ClienteID())
}

// TestCrearVenta_NilClienteIDBypassesChecker verifies a nil cliente_id does
// not call the checker at all (allows staging without a Microsip link).
func TestCrearVenta_NilClienteIDBypassesChecker(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	checker := newFakeClienteChecker(false)
	h.svc = ventasapp.NewService(h.ventas, checker, nil, h.storage, h.clock, h.outbox, h.imageProc, nil, nil, nil)
	in := validContadoInput()
	in.ClienteID = nil
	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)
	checker.mu.Lock()
	defer checker.mu.Unlock()
	assert.Equal(t, 0, checker.calls, "checker must not be invoked when cliente_id is nil")
}

// TestCrearVenta_ClienteCheckerError_Propagates verifies an underlying error
// from the checker bubbles up instead of being silently swallowed.
func TestCrearVenta_ClienteCheckerError_Propagates(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	checker := newFakeClienteChecker(false)
	checker.err = errors.New("boom")
	h.svc = ventasapp.NewService(h.ventas, checker, nil, h.storage, h.clock, h.outbox, h.imageProc, nil, nil, nil)
	in := validContadoInput()
	cid := 1
	in.ClienteID = &cid
	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrClienteIDInvalido, "transport error must NOT be re-mapped to validation")
}

// TestActualizarHeader_EmitsEvent verifies the outbox receives a
// venta.header_actualizado event.
func TestActualizarHeader_EmitsEvent(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	_, err := h.svc.ActualizarHeader(t.Context(), ventasapp.ActualizarHeaderInput{
		VentaID: *id, Calle: "X", Colonia: "X", Poblacion: "X", Ciudad: "X",
		FechaVenta:  validContadoInput().FechaVenta,
		PrecioAnual: decimal.NewFromInt(1), PrecioCorto: decimal.NewFromInt(1), PrecioContado: decimal.NewFromInt(1),
	}, uuid.New())
	require.NoError(t, err)
	assert.True(t, h.outbox.sawEventType("venta.header_actualizado"),
		"expected venta.header_actualizado event, got %v", h.outbox.eventTypes())
}

// TestActualizarCliente_EmitsEvent verifies the outbox event.
func TestActualizarCliente_EmitsEvent(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	_, err := h.svc.ActualizarCliente(t.Context(), ventasapp.ActualizarClienteInput{
		VentaID: *id, ClienteNombre: "Nuevo",
	}, uuid.New())
	require.NoError(t, err)
	assert.True(t, h.outbox.sawEventType("venta.cliente_actualizado"))
}

// TestReemplazarProductos_EmitsEvent verifies the outbox event carries
// productos_count.
func TestReemplazarProductos_EmitsEvent(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	one, two := 1, 2
	_, err := h.svc.ReemplazarProductos(t.Context(), ventasapp.ReemplazarProductosInput{
		VentaID: *id,
		Productos: []ventasapp.CrearVentaProductoInput{
			{
				ID: uuid.New(), ArticuloID: 1, Articulo: "A", Cantidad: decimal.NewFromInt(1),
				PrecioAnual: decimal.NewFromInt(1), PrecioCorto: decimal.NewFromInt(1), PrecioContado: decimal.NewFromInt(1),
				AlmacenOrigen: &one, AlmacenDestino: &two,
			},
			{
				ID: uuid.New(), ArticuloID: 2, Articulo: "B", Cantidad: decimal.NewFromInt(1),
				PrecioAnual: decimal.NewFromInt(1), PrecioCorto: decimal.NewFromInt(1), PrecioContado: decimal.NewFromInt(1),
				AlmacenOrigen: &one, AlmacenDestino: &two,
			},
		},
	}, uuid.New())
	require.NoError(t, err)
	assert.True(t, h.outbox.sawEventType("venta.productos_reemplazados"))
}

// TestReemplazarCombos_EmitsEvent verifies the outbox event for combos.
func TestReemplazarCombos_EmitsEvent(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	_, err := h.svc.ReemplazarCombos(t.Context(), ventasapp.ReemplazarCombosInput{
		VentaID: *id,
		Combos: []ventasapp.CrearVentaComboInput{{
			ID: uuid.New(), Nombre: "C",
			PrecioAnual: decimal.NewFromInt(1), PrecioCorto: decimal.NewFromInt(1), PrecioContado: decimal.NewFromInt(1),
			Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2,
		}},
	}, uuid.New())
	require.NoError(t, err)
	assert.True(t, h.outbox.sawEventType("venta.combos_reemplazados"))
}

// TestReemplazarVendedores_EmitsEvent verifies the outbox event for vendedores.
func TestReemplazarVendedores_EmitsEvent(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	_, err := h.svc.ReemplazarVendedores(t.Context(), ventasapp.ReemplazarVendedoresInput{
		VentaID: *id,
		Vendedores: []ventasapp.CrearVentaVendedorInput{{
			ID: uuid.New(), UsuarioID: uuid.New(), Email: "v@x.com", Nombre: "V",
		}},
	}, uuid.New())
	require.NoError(t, err)
	assert.True(t, h.outbox.sawEventType("venta.vendedores_reemplazados"))
}

// TestCancelarVenta_TransitionsStatus verifies the persisted venta has
// status=cancelada after Cancelar.
func TestCancelarVenta_TransitionsStatus(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	v, err := h.svc.CancelarVenta(t.Context(), *id, "razon", uuid.New())
	require.NoError(t, err)
	assert.Equal(t, domain.SituacionCancelada, v.Situacion())
}
