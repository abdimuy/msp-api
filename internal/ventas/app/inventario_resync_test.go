//nolint:misspell // domain vocabulary is Spanish (ventas, productos, combos, almacenes) per project convention.
package app_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/app"
)

// validComboInput returns a CrearVentaInput with one combo (AlmacenOrigen=5,
// AlmacenDestino=6) and one combo-child producto (ArticuloID=77, Cantidad=3,
// ComboID pointing to the combo). No standalone productos are included so the
// test can assert exclusively on the combo-child detalle.
func validComboInput() app.CrearVentaInput {
	comboID := uuid.New()
	productoID := uuid.New()
	vendedorID := uuid.New()
	return app.CrearVentaInput{
		ID:            uuid.New(),
		ClienteNombre: "María Gonzalez",
		Calle:         "Blvd. Independencia",
		Colonia:       "Centro",
		Poblacion:     "Torreón",
		Ciudad:        "Coahuila",
		Latitud:       25.5428,
		Longitud:      -103.4068,
		FechaVenta:    time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:     "CONTADO",
		PrecioAnual:   decimal.NewFromInt(3000),
		PrecioCorto:   decimal.NewFromInt(2700),
		PrecioContado: decimal.NewFromInt(2400),
		Combos: []app.CrearVentaComboInput{{
			ID:             comboID,
			Nombre:         "Combo Sala",
			PrecioAnual:    decimal.NewFromInt(3000),
			PrecioCorto:    decimal.NewFromInt(2700),
			PrecioContado:  decimal.NewFromInt(2400),
			Cantidad:       decimal.NewFromInt(1),
			AlmacenOrigen:  5,
			AlmacenDestino: 6,
		}},
		Productos: []app.CrearVentaProductoInput{{
			ID:            productoID,
			ArticuloID:    77,
			Articulo:      "Sillón Principal",
			Cantidad:      decimal.NewFromInt(3),
			PrecioAnual:   decimal.NewFromInt(3000),
			PrecioCorto:   decimal.NewFromInt(2700),
			PrecioContado: decimal.NewFromInt(2400),
			ComboID:       &comboID,
			// AlmacenOrigen intentionally nil: combo-child inherits from combo.
			AlmacenOrigen:  nil,
			AlmacenDestino: nil,
		}},
		Vendedores: []app.CrearVentaVendedorInput{{
			ID:        vendedorID,
			UsuarioID: uuid.New(),
			Email:     "vendedor@muebleriamsp.mx",
			Nombre:    "Carlos Vendedor",
		}},
	}
}

// TestCrearVenta_WithInventario_ComboChildrenReserved is a regression test for
// bug #3: combo-child productos were previously skipped when building the
// traspaso detalles, so they were never reserved in inventory. After the fix,
// buildTraspasoDetallesFromVenta includes them by inheriting AlmacenOrigen from
// the parent combo.
func TestCrearVenta_WithInventario_ComboChildrenReserved(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{}
	h.svc.WithInventario(inv)

	in := validComboInput()
	venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)
	require.NotNil(t, venta)

	assert.Equal(t, int32(1), inv.validarCalls.Load(), "ValidarStockParaVenta must run once")
	assert.Equal(t, int32(1), inv.crearCalls.Load(), "CrearTraspasoParaVenta must run once")

	// The stock validation should include the combo child's articuloID with
	// the parent combo's almacenOrigen (5).
	require.Len(t, inv.validarItems, 1)
	require.Len(t, inv.validarItems[0], 1, "exactly one stock item: the combo-child producto")
	assert.Equal(t, 77, inv.validarItems[0][0].ArticuloID)
	assert.Equal(t, 5, inv.validarItems[0][0].AlmacenOrigen, "combo child inherits almacenOrigen from parent combo")
	assert.True(t, inv.validarItems[0][0].Cantidad.Equal(decimal.NewFromInt(3)))

	// The traspaso detalle should include the combo child.
	require.Len(t, inv.crearParams, 1)
	assert.Equal(t, venta.ID(), inv.crearParams[0].VentaID)
	assert.Equal(t, 5, inv.crearParams[0].AlmacenOrigen)
	require.Len(t, inv.crearParams[0].Detalles, 1)
	assert.Equal(t, 77, inv.crearParams[0].Detalles[0].ArticuloID)
	assert.True(t, inv.crearParams[0].Detalles[0].Cantidad.Equal(decimal.NewFromInt(3)))
}

// TestBuildTraspasoDetalles_ViaCrearVenta_ComboChildrenIncluded verifies that
// the full detalles set (including combo children) is passed to
// CrearTraspasoParaVenta when using a combo-child input.
func TestBuildTraspasoDetalles_ViaCrearVenta_ComboChildrenIncluded(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{}
	h.svc.WithInventario(inv)

	in := validComboInput()
	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)

	require.Len(t, inv.crearParams, 1)
	assert.Equal(t, 5, inv.crearParams[0].AlmacenOrigen, "almacenOrigen derived from parent combo")
	require.Len(t, inv.crearParams[0].Detalles, 1, "one detalle for the combo-child producto")
	assert.Equal(t, 77, inv.crearParams[0].Detalles[0].ArticuloID)
}

// TestReemplazarProductos_WithInventario_CallsResincronizar verifies that
// ReemplazarProductos wires the resync call correctly: after the productos are
// replaced, ResincronizarTraspasoParaVenta is called once with the new detalles.
func TestReemplazarProductos_WithInventario_CallsResincronizar(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{}
	h.svc.WithInventario(inv)

	// Seed a venta so there is something to mutate.
	ventaID := h.seedVenta(t)

	// Replace with a new standalone producto.
	newID := uuid.New()
	three := 3
	four := 4
	_, err := h.svc.ReemplazarProductos(t.Context(), app.ReemplazarProductosInput{
		VentaID: *ventaID,
		Productos: []app.CrearVentaProductoInput{{
			ID:             newID,
			ArticuloID:     99,
			Articulo:       "Comedor 4 sillas",
			Cantidad:       decimal.NewFromInt(1),
			PrecioAnual:    decimal.NewFromInt(800),
			PrecioCorto:    decimal.NewFromInt(750),
			PrecioContado:  decimal.NewFromInt(700),
			AlmacenOrigen:  &three,
			AlmacenDestino: &four,
		}},
	}, uuid.New())
	require.NoError(t, err)

	assert.Equal(t, int32(1), inv.resincCalls.Load(), "ResincronizarTraspasoParaVenta must be called once")
	require.Len(t, inv.resincParams, 1)
	assert.Equal(t, *ventaID, inv.resincParams[0].VentaID)
	assert.Equal(t, 3, inv.resincParams[0].AlmacenOrigen)
	require.Len(t, inv.resincParams[0].Detalles, 1)
	assert.Equal(t, 99, inv.resincParams[0].Detalles[0].ArticuloID)
}

// TestReemplazarCombos_WithInventario_CallsResincronizar verifies that
// ReemplazarCombos also triggers ResincronizarTraspasoParaVenta after the
// combos replacement is persisted.
func TestReemplazarCombos_WithInventario_CallsResincronizar(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{}
	h.svc.WithInventario(inv)

	// Seed a venta with a standalone producto (AlmacenOrigen=1) so the venta
	// is valid. We then replace combos (to an empty set) to trigger the resync.
	ventaID := h.seedVenta(t)

	_, err := h.svc.ReemplazarCombos(t.Context(), app.ReemplazarCombosInput{
		VentaID: *ventaID,
		Combos:  []app.CrearVentaComboInput{},
	}, uuid.New())
	require.NoError(t, err)

	assert.Equal(t, int32(1), inv.resincCalls.Load(), "ResincronizarTraspasoParaVenta must be called once after ReemplazarCombos")
}

// TestReemplazarProductos_WithInventario_StockFailureAborts verifies that when
// stock validation fails inside ResincronizarTraspasoParaVenta (post-reversal,
// inside the inventario module), the error propagates to the caller.
//
// The ventas layer no longer pre-validates stock on the edit path — that check
// was removed because it double-counted the old reservation as unavailable.
// Stock is now validated inside the inventario resync after the active directo
// is reversed, so the existencia read reflects the released stock.
func TestReemplazarProductos_WithInventario_StockFailureAborts(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	sinStock := newSinStockError()
	// The stock error is returned by ResincronizarTraspasoParaVenta itself
	// (simulating the inventario module's post-reversal check failing).
	inv := &fakeInventarioService{resincErr: sinStock}
	h.svc.WithInventario(inv)

	// Seed the venta via the same harness. Since inv.validarErr is not set,
	// CrearVenta's stock check passes, CrearTraspasoParaVenta is called (and
	// returns ok). The venta is saved to h.ventas so ReemplazarProductos can
	// find it. The resincErr only triggers on ResincronizarTraspasoParaVenta.
	ventaID := h.seedVenta(t)

	three := 3
	four := 4
	_, err := h.svc.ReemplazarProductos(t.Context(), app.ReemplazarProductosInput{
		VentaID: *ventaID,
		Productos: []app.CrearVentaProductoInput{{
			ID:             uuid.New(),
			ArticuloID:     55,
			Articulo:       "Silla Gamer",
			Cantidad:       decimal.NewFromInt(2),
			PrecioAnual:    decimal.NewFromInt(1200),
			PrecioCorto:    decimal.NewFromInt(1100),
			PrecioContado:  decimal.NewFromInt(1000),
			AlmacenOrigen:  &three,
			AlmacenDestino: &four,
		}},
	}, uuid.New())
	require.Error(t, err, "stock failure from resync must propagate as an error")
	// ResincronizarTraspasoParaVenta was called (and returned the stock error).
	assert.Equal(t, int32(1), inv.resincCalls.Load(), "resync must be called; it returns the stock error internally")
}

// TestReemplazarProductos_NilInventario_StillSucceeds verifies that
// ReemplazarProductos works correctly when no InventarioService is wired.
func TestReemplazarProductos_NilInventario_StillSucceeds(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	// Note: do NOT call h.svc.WithInventario — verifying nil-safe path.
	ventaID := h.seedVenta(t)

	one, two := 1, 2
	_, err := h.svc.ReemplazarProductos(t.Context(), app.ReemplazarProductosInput{
		VentaID: *ventaID,
		Productos: []app.CrearVentaProductoInput{{
			ID:             uuid.New(),
			ArticuloID:     88,
			Articulo:       "Mesa",
			Cantidad:       decimal.NewFromInt(1),
			PrecioAnual:    decimal.NewFromInt(500),
			PrecioCorto:    decimal.NewFromInt(450),
			PrecioContado:  decimal.NewFromInt(400),
			AlmacenOrigen:  &one,
			AlmacenDestino: &two,
		}},
	}, uuid.New())
	require.NoError(t, err)
}

// TestReemplazarCombos_NilInventario_StillSucceeds verifies that
// ReemplazarCombos works correctly when no InventarioService is wired.
func TestReemplazarCombos_NilInventario_StillSucceeds(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	// Note: do NOT call h.svc.WithInventario — verifying nil-safe path.
	ventaID := h.seedVenta(t)

	_, err := h.svc.ReemplazarCombos(t.Context(), app.ReemplazarCombosInput{
		VentaID: *ventaID,
		Combos:  []app.CrearVentaComboInput{},
	}, uuid.New())
	require.NoError(t, err)
}

// TestReemplazarProductos_IdenticalEdit — case 9.
// ReemplazarProductos called with the SAME productos the venta already has →
// ResincronizarTraspasoParaVenta is STILL called (no short-circuit in ventas;
// the no-op decision lives in the inventario module). Assert the recording
// fake's resinc call count incremented and the detalles match the current set.
func TestReemplazarProductos_IdenticalEdit(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{}
	h.svc.WithInventario(inv)

	// Seed a venta with a known standalone producto.
	ventaID := h.seedVenta(t)

	// Re-supply the same producto that validContadoInput uses (ArticuloID=42,
	// AlmacenOrigen=1, AlmacenDestino=2) — effectively a no-op edit.
	one, two := 1, 2
	_, err := h.svc.ReemplazarProductos(t.Context(), app.ReemplazarProductosInput{
		VentaID: *ventaID,
		Productos: []app.CrearVentaProductoInput{{
			ID:             uuid.New(),
			ArticuloID:     42,
			Articulo:       "Refrigerador",
			Cantidad:       decimal.NewFromInt(1),
			PrecioAnual:    decimal.NewFromInt(1200),
			PrecioCorto:    decimal.NewFromInt(1100),
			PrecioContado:  decimal.NewFromInt(1000),
			AlmacenOrigen:  &one,
			AlmacenDestino: &two,
		}},
	}, uuid.New())
	require.NoError(t, err)

	// ResincronizarTraspasoParaVenta must be called even for an identical edit.
	assert.Equal(t, int32(1), inv.resincCalls.Load(),
		"ResincronizarTraspasoParaVenta must be called even when productos are identical")
	require.Len(t, inv.resincParams, 1)
	assert.Equal(t, *ventaID, inv.resincParams[0].VentaID)
	assert.Equal(t, 1, inv.resincParams[0].AlmacenOrigen)
	require.Len(t, inv.resincParams[0].Detalles, 1, "detalles must reflect the current producto set")
	assert.Equal(t, 42, inv.resincParams[0].Detalles[0].ArticuloID)
	assert.True(t, inv.resincParams[0].Detalles[0].Cantidad.Equal(decimal.NewFromInt(1)))
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// newSinStockError builds the canonical stock-out validation error so tests
// can assert on its code without importing apperror directly.
func newSinStockError() error {
	// We import the apperror package indirectly through the domain's sentinel,
	// but use errors.New here so the test only checks that *an* error propagates
	// when validarErr is set — the exact code is tested in crear_venta_inventario_test.go.
	return sinStockError{}
}

type sinStockError struct{}

func (e sinStockError) Error() string { return "sin existencia" }

// seedVentaViaRepo creates a venta directly in the repo (bypassing the service
// to avoid triggering inventario on an already-configured fake) and returns
// its ID. Used in tests where s.inventario is already set to an error state
// before the seed.
func (h *testHarness) seedVentaViaRepo(t *testing.T) uuid.UUID {
	t.Helper()
	// Create via a temporary harness with no inventario, sharing the same repo.
	tmp := newHarness(t)
	tmp.ventas = h.ventas
	tmp.outbox = h.outbox
	in := validContadoInput()
	venta, err := tmp.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)
	// Drop the create event so assertions start from a clean slate.
	h.outbox.mu.Lock()
	h.outbox.calls = nil
	h.outbox.mu.Unlock()
	return venta.ID()
}
