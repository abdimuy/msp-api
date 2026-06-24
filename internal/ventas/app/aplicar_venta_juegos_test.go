//nolint:misspell // domain vocabulary is Spanish (ventas, combos, juegos) per project convention.
package app_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── fake juego resolver ─────────────────────────────────────────────────────

// juegoResolverCall records one invocation of Resolve.
type juegoResolverCall struct {
	In outbound.MicrosipJuegoInput
}

// fakeMicrosipJuegoResolver records every Resolve call and returns a
// configurable result or error. Concurrency-safe.
type fakeMicrosipJuegoResolver struct {
	mu     sync.Mutex
	calls  []juegoResolverCall
	Err    error
	result outbound.MicrosipJuegoResult
}

func newFakeJuegoResolver(articuloID int) *fakeMicrosipJuegoResolver {
	return &fakeMicrosipJuegoResolver{
		result: outbound.MicrosipJuegoResult{ArticuloID: articuloID, Creado: false},
	}
}

func (f *fakeMicrosipJuegoResolver) Resolve(_ context.Context, in outbound.MicrosipJuegoInput) (outbound.MicrosipJuegoResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, juegoResolverCall{In: in})
	if f.Err != nil {
		return outbound.MicrosipJuegoResult{}, f.Err
	}
	return f.result, nil
}

func (f *fakeMicrosipJuegoResolver) callsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeMicrosipJuegoResolver) callsSnapshot() []juegoResolverCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]juegoResolverCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// newAplicarHarnessWithJuegos wires a full AplicarVenta harness that also
// has a juego resolver attached (enabled=true by default).
func newAplicarHarnessWithJuegos(t *testing.T, resolver *fakeMicrosipJuegoResolver, enabled bool) (
	*testHarness, *fakeAplicarConfig, *fakeMicrosipVentaWriter, *fakeMicrosipClienteWriter,
) {
	t.Helper()
	clock := fixedClock{T: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)}
	ventas := newFakeVentaRepo()
	storage := newFakeStorage()
	outbox := &fakeOutbox{}
	imageProc := &fakeImageProcessor{}
	cfg := newFakeAplicarConfig()
	writer := newFakeWriter(15239200, "Y00002300")
	clienteWriter := newFakeClienteWriter()
	svc := ventasapp.NewService(ventas, nil, nil, storage, clock, outbox, imageProc, nil, cfg, writer, clienteWriter)
	if resolver != nil {
		svc.WithJuegos(resolver, enabled, 99001)
	}
	h := &testHarness{
		svc: svc, ventas: ventas, storage: storage, outbox: outbox, imageProc: imageProc, clock: clock,
	}
	return h, cfg, writer, clienteWriter
}

// validContadoInputConCombos builds a CONTADO venta input that includes one
// combo with two child productos and a standalone producto. Returns the input
// and the combo's UUID for assertions.
func validContadoInputConCombos(t *testing.T) (ventasapp.CrearVentaInput, uuid.UUID) {
	t.Helper()
	comboID := uuid.New()
	hijoID1 := uuid.New()
	hijoID2 := uuid.New()
	standAloneID := uuid.New()
	vendedorID := uuid.New()
	one, two := 1, 2

	in := ventasapp.CrearVentaInput{
		ID:            uuid.New(),
		ClienteNombre: "Maria Lopez",
		Calle:         "Calle Juarez",
		Colonia:       "Centro",
		Poblacion:     "Tehuacan",
		Ciudad:        "Puebla",
		Latitud:       18.4626,
		Longitud:      -97.3833,
		FechaVenta:    time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:     "CONTADO",
		PrecioAnual:   decimal.NewFromInt(5000),
		PrecioCorto:   decimal.NewFromInt(4800),
		PrecioContado: decimal.NewFromInt(4500),
		Combos: []ventasapp.CrearVentaComboInput{{
			ID:             comboID,
			Nombre:         "Combo Sala",
			PrecioAnual:    decimal.NewFromInt(3000),
			PrecioCorto:    decimal.NewFromInt(2900),
			PrecioContado:  decimal.NewFromInt(2800),
			Cantidad:       decimal.NewFromInt(1),
			AlmacenOrigen:  one,
			AlmacenDestino: two,
		}},
		Productos: []ventasapp.CrearVentaProductoInput{
			{
				// child producto 1 of the combo
				ID:         hijoID1,
				ArticuloID: 1001,
				Articulo:   "Sillon Izquierdo",
				Cantidad:   decimal.NewFromInt(1),
				ComboID:    &comboID,
				// combo children must NOT carry almacenes (inherited from combo)
				AlmacenOrigen:  nil,
				AlmacenDestino: nil,
				PrecioAnual:    decimal.Zero,
				PrecioCorto:    decimal.Zero,
				PrecioContado:  decimal.Zero,
			},
			{
				// child producto 2 of the combo
				ID:             hijoID2,
				ArticuloID:     1002,
				Articulo:       "Sillon Derecho",
				Cantidad:       decimal.NewFromInt(1),
				ComboID:        &comboID,
				AlmacenOrigen:  nil,
				AlmacenDestino: nil,
				PrecioAnual:    decimal.Zero,
				PrecioCorto:    decimal.Zero,
				PrecioContado:  decimal.Zero,
			},
			{
				// standalone producto (not part of any combo)
				ID:             standAloneID,
				ArticuloID:     2001,
				Articulo:       "Mesa Centro",
				Cantidad:       decimal.NewFromInt(1),
				PrecioAnual:    decimal.NewFromInt(2000),
				PrecioCorto:    decimal.NewFromInt(1900),
				PrecioContado:  decimal.NewFromInt(1700),
				AlmacenOrigen:  &one,
				AlmacenDestino: &two,
			},
		},
		Vendedores: []ventasapp.CrearVentaVendedorInput{{
			ID:        vendedorID,
			UsuarioID: uuid.New(),
			Email:     "cobrador@muebleriamsp.mx",
			Nombre:    "Pedro Cobrador",
		}},
	}
	return in, comboID
}

// seedAprobadaConCombos seeds a CONTADO aprobada venta that has one combo with
// two child productos. Returns the venta ID and the combo's UUID.
func seedAprobadaConCombos(t *testing.T, h *testHarness) (uuid.UUID, uuid.UUID) {
	t.Helper()
	in, comboID := validContadoInputConCombos(t)
	cid := 47913
	in.ClienteID = &cid
	zona := 21563
	in.ZonaClienteID = &zona
	by := uuid.New()

	v, err := h.svc.CrearVenta(t.Context(), in, by)
	require.NoError(t, err)
	seedOneEvidencia(t, h, v.ID(), by)

	_, err = h.svc.EnviarARevision(t.Context(), v.ID(), by)
	require.NoError(t, err)
	_, err = h.svc.Aprobar(t.Context(), v.ID(), by)
	require.NoError(t, err)

	h.outbox.mu.Lock()
	h.outbox.calls = nil
	h.outbox.mu.Unlock()

	return v.ID(), comboID
}

// ─── tests ────────────────────────────────────────────────────────────────────

// TestAplicarVenta_Juegos_FeatureON_LlamadoPorCombo verifica que, con la
// feature habilitada, el resolver es llamado exactamente una vez por combo y
// que el mapa JuegosPorCombo llega correctamente al writer.
func TestAplicarVenta_Juegos_FeatureON_LlamadoPorCombo(t *testing.T) {
	t.Parallel()
	resolver := newFakeJuegoResolver(55001)
	h, _, writer, _ := newAplicarHarnessWithJuegos(t, resolver, true)
	ventaID, comboID := seedAprobadaConCombos(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), ventaID, uuid.New())

	require.NoError(t, err)
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())

	// Resolver must have been called exactly once (one combo in the venta).
	assert.Equal(t, 1, resolver.callsCount(), "resolver must be called once per combo")

	calls := resolver.callsSnapshot()
	require.Len(t, calls, 1)
	call := calls[0]

	// Correct nombre and lineaArticuloID.
	assert.Equal(t, "Combo Sala", call.In.NombrePropuesto)
	assert.Equal(t, 99001, call.In.LineaArticuloID)

	// Receta must have two components (articulo 1001 and 1002).
	componentes := call.In.Receta.Componentes()
	require.Len(t, componentes, 2, "receta must have 2 components")
	ids := []int{componentes[0].ArticuloID(), componentes[1].ArticuloID()}
	assert.ElementsMatch(t, []int{1001, 1002}, ids)

	// JuegosPorCombo must map comboID → resolved articleID.
	in := writer.lastInput()
	require.NotNil(t, in.JuegosPorCombo, "JuegosPorCombo must be non-nil")
	artID, ok := in.JuegosPorCombo[comboID]
	require.True(t, ok, "comboID must be present in JuegosPorCombo")
	assert.Equal(t, 55001, artID)
}

// TestAplicarVenta_Juegos_ResolverError_RollsBack verifica que un error del
// resolver propaga a AplicarVenta y la venta NO queda marcada como aplicada.
func TestAplicarVenta_Juegos_ResolverError_RollsBack(t *testing.T) {
	t.Parallel()
	resolver := newFakeJuegoResolver(0)
	resolver.Err = errors.New("juego resolver: firebird connection refused")
	h, _, writer, _ := newAplicarHarnessWithJuegos(t, resolver, true)
	ventaID, _ := seedAprobadaConCombos(t, h)

	_, err := h.svc.AplicarVenta(t.Context(), ventaID, uuid.New())

	require.Error(t, err, "AplicarVenta must fail when resolver returns an error")
	assert.Equal(t, 1, resolver.callsCount(), "resolver was attempted")
	// Writer must NOT have been called — error occurred before buildWriterInput returned.
	assert.Equal(t, 0, writer.callsCount(), "writer must NOT be called when resolver fails")

	// Venta must remain in SincronizacionPendiente.
	stored, findErr := h.svc.ObtenerVenta(t.Context(), ventaID)
	require.NoError(t, findErr)
	assert.Equal(t, domain.SincronizacionPendiente, stored.Sincronizacion(),
		"venta must not be aplicada when resolver fails")
}

// TestAplicarVenta_Juegos_FeatureOFF_NilResolver_NoLlama verifica que cuando
// el resolver es nil (feature no configurada), no se llama nada y la venta
// se aplica normalmente con JuegosPorCombo nil.
func TestAplicarVenta_Juegos_FeatureOFF_NilResolver_NoLlama(t *testing.T) {
	t.Parallel()
	// nil resolver — feature is effectively off.
	h, _, writer, _ := newAplicarHarnessWithJuegos(t, nil, false)
	ventaID, _ := seedAprobadaConCombos(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), ventaID, uuid.New())

	require.NoError(t, err)
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
	assert.Equal(t, 1, writer.callsCount(), "writer must still be called")
	in := writer.lastInput()
	assert.Nil(t, in.JuegosPorCombo, "JuegosPorCombo must be nil when feature is off")
}

// TestAplicarVenta_Juegos_FeatureOFF_EnabledFalse_NoLlama verifica que cuando
// el resolver está adjunto pero enabled=false, no se llama al resolver.
func TestAplicarVenta_Juegos_FeatureOFF_EnabledFalse_NoLlama(t *testing.T) {
	t.Parallel()
	resolver := newFakeJuegoResolver(55001)
	// Resolver is wired but enabled=false.
	h, _, writer, _ := newAplicarHarnessWithJuegos(t, resolver, false)
	ventaID, _ := seedAprobadaConCombos(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), ventaID, uuid.New())

	require.NoError(t, err)
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
	assert.Equal(t, 0, resolver.callsCount(), "resolver must NOT be called when enabled=false")
	assert.Equal(t, 1, writer.callsCount())
	in := writer.lastInput()
	assert.Nil(t, in.JuegosPorCombo, "JuegosPorCombo must be nil when feature is disabled")
}

// TestAplicarVenta_Juegos_SinCombos_FeatureON_NoLlama verifica que cuando la
// feature está activa pero la venta no tiene combos, el resolver nunca se llama
// y JuegosPorCombo llega vacío al writer.
func TestAplicarVenta_Juegos_SinCombos_FeatureON_NoLlama(t *testing.T) {
	t.Parallel()
	resolver := newFakeJuegoResolver(55001)
	h, _, writer, _ := newAplicarHarnessWithJuegos(t, resolver, true)
	// seedAprobadaContado has no combos.
	ventaID := seedAprobadaContado(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), ventaID, uuid.New())

	require.NoError(t, err)
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
	assert.Equal(t, 0, resolver.callsCount(), "resolver must NOT be called for venta with no combos")
	assert.Equal(t, 1, writer.callsCount())
	in := writer.lastInput()
	// Map is empty (not nil) because we built it — that's fine; writer sees len==0.
	assert.Empty(t, in.JuegosPorCombo, "JuegosPorCombo must be empty when venta has no combos")
}

// TestAplicarVenta_Juegos_FeatureON_MultiplesCombos verifica que el resolver
// es llamado una vez por cada combo cuando la venta tiene N combos.
func TestAplicarVenta_Juegos_FeatureON_MultiplesCombos(t *testing.T) {
	t.Parallel()
	resolver := newFakeJuegoResolver(77001)
	h, _, writer, _ := newAplicarHarnessWithJuegos(t, resolver, true)

	// Build a venta with 2 combos.
	combo1ID := uuid.New()
	combo2ID := uuid.New()
	one, two := 1, 2
	vendedorID := uuid.New()
	in := ventasapp.CrearVentaInput{
		ID:            uuid.New(),
		ClienteNombre: "Rosa Gutierrez",
		Calle:         "Av. Independencia",
		Colonia:       "Barrio Nuevo",
		Poblacion:     "Tehuacan",
		Ciudad:        "Puebla",
		Latitud:       18.4626,
		Longitud:      -97.3833,
		FechaVenta:    time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:     "CONTADO",
		PrecioAnual:   decimal.NewFromInt(8000),
		PrecioCorto:   decimal.NewFromInt(7800),
		PrecioContado: decimal.NewFromInt(7500),
		Combos: []ventasapp.CrearVentaComboInput{
			{
				ID: combo1ID, Nombre: "Combo Sala",
				PrecioAnual: decimal.NewFromInt(4000), PrecioCorto: decimal.NewFromInt(3900), PrecioContado: decimal.NewFromInt(3800),
				Cantidad: decimal.NewFromInt(1), AlmacenOrigen: one, AlmacenDestino: two,
			},
			{
				ID: combo2ID, Nombre: "Combo Comedor",
				PrecioAnual: decimal.NewFromInt(4000), PrecioCorto: decimal.NewFromInt(3900), PrecioContado: decimal.NewFromInt(3700),
				Cantidad: decimal.NewFromInt(1), AlmacenOrigen: one, AlmacenDestino: two,
			},
		},
		Productos: []ventasapp.CrearVentaProductoInput{
			{ID: uuid.New(), ArticuloID: 1001, Articulo: "Sillon A", Cantidad: decimal.NewFromInt(1), ComboID: &combo1ID},
			{ID: uuid.New(), ArticuloID: 2001, Articulo: "Mesa", Cantidad: decimal.NewFromInt(1), ComboID: &combo2ID},
		},
		Vendedores: []ventasapp.CrearVentaVendedorInput{{
			ID: vendedorID, UsuarioID: uuid.New(), Email: "vendedor@muebleriamsp.mx", Nombre: "Luis Vendedor",
		}},
	}
	cid := 47913
	in.ClienteID = &cid
	zona := 21563
	in.ZonaClienteID = &zona
	by := uuid.New()

	v, err := h.svc.CrearVenta(t.Context(), in, by)
	require.NoError(t, err)
	seedOneEvidencia(t, h, v.ID(), by)
	_, err = h.svc.EnviarARevision(t.Context(), v.ID(), by)
	require.NoError(t, err)
	_, err = h.svc.Aprobar(t.Context(), v.ID(), by)
	require.NoError(t, err)

	_, err = h.svc.AplicarVenta(t.Context(), v.ID(), uuid.New())
	require.NoError(t, err)

	// Resolver must be called exactly 2 times.
	assert.Equal(t, 2, resolver.callsCount(), "resolver called once per combo")

	in2 := writer.lastInput()
	require.Len(t, in2.JuegosPorCombo, 2, "JuegosPorCombo must have one entry per combo")
	assert.Equal(t, 77001, in2.JuegosPorCombo[combo1ID])
	assert.Equal(t, 77001, in2.JuegosPorCombo[combo2ID])
}

// TestAplicarVenta_Juegos_Regression_SinJuegos verifica que la ruta original
// (sin juegos configurados) continúa funcionando para ventas CONTADO y CREDITO.
func TestAplicarVenta_Juegos_Regression_SinJuegos(t *testing.T) {
	t.Parallel()
	t.Run("CONTADO sin juegos", func(t *testing.T) {
		t.Parallel()
		h, _, writer, _ := newAplicarHarness(t)
		id := seedAprobadaContado(t, h)

		v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())
		require.NoError(t, err)
		assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
		assert.Equal(t, 1, writer.callsCount())
		assert.Nil(t, writer.lastInput().JuegosPorCombo)
	})

	t.Run("CREDITO sin juegos", func(t *testing.T) {
		t.Parallel()
		h, _, writer, _ := newAplicarHarness(t)
		id := seedAprobadaCredito(t, h)

		v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())
		require.NoError(t, err)
		assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
		assert.Equal(t, 1, writer.callsCount())
		assert.Nil(t, writer.lastInput().JuegosPorCombo)
	})
}
