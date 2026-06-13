//nolint:misspell // domain vocabulary is Spanish (ventas, productos, combos, almacenes) per project convention.
package app

// White-box tests for buildTraspasoDetallesFromVenta and resincronizarTraspaso.
// These live in package app (not app_test) so they can call unexported functions
// and wire an unexported resincronizarTraspaso directly.

import (
	"context"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── White-box fake ──────────────────────────────────────────────────────────

// wbFakeInventario is a minimal InventarioService recording fake for white-box
// tests. It lives here (package app) so resincronizarTraspaso can be exercised
// via a *Service constructed inside this package.
type wbFakeInventario struct {
	resincCalls  atomic.Int32
	resincParams []outbound.InventarioCrearTraspasoParams
	resincErr    error
}

func (f *wbFakeInventario) ValidarStockParaVenta(_ context.Context, _ []outbound.InventarioStockItem) error {
	return nil
}

func (f *wbFakeInventario) CrearTraspasoParaVenta(_ context.Context, _ outbound.InventarioCrearTraspasoParams) (int, error) {
	return 0, nil
}

func (f *wbFakeInventario) CrearTraspasoReverso(_ context.Context, _, _ uuid.UUID) (int, error) {
	return 0, nil
}

func (f *wbFakeInventario) ResincronizarTraspasoParaVenta(_ context.Context, p outbound.InventarioCrearTraspasoParams) (int, error) {
	f.resincCalls.Add(1)
	f.resincParams = append(f.resincParams, p)
	return 0, f.resincErr
}

// ─── Fixture helpers ─────────────────────────────────────────────────────────

// zeroMontos returns a zero-valued MontoSnapshot suitable for test fixtures.
func zeroMontos() domain.MontoSnapshot {
	return domain.HydrateMontoSnapshot(decimal.Zero, decimal.Zero, decimal.Zero)
}

// hydrateStandaloneProducto builds a *domain.Producto without combo affiliation.
func hydrateStandaloneProducto(articuloID int, cantidad decimal.Decimal, almacenOrigen, almacenDestino int) *domain.Producto {
	orig := almacenOrigen
	dest := almacenDestino
	return domain.HydrateProducto(domain.HydrateProductoParams{
		ID:             uuid.New(),
		ArticuloID:     articuloID,
		Articulo:       "Articulo de prueba",
		Cantidad:       cantidad,
		Precios:        zeroMontos(),
		ComboID:        nil,
		AlmacenOrigen:  &orig,
		AlmacenDestino: &dest,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
}

// hydrateComboProducto builds a *domain.Producto that is a child of a combo.
func hydrateComboProducto(articuloID int, cantidad decimal.Decimal, comboID uuid.UUID) *domain.Producto {
	return domain.HydrateProducto(domain.HydrateProductoParams{
		ID:             uuid.New(),
		ArticuloID:     articuloID,
		Articulo:       "Articulo de combo",
		Cantidad:       cantidad,
		Precios:        zeroMontos(),
		ComboID:        &comboID,
		AlmacenOrigen:  nil,
		AlmacenDestino: nil,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
}

// hydrateCombo builds a *domain.Combo via HydrateCombo (no validation).
func hydrateCombo(almacenOrigen, almacenDestino int) *domain.Combo {
	return domain.HydrateCombo(domain.HydrateComboParams{
		ID:             uuid.New(),
		Nombre:         "Combo Test",
		Precios:        zeroMontos(),
		Cantidad:       decimal.NewFromInt(1),
		AlmacenOrigen:  almacenOrigen,
		AlmacenDestino: almacenDestino,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
}

// buildVentaFixture builds a *domain.Venta using HydrateVenta with the
// supplied combos and productos. Synchronization defaults to pendiente.
func buildVentaFixture(combos []*domain.Combo, productos []*domain.Producto) *domain.Venta {
	return domain.HydrateVenta(domain.HydrateVentaParams{
		ID:             uuid.New(),
		Cliente:        domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: domain.HydrateNombreCliente("CLIENTE TEST")}),
		Direccion:      domain.HydrateDireccion(domain.NewDireccionParams{Calle: "AV TEST", Colonia: "COL", Poblacion: "POB", Ciudad: "CIUDAD"}),
		GPS:            domain.HydrateGPSCoords(25.5, -103.4),
		FechaVenta:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:      domain.TipoVentaContado,
		Montos:         zeroMontos(),
		Estado:         domain.EstadoActive,
		Situacion:      domain.SituacionBorrador,
		Sincronizacion: domain.SincronizacionPendiente,
		Combos:         combos,
		Productos:      productos,
		Vendedores:     nil,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
}

// buildAplicadaVentaFixture builds a venta with SincronizacionAplicada.
func buildAplicadaVentaFixture(combos []*domain.Combo, productos []*domain.Producto) *domain.Venta {
	v := buildVentaFixture(combos, productos)
	// Replace with an applied venta by re-hydrating with aplicada sincronizacion.
	return domain.HydrateVenta(domain.HydrateVentaParams{
		ID:             v.ID(),
		Cliente:        domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: domain.HydrateNombreCliente("CLIENTE TEST")}),
		Direccion:      domain.HydrateDireccion(domain.NewDireccionParams{Calle: "AV TEST", Colonia: "COL", Poblacion: "POB", Ciudad: "CIUDAD"}),
		GPS:            domain.HydrateGPSCoords(25.5, -103.4),
		FechaVenta:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:      domain.TipoVentaContado,
		Montos:         zeroMontos(),
		Estado:         domain.EstadoActive,
		Situacion:      domain.SituacionAprobada,
		Sincronizacion: domain.SincronizacionAplicada,
		Combos:         combos,
		Productos:      productos,
		Vendedores:     nil,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
}

// ─── Section A: White-box pure tests for buildTraspasoDetallesFromVenta ─────

// TestBuildTraspasoDetalles_StandaloneOnly — case 1.
// Standalone productos → detalles match ArticuloID/Cantidad; almacenOrigen is
// the shared origin.
func TestBuildTraspasoDetalles_StandaloneOnly(t *testing.T) {
	t.Parallel()
	p1 := hydrateStandaloneProducto(10, decimal.NewFromInt(2), 5, 6)
	p2 := hydrateStandaloneProducto(20, decimal.NewFromInt(3), 5, 7)
	venta := buildVentaFixture(nil, []*domain.Producto{p1, p2})

	detalles, almacen, err := buildTraspasoDetallesFromVenta(venta)

	require.NoError(t, err)
	assert.Equal(t, 5, almacen, "almacenOrigen must be the shared origin of standalone productos")
	require.Len(t, detalles, 2)

	// Build expected set keyed by ArticuloID.
	byArticulo := make(map[int]outbound.InventarioTraspasoDetalle, 2)
	for _, d := range detalles {
		byArticulo[d.ArticuloID] = d
	}
	d1, ok := byArticulo[10]
	require.True(t, ok, "detalle for ArticuloID=10 must be present")
	assert.True(t, d1.Cantidad.Equal(decimal.NewFromInt(2)), "cantidad for ArticuloID=10 must be 2")

	d2, ok := byArticulo[20]
	require.True(t, ok, "detalle for ArticuloID=20 must be present")
	assert.True(t, d2.Cantidad.Equal(decimal.NewFromInt(3)), "cantidad for ArticuloID=20 must be 3")
}

// TestBuildTraspasoDetalles_ComboChildrenOnly — case 2.
// Combo-child productos inherit almacenOrigen from the parent combo.
// The child's OWN Cantidad is used (NOT multiplied by combo Cantidad).
// Combo Cantidad=3, child Cantidad=2 → detalle Cantidad must be 2.
func TestBuildTraspasoDetalles_ComboChildrenOnly(t *testing.T) {
	t.Parallel()
	combo := domain.HydrateCombo(domain.HydrateComboParams{
		ID:             uuid.New(),
		Nombre:         "Combo Sala",
		Precios:        zeroMontos(),
		Cantidad:       decimal.NewFromInt(3), // 3 bundles — must NOT affect child cantidad
		AlmacenOrigen:  9,
		AlmacenDestino: 10,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
	child := hydrateComboProducto(77, decimal.NewFromInt(2), combo.ID())
	venta := buildVentaFixture([]*domain.Combo{combo}, []*domain.Producto{child})

	detalles, almacen, err := buildTraspasoDetallesFromVenta(venta)

	require.NoError(t, err)
	assert.Equal(t, 9, almacen, "almacenOrigen must be inherited from the parent combo")
	require.Len(t, detalles, 1)
	assert.Equal(t, 77, detalles[0].ArticuloID)
	// Prove no multiplication: child Cantidad=2, combo Cantidad=3 → must be 2.
	assert.True(t, detalles[0].Cantidad.Equal(decimal.NewFromInt(2)),
		"detalle cantidad must be the child's OWN Cantidad (2), not combo×child (6)")
}

// TestBuildTraspasoDetalles_MixedStandaloneAndCombo — case 3.
// Mixed standalone + combo-children sharing the same origin → all detalles present.
func TestBuildTraspasoDetalles_MixedStandaloneAndCombo(t *testing.T) {
	t.Parallel()
	const sharedOrigin = 5

	combo := domain.HydrateCombo(domain.HydrateComboParams{
		ID:             uuid.New(),
		Nombre:         "Combo Recamara",
		Precios:        zeroMontos(),
		Cantidad:       decimal.NewFromInt(1),
		AlmacenOrigen:  sharedOrigin,
		AlmacenDestino: 6,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
	standalone := hydrateStandaloneProducto(100, decimal.NewFromInt(1), sharedOrigin, 6)
	comboChild := hydrateComboProducto(200, decimal.NewFromInt(2), combo.ID())
	venta := buildVentaFixture([]*domain.Combo{combo}, []*domain.Producto{standalone, comboChild})

	detalles, almacen, err := buildTraspasoDetallesFromVenta(venta)

	require.NoError(t, err)
	assert.Equal(t, sharedOrigin, almacen)
	require.Len(t, detalles, 2, "both standalone and combo-child must appear in detalles")

	articuloIDs := make(map[int]bool, 2)
	for _, d := range detalles {
		articuloIDs[d.ArticuloID] = true
	}
	assert.True(t, articuloIDs[100], "standalone detalle must be present")
	assert.True(t, articuloIDs[200], "combo-child detalle must be present")
}

// TestBuildTraspasoDetalles_MismatchedOrigenes — case 4.
// Products from different almacen origins → error with code
// "productos_multiples_almacenes_origen".
func TestBuildTraspasoDetalles_MismatchedOrigenes(t *testing.T) {
	t.Parallel()
	// Two standalones with distinct origins trigger the mismatch.
	p1 := hydrateStandaloneProducto(10, decimal.NewFromInt(1), 1, 2)
	p2 := hydrateStandaloneProducto(20, decimal.NewFromInt(1), 3, 4) // different origin
	venta := buildVentaFixture(nil, []*domain.Producto{p1, p2})

	_, _, err := buildTraspasoDetallesFromVenta(venta)

	require.Error(t, err, "mismatched origins must produce an error")
	var apperr *apperror.Error
	if assert.ErrorAs(t, err, &apperr) {
		assert.Equal(t, "productos_multiples_almacenes_origen", apperr.Code)
	}
}

// TestBuildTraspasoDetalles_OrphanedComboReference — case 5.
// A combo-child producto references a comboID that is NOT in Combos() →
// returns a non-nil error. Built via HydrateVenta so validation is bypassed.
func TestBuildTraspasoDetalles_OrphanedComboReference(t *testing.T) {
	t.Parallel()
	ghostComboID := uuid.New()
	// The venta has zero combos but a producto referencing a ghost combo ID.
	orphanProducto := hydrateComboProducto(42, decimal.NewFromInt(1), ghostComboID)
	venta := buildVentaFixture(nil, []*domain.Producto{orphanProducto})

	_, _, err := buildTraspasoDetallesFromVenta(venta)

	require.Error(t, err, "orphaned combo reference must return a non-nil error")
}

// TestBuildTraspasoDetalles_Empty — case 6.
// HydrateVenta with zero productos → returns (nil, 0, nil).
// HydrateVenta bypasses CrearVenta's len(Productos)==0 guard, so we CAN
// construct a zero-producto venta for this test.
func TestBuildTraspasoDetalles_Empty(t *testing.T) {
	t.Parallel()
	venta := buildVentaFixture(nil, nil)

	detalles, almacen, err := buildTraspasoDetallesFromVenta(venta)

	require.NoError(t, err, "empty venta must not error")
	assert.Nil(t, detalles, "empty venta must return nil detalles")
	assert.Equal(t, 0, almacen, "empty venta must return zero almacenOrigen")
}

// ─── Section B: resincronizarTraspaso guard tests ────────────────────────────

// newWhiteBoxService builds a minimal *Service suitable for calling
// resincronizarTraspaso in white-box tests. It does not wire a real repo or
// outbox because the method only needs s.inventario.
func newWhiteBoxService(inv outbound.InventarioService) *Service {
	return &Service{inventario: inv}
}

// TestResincronizarTraspaso_SkipsWhenAplicada — case 7.
// venta.IsAplicada()==true → resincronizarTraspaso returns nil and does NOT
// call the fake's ResincronizarTraspasoParaVenta.
func TestResincronizarTraspaso_SkipsWhenAplicada(t *testing.T) {
	t.Parallel()
	inv := &wbFakeInventario{}
	svc := newWhiteBoxService(inv)

	// An aplicada venta: IsAplicada() == true.
	p1 := hydrateStandaloneProducto(99, decimal.NewFromInt(1), 5, 6)
	venta := buildAplicadaVentaFixture(nil, []*domain.Producto{p1})
	require.True(t, venta.IsAplicada(), "fixture must be aplicada")

	err := svc.resincronizarTraspaso(t.Context(), venta, uuid.New(), time.Now())

	require.NoError(t, err, "resincronizarTraspaso must return nil for an aplicada venta")
	assert.Equal(t, int32(0), inv.resincCalls.Load(),
		"ResincronizarTraspasoParaVenta must NOT be called when venta is aplicada")
}

// TestResincronizarTraspaso_SkipsWhenInventarioNil — case 8.
// s.inventario==nil → resincronizarTraspaso returns nil with no panic.
func TestResincronizarTraspaso_SkipsWhenInventarioNil(t *testing.T) {
	t.Parallel()
	svc := newWhiteBoxService(nil) // nil inventario

	p1 := hydrateStandaloneProducto(88, decimal.NewFromInt(2), 1, 2)
	venta := buildVentaFixture(nil, []*domain.Producto{p1})
	require.False(t, venta.IsAplicada())

	err := svc.resincronizarTraspaso(t.Context(), venta, uuid.New(), time.Now())

	require.NoError(t, err, "nil inventario must not panic and must return nil")
}

// ─── Section C: Identical-edit test (case 9) ─────────────────────────────────
// This test must go through the full service, which lives in package app_test.
// It is placed in inventario_resync_test.go (package app_test) below.
// NOTE: see TestReemplazarProductos_IdenticalEdit in inventario_resync_test.go.

// ─── Section D: Property test ─────────────────────────────────────────────────

// wbTestHarness is a minimal harness for white-box property tests.
// It wires a *Service with the recording wbFakeInventario so that
// resincronizarTraspaso calls are captured.
type wbTestHarness struct {
	svc *Service
	inv *wbFakeInventario
}

func newWBHarness(inv *wbFakeInventario) *wbTestHarness {
	return &wbTestHarness{
		svc: newWhiteBoxService(inv),
		inv: inv,
	}
}

// sortDetalles returns a copy of the detalles slice sorted by ArticuloID so
// comparison is order-independent.
func sortDetalles(in []outbound.InventarioTraspasoDetalle) []outbound.InventarioTraspasoDetalle {
	out := make([]outbound.InventarioTraspasoDetalle, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ArticuloID != out[j].ArticuloID {
			return out[i].ArticuloID < out[j].ArticuloID
		}
		return out[i].Cantidad.LessThan(out[j].Cantidad)
	})
	return out
}

// TestProperty_ResincDetallesMatchBuilder — case 10.
// After resincronizarTraspaso is called, the detalles recorded by the fake
// must equal what buildTraspasoDetallesFromVenta returns for the same venta.
// Exercises random venta configurations: standalone only, combo-only, mixed
// (all items share the same almacenOrigen to avoid the mismatch error path).
func TestProperty_ResincDetallesMatchBuilder(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		inv := &wbFakeInventario{}
		h := newWBHarness(inv)

		// Pick a shared almacen origin/destination pair.
		sharedOrigin := rapid.IntRange(1, 50).Draw(rt, "shared_origin")
		sharedDest := sharedOrigin + rapid.IntRange(1, 50).Draw(rt, "dest_delta")

		// Decide the fixture shape: 0=standalone only, 1=combo-child only, 2=mixed.
		shape := rapid.IntRange(0, 2).Draw(rt, "shape")

		var combos []*domain.Combo
		var productos []*domain.Producto

		switch shape {
		case 0: // standalone only
			n := rapid.IntRange(1, 5).Draw(rt, "n_standalone")
			for i := range n {
				artID := rapid.IntRange(1, 999).Draw(rt, "art_id_s")
				_ = i
				cnt := decimal.NewFromInt(int64(rapid.IntRange(1, 10).Draw(rt, "cnt_s")))
				productos = append(productos, hydrateStandaloneProducto(artID, cnt, sharedOrigin, sharedDest))
			}

		case 1: // combo-child only
			combo := domain.HydrateCombo(domain.HydrateComboParams{
				ID:             uuid.New(),
				Nombre:         "Combo Prop",
				Precios:        zeroMontos(),
				Cantidad:       decimal.NewFromInt(int64(rapid.IntRange(1, 5).Draw(rt, "combo_cant"))),
				AlmacenOrigen:  sharedOrigin,
				AlmacenDestino: sharedDest,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
				CreatedBy:      uuid.New(),
				UpdatedBy:      uuid.New(),
			})
			combos = append(combos, combo)
			n := rapid.IntRange(1, 4).Draw(rt, "n_children")
			for i := range n {
				artID := rapid.IntRange(1, 999).Draw(rt, "art_id_c")
				_ = i
				cnt := decimal.NewFromInt(int64(rapid.IntRange(1, 8).Draw(rt, "cnt_c")))
				productos = append(productos, hydrateComboProducto(artID, cnt, combo.ID()))
			}

		case 2: // mixed: one standalone + one combo-child, same origin
			standalone := hydrateStandaloneProducto(
				rapid.IntRange(1, 499).Draw(rt, "art_id_sa"),
				decimal.NewFromInt(int64(rapid.IntRange(1, 5).Draw(rt, "cnt_sa"))),
				sharedOrigin, sharedDest,
			)
			combo := domain.HydrateCombo(domain.HydrateComboParams{
				ID:             uuid.New(),
				Nombre:         "Combo Mix",
				Precios:        zeroMontos(),
				Cantidad:       decimal.NewFromInt(int64(rapid.IntRange(1, 3).Draw(rt, "combo_cant_m"))),
				AlmacenOrigen:  sharedOrigin,
				AlmacenDestino: sharedDest,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
				CreatedBy:      uuid.New(),
				UpdatedBy:      uuid.New(),
			})
			child := hydrateComboProducto(
				rapid.IntRange(500, 999).Draw(rt, "art_id_ch"),
				decimal.NewFromInt(int64(rapid.IntRange(1, 5).Draw(rt, "cnt_ch"))),
				combo.ID(),
			)
			combos = append(combos, combo)
			productos = append(productos, standalone, child)
		}

		venta := buildVentaFixture(combos, productos)

		// Call resincronizarTraspaso.
		actor := uuid.New()
		now := time.Now()
		err := h.svc.resincronizarTraspaso(t.Context(), venta, actor, now)
		if err != nil {
			rt.Fatalf("resincronizarTraspaso failed unexpectedly: %v", err)
		}

		// Compute expected detalles via buildTraspasoDetallesFromVenta.
		expectedDetalles, expectedAlmacen, buildErr := buildTraspasoDetallesFromVenta(venta)
		if buildErr != nil {
			rt.Fatalf("buildTraspasoDetallesFromVenta failed unexpectedly: %v", buildErr)
		}

		// Verify resincronizarTraspaso called the fake exactly once.
		if inv.resincCalls.Load() != 1 {
			rt.Fatalf("expected exactly 1 resinc call, got %d", inv.resincCalls.Load())
		}
		recorded := inv.resincParams[0]

		// AlmacenOrigen must match.
		if recorded.AlmacenOrigen != expectedAlmacen {
			rt.Fatalf("almacenOrigen mismatch: got %d, expected %d", recorded.AlmacenOrigen, expectedAlmacen)
		}

		// Detalles must match (order-independent).
		gotSorted := sortDetalles(recorded.Detalles)
		wantSorted := sortDetalles(expectedDetalles)
		if len(gotSorted) != len(wantSorted) {
			rt.Fatalf("detalles length mismatch: got %d, expected %d", len(gotSorted), len(wantSorted))
		}
		for i := range gotSorted {
			if gotSorted[i].ArticuloID != wantSorted[i].ArticuloID {
				rt.Fatalf("detalle[%d] ArticuloID mismatch: got %d, expected %d",
					i, gotSorted[i].ArticuloID, wantSorted[i].ArticuloID)
			}
			if !gotSorted[i].Cantidad.Equal(wantSorted[i].Cantidad) {
				rt.Fatalf("detalle[%d] Cantidad mismatch: got %v, expected %v",
					i, gotSorted[i].Cantidad, wantSorted[i].Cantidad)
			}
		}
	})
}
