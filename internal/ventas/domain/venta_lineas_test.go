//nolint:misspell // ventas vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ─── helpers ────────────────────────────────────────────────────────────────

// lineasMontos returns a fixed per-line MontoSnapshot so the tests can assert
// on recomputed header totals without arithmetic noise.
func lineasMontos(t *testing.T, anual, corto, contado int64) domain.MontoSnapshot {
	t.Helper()
	m, err := domain.NewMontoSnapshot(
		decimal.NewFromInt(anual), decimal.NewFromInt(corto), decimal.NewFromInt(contado),
	)
	require.NoError(t, err)
	return m
}

// comboInput builds one combo line.
func comboInput(t *testing.T, id uuid.UUID, nombre string, cantidad int64) domain.CrearVentaComboInput {
	t.Helper()
	return domain.CrearVentaComboInput{
		ID:             id,
		Nombre:         nombre,
		Precios:        lineasMontos(t, 500, 450, 400),
		Cantidad:       decimal.NewFromInt(cantidad),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
	}
}

// comboChildInput builds a producto that belongs to the combo identified by
// comboID: no almacenes of its own, it inherits the parent combo's.
func comboChildInput(t *testing.T, comboID uuid.UUID, articuloID int, cantidad int64) domain.CrearVentaProductoInput {
	t.Helper()
	cid := comboID
	return domain.CrearVentaProductoInput{
		ID:             uuid.New(),
		ArticuloID:     articuloID,
		Articulo:       "Hijo de combo",
		Cantidad:       decimal.NewFromInt(cantidad),
		Precios:        lineasMontos(t, 100, 90, 80),
		ComboID:        &cid,
		AlmacenOrigen:  nil,
		AlmacenDestino: nil,
	}
}

// standaloneInput builds a producto that belongs to no combo.
func standaloneInput(t *testing.T, articuloID int, cantidad int64) domain.CrearVentaProductoInput {
	t.Helper()
	one, two := 1, 2
	return domain.CrearVentaProductoInput{
		ID:             uuid.New(),
		ArticuloID:     articuloID,
		Articulo:       "Producto suelto",
		Cantidad:       decimal.NewFromInt(cantidad),
		Precios:        lineasMontos(t, 200, 180, 160),
		AlmacenOrigen:  &one,
		AlmacenDestino: &two,
	}
}

// ventaConCombo returns a borrador venta that already has one combo and one
// combo-child producto — the shape the desktop edits.
func ventaConCombo(t *testing.T) (*domain.Venta, uuid.UUID) {
	t.Helper()
	p := validCrearVentaParams(t)
	comboID := uuid.New()
	p.Combos = []domain.CrearVentaComboInput{comboInput(t, comboID, "Combo Original", 1)}
	p.Productos = []domain.CrearVentaProductoInput{comboChildInput(t, comboID, 77, 1)}
	v, err := domain.CrearVenta(p)
	require.NoError(t, err)
	v.ClearPendingEvents()
	return v, comboID
}

// requireCode asserts err carries the given apperror code.
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok, "expected typed apperror, got %T: %v", err, err)
	assert.Equal(t, code, ae.Code)
}

// ─── the case that motivated the endpoint ───────────────────────────────────

// TestVenta_ReemplazarLineas_BorrarComboYCrearOtro is the defect this whole
// endpoint exists for. The capture screen cannot edit the contents of a combo,
// so a user who wants to change one deletes it and creates another — with a
// NEW id — and moves the productos to it. Neither single-collection mutator
// can accept that, in either order. The combined one must.
func TestVenta_ReemplazarLineas_BorrarComboYCrearOtro(t *testing.T) {
	t.Parallel()
	v, viejoComboID := ventaConCombo(t)
	nuevoComboID := uuid.New()
	require.NotEqual(t, viejoComboID, nuevoComboID)

	err := v.ReemplazarLineas(domain.ReemplazarLineasParams{
		Combos:    []domain.CrearVentaComboInput{comboInput(t, nuevoComboID, "Combo Nuevo", 1)},
		Productos: []domain.CrearVentaProductoInput{comboChildInput(t, nuevoComboID, 88, 2)},
		By:        uuid.New(),
		Now:       time.Now().UTC(),
	})
	require.NoError(t, err)

	require.Equal(t, 1, v.CombosCount())
	require.Equal(t, 1, v.ProductosCount())
	for c := range v.Combos() {
		assert.Equal(t, nuevoComboID, c.ID(), "the surviving combo must be the new one")
	}
	for p := range v.Productos() {
		require.NotNil(t, p.ComboID())
		assert.Equal(t, nuevoComboID, *p.ComboID(), "the producto must point at the new combo")
	}
}

// TestVenta_ReemplazarCombos_NoPuedeBorrarYCrear is the control that proves
// the previous test is not vacuous: the same edit through the two existing
// single-collection mutators is rejected in BOTH orders. If either of these
// ever starts passing, the combined endpoint stopped being necessary — and
// something silently changed.
func TestVenta_ReemplazarCombos_NoPuedeBorrarYCrear(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	by := uuid.New()

	t.Run("combos primero deja huérfanos a los productos vigentes", func(t *testing.T) {
		t.Parallel()
		v, _ := ventaConCombo(t)
		nuevoComboID := uuid.New()
		err := v.ReemplazarCombos(domain.ReemplazarCombosParams{
			Combos: []domain.CrearVentaComboInput{comboInput(t, nuevoComboID, "Combo Nuevo", 1)},
			By:     by, Now: now,
		})
		requireCode(t, err, "producto_combo_referencia_invalida")
	})

	t.Run("productos primero apuntan a un combo inexistente", func(t *testing.T) {
		t.Parallel()
		v, _ := ventaConCombo(t)
		nuevoComboID := uuid.New()
		err := v.ReemplazarProductos(domain.ReemplazarProductosParams{
			Productos: []domain.CrearVentaProductoInput{comboChildInput(t, nuevoComboID, 88, 2)},
			By:        by, Now: now,
		})
		requireCode(t, err, "producto_combo_referencia_invalida")
	})
}

// ─── cross-collection validation ────────────────────────────────────────────

// TestVenta_ReemplazarLineas_ProductoHaciaComboAusente pins the decision the
// owner made: the desktop is expected to send coherent collections, but the
// API does not trust it. A producto pointing at a combo absent from the SAME
// request is still rejected.
func TestVenta_ReemplazarLineas_ProductoHaciaComboAusente(t *testing.T) {
	t.Parallel()
	v, _ := ventaConCombo(t)
	presente := uuid.New()
	ausente := uuid.New()

	err := v.ReemplazarLineas(domain.ReemplazarLineasParams{
		Combos: []domain.CrearVentaComboInput{comboInput(t, presente, "Combo Presente", 1)},
		Productos: []domain.CrearVentaProductoInput{
			comboChildInput(t, presente, 10, 1),
			comboChildInput(t, ausente, 11, 1), // points nowhere
		},
		By: uuid.New(), Now: time.Now().UTC(),
	})
	requireCode(t, err, "producto_combo_referencia_invalida")

	// And the aggregate is untouched: the rejection happens before any
	// assignment, so a failed request cannot leave a half-edited venta.
	assert.Equal(t, 1, v.CombosCount())
	assert.Equal(t, 1, v.ProductosCount())
	assert.Empty(t, v.PendingEvents(), "a rejected edit must not emit events")
}

// TestVenta_ReemplazarLineas_ComboSinProductos: a combo with no child
// productos is valid. The venta still needs at least one producto, which the
// standalone line provides.
func TestVenta_ReemplazarLineas_ComboSinProductos(t *testing.T) {
	t.Parallel()
	v, _ := ventaConCombo(t)
	comboID := uuid.New()

	require.NoError(t, v.ReemplazarLineas(domain.ReemplazarLineasParams{
		Combos:    []domain.CrearVentaComboInput{comboInput(t, comboID, "Combo Vacío", 1)},
		Productos: []domain.CrearVentaProductoInput{standaloneInput(t, 55, 1)},
		By:        uuid.New(), Now: time.Now().UTC(),
	}))
	assert.Equal(t, 1, v.CombosCount())
	assert.Equal(t, 1, v.ProductosCount())
	for p := range v.Productos() {
		assert.Nil(t, p.ComboID())
	}
}

// TestVenta_ReemplazarLineas_ProductosSueltosIntactos: productos with no
// comboID are never touched by the combo validation, even when every combo is
// dropped in the same call.
func TestVenta_ReemplazarLineas_ProductosSueltosIntactos(t *testing.T) {
	t.Parallel()
	v, _ := ventaConCombo(t)

	require.NoError(t, v.ReemplazarLineas(domain.ReemplazarLineasParams{
		Combos: nil, // every combo dropped
		Productos: []domain.CrearVentaProductoInput{
			standaloneInput(t, 41, 1),
			standaloneInput(t, 42, 2),
		},
		By: uuid.New(), Now: time.Now().UTC(),
	}))
	assert.Equal(t, 0, v.CombosCount())
	assert.Equal(t, 2, v.ProductosCount())
}

// TestVenta_ReemplazarLineas_ComboMixtoConSueltos covers the realistic mix:
// one combo with two children plus a stand-alone producto.
func TestVenta_ReemplazarLineas_ComboMixtoConSueltos(t *testing.T) {
	t.Parallel()
	v, _ := ventaConCombo(t)
	comboID := uuid.New()

	require.NoError(t, v.ReemplazarLineas(domain.ReemplazarLineasParams{
		Combos: []domain.CrearVentaComboInput{comboInput(t, comboID, "Combo Recámara", 1)},
		Productos: []domain.CrearVentaProductoInput{
			comboChildInput(t, comboID, 1, 1),
			comboChildInput(t, comboID, 2, 1),
			standaloneInput(t, 3, 1),
		},
		By: uuid.New(), Now: time.Now().UTC(),
	}))
	assert.Equal(t, 1, v.CombosCount())
	assert.Equal(t, 3, v.ProductosCount())

	// Montos: Σ(stand-alone productos) + Σ(combos). Combo children are NOT
	// counted — their value lives in the parent combo's price.
	// stand-alone 200×1 + combo 500×1 = 700 anual.
	assert.True(t, v.Montos().Anual().Equal(decimal.NewFromInt(700)),
		"montos must be recomputed from the new lines; got %s", v.Montos().Anual().String())
	assert.True(t, v.Montos().CortoPlazo().Equal(decimal.NewFromInt(630)),
		"got %s", v.Montos().CortoPlazo().String())
	assert.True(t, v.Montos().Contado().Equal(decimal.NewFromInt(560)),
		"got %s", v.Montos().Contado().String())
}

// ─── guardrails ─────────────────────────────────────────────────────────────

// TestVenta_ReemplazarLineas_ProductosVaciosRechazados: a venta always keeps
// at least one producto.
func TestVenta_ReemplazarLineas_ProductosVaciosRechazados(t *testing.T) {
	t.Parallel()
	v, _ := ventaConCombo(t)
	err := v.ReemplazarLineas(domain.ReemplazarLineasParams{
		Combos: nil, Productos: nil, By: uuid.New(), Now: time.Now().UTC(),
	})
	requireCode(t, err, "venta_productos_vacios")
}

// TestVenta_ReemplazarLineas_NoBorradorRechazado walks every non-borrador
// situación and asserts the edit is refused with venta_no_editable.
func TestVenta_ReemplazarLineas_NoBorradorRechazado(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Now().UTC()

	cases := map[string]func(t *testing.T, v *domain.Venta){
		"cancelada": func(t *testing.T, v *domain.Venta) {
			t.Helper()
			require.NoError(t, v.Cancelar("cliente se arrepintió", by, now))
		},
		"revisada": func(t *testing.T, v *domain.Venta) {
			t.Helper()
			require.NoError(t, v.EnviarARevision(by, now))
		},
		"aprobada": func(t *testing.T, v *domain.Venta) {
			t.Helper()
			require.NoError(t, v.EnviarARevision(by, now))
			require.NoError(t, v.Aprobar(by, now))
		},
	}
	for name, transition := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v, _ := ventaConCombo(t)
			transition(t, v)
			err := v.ReemplazarLineas(domain.ReemplazarLineasParams{
				Combos:    []domain.CrearVentaComboInput{comboInput(t, uuid.New(), "X", 1)},
				Productos: []domain.CrearVentaProductoInput{standaloneInput(t, 9, 1)},
				By:        by, Now: now,
			})
			requireCode(t, err, "venta_no_editable")
		})
	}
}

// TestVenta_ReemplazarLineas_EmiteAmbosEventos pins the outbox contract: the
// combined mutator emits the same two events the two single-collection
// mutators emit between them, so the search reindex and the event timeline
// keep working without registering a new event type.
func TestVenta_ReemplazarLineas_EmiteAmbosEventos(t *testing.T) {
	t.Parallel()
	v, _ := ventaConCombo(t)
	comboID := uuid.New()

	require.NoError(t, v.ReemplazarLineas(domain.ReemplazarLineasParams{
		Combos: []domain.CrearVentaComboInput{comboInput(t, comboID, "Combo", 1)},
		Productos: []domain.CrearVentaProductoInput{
			comboChildInput(t, comboID, 1, 1),
			standaloneInput(t, 2, 1),
		},
		By: uuid.New(), Now: time.Now().UTC(),
	}))

	evs := v.PendingEvents()
	require.Len(t, evs, 2)
	assert.Equal(t, "venta.combos_reemplazados", evs[0].EventType())
	assert.Equal(t, "venta.productos_reemplazados", evs[1].EventType())
	assert.Equal(t, 1, evs[0].Payload()["combos_count"])
	assert.Equal(t, 2, evs[1].Payload()["productos_count"])
}

// TestVenta_ReemplazarLineas_LineaInvalidaNoMuta: a malformed line (negative
// price) is rejected by the per-line constructors before anything is assigned.
func TestVenta_ReemplazarLineas_LineaInvalidaNoMuta(t *testing.T) {
	t.Parallel()
	v, comboOriginal := ventaConCombo(t)

	mala := standaloneInput(t, 7, 1)
	mala.Cantidad = decimal.NewFromInt(0) // cantidad must be > 0

	err := v.ReemplazarLineas(domain.ReemplazarLineasParams{
		Combos:    []domain.CrearVentaComboInput{comboInput(t, uuid.New(), "Otro", 1)},
		Productos: []domain.CrearVentaProductoInput{mala},
		By:        uuid.New(), Now: time.Now().UTC(),
	})
	require.Error(t, err)

	assert.Equal(t, 1, v.CombosCount())
	for c := range v.Combos() {
		assert.Equal(t, comboOriginal, c.ID(), "the original combo must survive a rejected edit")
	}
	assert.Empty(t, v.PendingEvents())
}
