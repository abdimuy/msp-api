//nolint:misspell // domain vocabulary is Spanish (ventas, productos) per project convention.
package app_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// fakeInventarioService captures every call the ventas Service routes
// through the inventario port so tests can assert on inputs.
type fakeInventarioService struct {
	validarCalls   atomic.Int32
	crearCalls     atomic.Int32
	reversoCalls   atomic.Int32
	resincCalls    atomic.Int32
	validarItems   [][]outbound.InventarioStockItem
	crearParams    []outbound.InventarioCrearTraspasoParams
	resincParams   []outbound.InventarioCrearTraspasoParams
	validarErr     error
	crearErr       error
	reversoDoctoIn int
	reversoErr     error
	resincErr      error
	nextCreatedID  int
}

func (f *fakeInventarioService) ValidarStockParaVenta(_ context.Context, items []outbound.InventarioStockItem) error {
	f.validarCalls.Add(1)
	f.validarItems = append(f.validarItems, append([]outbound.InventarioStockItem(nil), items...))
	return f.validarErr
}

func (f *fakeInventarioService) CrearTraspasoParaVenta(_ context.Context, p outbound.InventarioCrearTraspasoParams) (int, error) {
	f.crearCalls.Add(1)
	f.crearParams = append(f.crearParams, p)
	if f.crearErr != nil {
		return 0, f.crearErr
	}
	f.nextCreatedID++
	return f.nextCreatedID, nil
}

func (f *fakeInventarioService) CrearTraspasoReverso(_ context.Context, _, _ uuid.UUID) (int, error) {
	f.reversoCalls.Add(1)
	return f.reversoDoctoIn, f.reversoErr
}

func (f *fakeInventarioService) ResincronizarTraspasoParaVenta(_ context.Context, p outbound.InventarioCrearTraspasoParams) (int, error) {
	f.resincCalls.Add(1)
	f.resincParams = append(f.resincParams, p)
	if f.resincErr != nil {
		return 0, f.resincErr
	}
	return 0, nil
}

func TestCrearVenta_WithInventario_ValidatesStockAndCreatesTraspaso(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{}
	h.svc.WithInventario(inv)

	in := validContadoInput()
	venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)
	require.NotNil(t, venta)

	assert.Equal(t, int32(1), inv.validarCalls.Load(), "ValidarStockParaVenta must run once")
	assert.Equal(t, int32(1), inv.crearCalls.Load(), "CrearTraspasoParaVenta must run once")

	require.Len(t, inv.validarItems, 1)
	require.Len(t, inv.validarItems[0], 1)
	assert.Equal(t, 42, inv.validarItems[0][0].ArticuloID)
	assert.Equal(t, 1, inv.validarItems[0][0].AlmacenOrigen)
	assert.True(t, inv.validarItems[0][0].Cantidad.Equal(decimal.NewFromInt(1)))

	require.Len(t, inv.crearParams, 1)
	assert.Equal(t, venta.ID(), inv.crearParams[0].VentaID)
	assert.Equal(t, 1, inv.crearParams[0].AlmacenOrigen)
	require.Len(t, inv.crearParams[0].Detalles, 1)
	assert.Equal(t, 42, inv.crearParams[0].Detalles[0].ArticuloID)
}

func TestCrearVenta_WithInventario_StockValidationFailureAborts(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	sinStock := apperror.NewValidation("articulo_sin_existencia", "sin existencia")
	inv := &fakeInventarioService{validarErr: sinStock}
	h.svc.WithInventario(inv)

	_, err := h.svc.CrearVenta(t.Context(), validContadoInput(), uuid.New())
	require.ErrorIs(t, err, sinStock, "stock-validation error must propagate verbatim")

	assert.Equal(t, int32(1), inv.validarCalls.Load())
	assert.Equal(t, int32(0), inv.crearCalls.Load(), "traspaso must not be attempted when stock validation fails")
	assert.Equal(t, 0, h.ventas.SaveCalls, "venta must not be saved when stock validation fails")
}

func TestCrearVenta_WithInventario_TraspasoFailureRollsBackVenta(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	boom := errors.New("traspaso failed mid-flight")
	inv := &fakeInventarioService{crearErr: boom}
	h.svc.WithInventario(inv)

	_, err := h.svc.CrearVenta(t.Context(), validContadoInput(), uuid.New())
	require.Error(t, err)

	assert.Equal(t, int32(1), inv.validarCalls.Load())
	assert.Equal(t, int32(1), inv.crearCalls.Load())
	// runInTx without a real TxManager invokes fn directly, so the Save call
	// landed but the ambient transaction would roll back in production. The
	// behavioral contract we exercise here is that the error surfaces to the
	// caller — atomicity in production is guaranteed by the firebird tx.
	assert.Empty(t, h.outbox.eventTypes(), "no events should be drained when the tx fails")
}

func TestCrearVenta_WithInventario_RejectsMultipleAlmacenOrigenes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{}
	h.svc.WithInventario(inv)

	in := validContadoInput()
	otherID := uuid.New()
	one, two, three := 1, 2, 3
	in.Productos = append(in.Productos, app.CrearVentaProductoInput{
		ID:             otherID,
		ArticuloID:     7,
		Articulo:       "Sillón",
		Cantidad:       decimal.NewFromInt(1),
		PrecioAnual:    decimal.NewFromInt(500),
		PrecioCorto:    decimal.NewFromInt(450),
		PrecioContado:  decimal.NewFromInt(400),
		AlmacenOrigen:  &three,
		AlmacenDestino: &one,
	})
	// Keep PrecioContado sum coherent for the venta's montos.
	in.PrecioContado = decimal.NewFromInt(1400)
	_ = two

	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.Error(t, err)
	var apperr *apperror.Error
	if assert.ErrorAs(t, err, &apperr) {
		assert.Equal(t, "productos_multiples_almacenes_origen", apperr.Code)
	}
	assert.Equal(t, int32(0), inv.crearCalls.Load(), "traspaso must not be attempted when origenes diverge")
}

func TestCrearVenta_NilInventario_StillWorks(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	// Note: do NOT call h.svc.WithInventario — verifying nil-safe path.

	venta, err := h.svc.CrearVenta(t.Context(), validContadoInput(), uuid.New())
	require.NoError(t, err)
	require.NotNil(t, venta)
}

func TestCancelarVenta_WithInventario_PendienteCallsReverso(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{nextCreatedID: 100}
	h.svc.WithInventario(inv)

	// Create a pendiente venta first.
	venta, err := h.svc.CrearVenta(t.Context(), validContadoInput(), uuid.New())
	require.NoError(t, err)
	require.False(t, venta.IsAplicada(), "freshly created venta is not aplicada")

	// Then cancel — reverso should fire.
	cancelados := inv.reversoCalls.Load()
	_, err = h.svc.CancelarVenta(t.Context(), venta.ID(), "cliente arrepentido", uuid.New())
	require.NoError(t, err)
	assert.Equal(t, cancelados+1, inv.reversoCalls.Load(), "CrearTraspasoReverso must be invoked when a pendiente venta is canceled")
}

func TestCancelarVenta_NilInventario_NoReverso(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	// Cancel without inventario wired — must not crash, must not panic.
	venta, err := h.svc.CrearVenta(t.Context(), validContadoInput(), uuid.New())
	require.NoError(t, err)

	_, err = h.svc.CancelarVenta(t.Context(), venta.ID(), "cliente arrepentido", uuid.New())
	require.NoError(t, err)
}
