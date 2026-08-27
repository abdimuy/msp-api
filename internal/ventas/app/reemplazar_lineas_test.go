//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/app"
)

// ─── input builders ─────────────────────────────────────────────────────────

// lineasComboInput builds one combo line for the app-level request.
func lineasComboInput(id uuid.UUID, nombre string) app.CrearVentaComboInput {
	return app.CrearVentaComboInput{
		ID:             id,
		Nombre:         nombre,
		PrecioAnual:    decimal.NewFromInt(3000),
		PrecioCorto:    decimal.NewFromInt(2700),
		PrecioContado:  decimal.NewFromInt(2400),
		Cantidad:       decimal.NewFromInt(1),
		AlmacenOrigen:  7,
		AlmacenDestino: 8,
	}
}

// lineasComboChildInput builds a producto that belongs to comboID and thus
// carries no almacenes of its own.
func lineasComboChildInput(comboID uuid.UUID, articuloID int, cantidad int64) app.CrearVentaProductoInput {
	cid := comboID
	return app.CrearVentaProductoInput{
		ID:             uuid.New(),
		ArticuloID:     articuloID,
		Articulo:       "Hijo de combo",
		Cantidad:       decimal.NewFromInt(cantidad),
		PrecioAnual:    decimal.NewFromInt(1000),
		PrecioCorto:    decimal.NewFromInt(900),
		PrecioContado:  decimal.NewFromInt(800),
		ComboID:        &cid,
		AlmacenOrigen:  nil,
		AlmacenDestino: nil,
	}
}

// seedVentaConCombo creates a venta that already carries a combo plus its
// child producto — the shape the desktop edits when the defect fires — and
// returns the venta id and the id of the ORIGINAL combo.
func seedVentaConCombo(t *testing.T, h *testHarness) (uuid.UUID, uuid.UUID) {
	t.Helper()
	in := validComboInput()
	venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)
	h.outbox.mu.Lock()
	h.outbox.calls = nil
	h.outbox.mu.Unlock()
	return venta.ID(), in.Combos[0].ID
}

// ─── the case that motivated the endpoint ───────────────────────────────────

// TestReemplazarLineas_BorrarComboYCrearOtro is the production defect, at the
// app layer: eight failed attempts by the same user on the same venta on the
// same day, all 'el combo referenciado por el producto no existe en la venta'.
// The user deleted a combo and created another (new id) with its productos
// moved across; no ordering of the two single-collection endpoints can accept
// that. This one must.
func TestReemplazarLineas_BorrarComboYCrearOtro(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{}
	h.svc.WithInventario(inv)

	ventaID, viejoComboID := seedVentaConCombo(t, h)
	nuevoComboID := uuid.New()
	require.NotEqual(t, viejoComboID, nuevoComboID)

	out, err := h.svc.ReemplazarLineas(t.Context(), app.ReemplazarLineasInput{
		VentaID:   ventaID,
		Combos:    []app.CrearVentaComboInput{lineasComboInput(nuevoComboID, "Combo Nuevo")},
		Productos: []app.CrearVentaProductoInput{lineasComboChildInput(nuevoComboID, 88, 2)},
	}, uuid.New())
	require.NoError(t, err, "deleting a combo and creating another in one save must succeed")

	require.Equal(t, 1, out.CombosCount())
	require.Equal(t, 1, out.ProductosCount())
	for c := range out.Combos() {
		assert.Equal(t, nuevoComboID, c.ID())
	}
	for p := range out.Productos() {
		require.NotNil(t, p.ComboID())
		assert.Equal(t, nuevoComboID, *p.ComboID())
	}
}

// TestReemplazarCombos_BorrarYCrear_SigueFallando is the control that keeps
// the previous test honest: the SAME edit through the pre-existing endpoints
// still fails in both orders. The old endpoints were not fixed — they were
// left exactly as production runs them.
func TestReemplazarCombos_BorrarYCrear_SigueFallando(t *testing.T) {
	t.Parallel()

	t.Run("combos primero", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID, _ := seedVentaConCombo(t, h)
		_, err := h.svc.ReemplazarCombos(t.Context(), app.ReemplazarCombosInput{
			VentaID: ventaID,
			Combos:  []app.CrearVentaComboInput{lineasComboInput(uuid.New(), "Combo Nuevo")},
		}, uuid.New())
		requireAppCode(t, err, "producto_combo_referencia_invalida")
	})

	t.Run("productos primero", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID, _ := seedVentaConCombo(t, h)
		_, err := h.svc.ReemplazarProductos(t.Context(), app.ReemplazarProductosInput{
			VentaID:   ventaID,
			Productos: []app.CrearVentaProductoInput{lineasComboChildInput(uuid.New(), 88, 2)},
		}, uuid.New())
		requireAppCode(t, err, "producto_combo_referencia_invalida")
	})
}

// ─── inventory ──────────────────────────────────────────────────────────────

// TestReemplazarLineas_ResincronizaUnaSolaVez is the inventory guarantee. The
// old two-request edit ran ResincronizarTraspasoParaVenta TWICE — once against
// a venta whose combos and productos disagreed. The combined command runs it
// exactly once, against the final line-item set, so stock can neither be
// reserved twice nor reserved against an intermediate state.
func TestReemplazarLineas_ResincronizaUnaSolaVez(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{}
	h.svc.WithInventario(inv)

	ventaID, _ := seedVentaConCombo(t, h)
	nuevoComboID := uuid.New()

	// Seeding the venta already spent one ValidarStockParaVenta and one
	// CrearTraspasoParaVenta (that is what CrearVenta does). Zero the counters
	// so what follows measures the EDIT alone.
	inv.validarCalls.Store(0)
	inv.crearCalls.Store(0)

	_, err := h.svc.ReemplazarLineas(t.Context(), app.ReemplazarLineasInput{
		VentaID:   ventaID,
		Combos:    []app.CrearVentaComboInput{lineasComboInput(nuevoComboID, "Combo Nuevo")},
		Productos: []app.CrearVentaProductoInput{lineasComboChildInput(nuevoComboID, 88, 3)},
	}, uuid.New())
	require.NoError(t, err)

	require.Equal(t, int32(1), inv.resincCalls.Load(),
		"the combined edit must resync inventory exactly once, not once per collection")
	require.Len(t, inv.resincParams, 1)

	// The single resync must describe the FINAL state: the child articulo, at
	// the almacén of the NEW combo (7, from lineasComboInput), quantity 3.
	p := inv.resincParams[0]
	assert.Equal(t, ventaID, p.VentaID)
	assert.Equal(t, 7, p.AlmacenOrigen, "origin comes from the new combo, not the deleted one")
	require.Len(t, p.Detalles, 1)
	assert.Equal(t, 88, p.Detalles[0].ArticuloID)
	assert.True(t, p.Detalles[0].Cantidad.Equal(decimal.NewFromInt(3)),
		"got %s", p.Detalles[0].Cantidad.String())

	// And nothing else in the inventory surface was touched: no new directo
	// (CrearTraspasoParaVenta) and no reverso of its own — the resync owns the
	// reverse-then-recreate pair internally.
	assert.Equal(t, int32(0), inv.crearCalls.Load(), "must not create a second traspaso")
	assert.Equal(t, int32(0), inv.reversoCalls.Load(), "must not reverse the traspaso itself")
	assert.Equal(t, int32(0), inv.validarCalls.Load(),
		"stock validation on the edit path lives inside the inventario resync, not here")
}

// TestReemplazarLineas_DosLlamadasSeparadasResincronizanDosVeces is the
// measurement that justifies the previous assertion: doing the same work with
// the two old endpoints costs two resyncs.
func TestReemplazarLineas_DosLlamadasSeparadasResincronizanDosVeces(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{}
	h.svc.WithInventario(inv)
	ventaID := h.seedVenta(t)

	_, err := h.svc.ReemplazarCombos(t.Context(), app.ReemplazarCombosInput{
		VentaID: *ventaID, Combos: []app.CrearVentaComboInput{},
	}, uuid.New())
	require.NoError(t, err)

	one, two := 1, 2
	_, err = h.svc.ReemplazarProductos(t.Context(), app.ReemplazarProductosInput{
		VentaID: *ventaID,
		Productos: []app.CrearVentaProductoInput{{
			ID: uuid.New(), ArticuloID: 42, Articulo: "Refrigerador",
			Cantidad:      decimal.NewFromInt(1),
			PrecioAnual:   decimal.NewFromInt(1200),
			PrecioCorto:   decimal.NewFromInt(1100),
			PrecioContado: decimal.NewFromInt(1000),
			AlmacenOrigen: &one, AlmacenDestino: &two,
		}},
	}, uuid.New())
	require.NoError(t, err)

	assert.Equal(t, int32(2), inv.resincCalls.Load(),
		"the two-endpoint edit resyncs twice — this is what the combined endpoint replaces")
}

// TestReemplazarLineas_NilInventario_SigueFuncionando: the inventario port is
// optional (tests and pre-inventario deployments leave it nil).
func TestReemplazarLineas_NilInventario_SigueFuncionando(t *testing.T) {
	t.Parallel()
	h := newHarness(t) // deliberately NOT calling WithInventario
	ventaID, _ := seedVentaConCombo(t, h)
	comboID := uuid.New()

	_, err := h.svc.ReemplazarLineas(t.Context(), app.ReemplazarLineasInput{
		VentaID:   ventaID,
		Combos:    []app.CrearVentaComboInput{lineasComboInput(comboID, "Combo")},
		Productos: []app.CrearVentaProductoInput{lineasComboChildInput(comboID, 1, 1)},
	}, uuid.New())
	require.NoError(t, err)
}

// ─── failure paths: nothing partial escapes ─────────────────────────────────

// TestReemplazarLineas_RepoFalla_NiInventarioNiEventos: when the combined
// write fails, the transaction body returns before the inventory resync, and
// the outbox is never drained. Nothing observable escaped the failed save.
func TestReemplazarLineas_RepoFalla_NiInventarioNiEventos(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{}
	h.svc.WithInventario(inv)
	ventaID, _ := seedVentaConCombo(t, h)

	boom := errors.New("firebird: write failed")
	h.ventas.mu.Lock()
	h.ventas.ReplaceLineasErr = boom
	h.ventas.mu.Unlock()

	comboID := uuid.New()
	_, err := h.svc.ReemplazarLineas(t.Context(), app.ReemplazarLineasInput{
		VentaID:   ventaID,
		Combos:    []app.CrearVentaComboInput{lineasComboInput(comboID, "Combo")},
		Productos: []app.CrearVentaProductoInput{lineasComboChildInput(comboID, 1, 1)},
	}, uuid.New())
	require.ErrorIs(t, err, boom)

	assert.Equal(t, int32(0), inv.resincCalls.Load(),
		"inventory must not move when the line write failed")
	assert.Empty(t, h.outbox.eventTypes(),
		"no outbox event may be emitted for a save that did not happen")
}

// TestReemplazarLineas_ResyncFalla_SinEventos: when the inventory resync fails
// (e.g. the post-reversal stock check rejects), the error propagates and the
// outbox stays empty — the caller must not see a 'lines replaced' event for a
// transaction that rolled back.
func TestReemplazarLineas_ResyncFalla_SinEventos(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{resincErr: newSinStockError()}
	h.svc.WithInventario(inv)
	ventaID, _ := seedVentaConCombo(t, h)

	comboID := uuid.New()
	_, err := h.svc.ReemplazarLineas(t.Context(), app.ReemplazarLineasInput{
		VentaID:   ventaID,
		Combos:    []app.CrearVentaComboInput{lineasComboInput(comboID, "Combo")},
		Productos: []app.CrearVentaProductoInput{lineasComboChildInput(comboID, 1, 1)},
	}, uuid.New())
	require.Error(t, err)

	assert.Equal(t, int32(1), inv.resincCalls.Load())
	assert.Equal(t, 1, h.ventas.ReplaceLineasCalls,
		"the write ran inside the tx; the tx is what rolls it back")
	assert.Empty(t, h.outbox.eventTypes(),
		"no outbox event may be emitted when the transaction failed")
}

// TestReemplazarLineas_ReferenciaInvalida_NoTocaNada: a producto pointing at a
// combo absent from the same body is rejected BEFORE the transaction opens.
func TestReemplazarLineas_ReferenciaInvalida_NoTocaNada(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	inv := &fakeInventarioService{}
	h.svc.WithInventario(inv)
	ventaID, _ := seedVentaConCombo(t, h)

	presente := uuid.New()
	_, err := h.svc.ReemplazarLineas(t.Context(), app.ReemplazarLineasInput{
		VentaID: ventaID,
		Combos:  []app.CrearVentaComboInput{lineasComboInput(presente, "Combo Presente")},
		Productos: []app.CrearVentaProductoInput{
			lineasComboChildInput(presente, 1, 1),
			lineasComboChildInput(uuid.New(), 2, 1), // points nowhere
		},
	}, uuid.New())
	requireAppCode(t, err, "producto_combo_referencia_invalida")

	assert.Equal(t, 0, h.ventas.ReplaceLineasCalls, "the write must never be attempted")
	assert.Equal(t, int32(0), inv.resincCalls.Load())
	assert.Empty(t, h.outbox.eventTypes())
}

// TestReemplazarLineas_VentaNoEncontrada surfaces the not-found sentinel.
func TestReemplazarLineas_VentaNoEncontrada(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	comboID := uuid.New()
	_, err := h.svc.ReemplazarLineas(t.Context(), app.ReemplazarLineasInput{
		VentaID:   uuid.New(),
		Combos:    []app.CrearVentaComboInput{lineasComboInput(comboID, "Combo")},
		Productos: []app.CrearVentaProductoInput{lineasComboChildInput(comboID, 1, 1)},
	}, uuid.New())
	requireAppCode(t, err, "venta_not_found")
}

// TestReemplazarLineas_EmiteAmbosEventos pins the outbox surface: the two
// pre-existing event types, in order, so the Meilisearch reindex handler
// (registered per event type) keeps claiming these rows without a new
// registration.
func TestReemplazarLineas_EmiteAmbosEventos(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID, _ := seedVentaConCombo(t, h)
	comboID := uuid.New()

	_, err := h.svc.ReemplazarLineas(t.Context(), app.ReemplazarLineasInput{
		VentaID:   ventaID,
		Combos:    []app.CrearVentaComboInput{lineasComboInput(comboID, "Combo")},
		Productos: []app.CrearVentaProductoInput{lineasComboChildInput(comboID, 1, 1)},
	}, uuid.New())
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"venta.combos_reemplazados", "venta.productos_reemplazados"},
		h.outbox.eventTypes())
}

// ─── helpers ────────────────────────────────────────────────────────────────

// requireAppCode asserts err carries the given apperror code.
func requireAppCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok, "expected typed apperror, got %T: %v", err, err)
	assert.Equal(t, code, ae.Code)
}
