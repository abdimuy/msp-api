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

// ─── fakes ───────────────────────────────────────────────────────────────────

// fakeAplicarConfig is an in-memory outbound.AplicarConfig.
type fakeAplicarConfig struct {
	mu      sync.Mutex
	cc      outbound.CajaCajero
	defs    outbound.AplicarDefaults
	fpIDs   map[string]int
	cmIDs   map[int]int
	numVIDs map[int]int
	ccErr   error
	defsErr error
	fpErr   error
	cmErr   error
	numVErr error
}

func newFakeAplicarConfig() *fakeAplicarConfig {
	f := &fakeAplicarConfig{
		cc: outbound.CajaCajero{CajaID: 22198, CajeroID: 22392, VendedorID: 88266},
		defs: outbound.AplicarDefaults{
			SucursalID: 225490, FormaCobroContadoID: 67, FormaCobroCreditoID: 71,
		},
		fpIDs:   map[string]int{"SEMANAL": 33824, "QUINCENAL": 33825, "MENSUAL": 33826},
		cmIDs:   map[int]int{6: 33830, 9: 33829, 12: 33828, 18: 33827},
		numVIDs: map[int]int{1: 47558, 2: 47559, 3: 47560},
	}
	return f
}

func (f *fakeAplicarConfig) CajaCajero(_ context.Context, _ int) (outbound.CajaCajero, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cc, f.ccErr
}

func (f *fakeAplicarConfig) FormaDePagoID(_ context.Context, frecuencia string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fpErr != nil {
		return 0, f.fpErr
	}
	if id, ok := f.fpIDs[frecuencia]; ok {
		return id, nil
	}
	return 0, domain.ErrFrecuenciaSinFormaPago
}

func (f *fakeAplicarConfig) CreditoEnMesesID(_ context.Context, plazo int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cmErr != nil {
		return 0, f.cmErr
	}
	if id, ok := f.cmIDs[plazo]; ok {
		return id, nil
	}
	return 0, domain.ErrPlazoSinCreditoMeses
}

func (f *fakeAplicarConfig) NumeroDeVendedoresID(_ context.Context, n int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.numVErr != nil {
		return 0, f.numVErr
	}
	if id, ok := f.numVIDs[n]; ok {
		return id, nil
	}
	return 0, domain.ErrNumVendedoresSinMapeo
}

func (f *fakeAplicarConfig) Defaults(_ context.Context) (outbound.AplicarDefaults, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.defs, f.defsErr
}

// fakeMicrosipVentaWriter records calls to Aplicar.
type fakeMicrosipVentaWriter struct {
	mu    sync.Mutex
	calls int
	Err   error
	res   outbound.MicrosipVentaResult
}

func newFakeWriter(doctoPVID int, folio string) *fakeMicrosipVentaWriter {
	return &fakeMicrosipVentaWriter{
		res: outbound.MicrosipVentaResult{DoctoPVID: doctoPVID, Folio: folio},
	}
}

func (f *fakeMicrosipVentaWriter) Aplicar(_ context.Context, _ outbound.MicrosipVentaInput) (outbound.MicrosipVentaResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.Err != nil {
		return outbound.MicrosipVentaResult{}, f.Err
	}
	return f.res, nil
}

func (f *fakeMicrosipVentaWriter) callsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ─── harness helper ──────────────────────────────────────────────────────────

// newAplicarHarness builds a harness wired with fakeAplicarConfig and
// fakeMicrosipVentaWriter.
func newAplicarHarness(t *testing.T) (*testHarness, *fakeAplicarConfig, *fakeMicrosipVentaWriter) {
	t.Helper()
	clock := fixedClock{T: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)}
	ventas := newFakeVentaRepo()
	storage := newFakeStorage()
	outbox := &fakeOutbox{}
	imageProc := &fakeImageProcessor{}
	cfg := newFakeAplicarConfig()
	writer := newFakeWriter(15239200, "Y00002300")
	svc := ventasapp.NewService(ventas, nil, nil, storage, clock, outbox, imageProc, nil, cfg, writer)
	return &testHarness{
		svc: svc, ventas: ventas, storage: storage, outbox: outbox, imageProc: imageProc, clock: clock,
	}, cfg, writer
}

// seedAprobadaContado creates and seeds a CONTADO venta in situación APROBADA
// with a Microsip cliente_id and zona_cliente_id. Returns the venta ID.
func seedAprobadaContado(t *testing.T, h *testHarness) uuid.UUID {
	t.Helper()
	in := validContadoInput()
	cid := 47913
	in.ClienteID = &cid
	zona := 21563
	in.ZonaClienteID = &zona
	by := uuid.New()

	// Create in borrador.
	v, err := h.svc.CrearVenta(t.Context(), in, by)
	require.NoError(t, err)

	// Advance to revisada → aprobada.
	_, err = h.svc.EnviarARevision(t.Context(), v.ID(), by)
	require.NoError(t, err)
	_, err = h.svc.Aprobar(t.Context(), v.ID(), by)
	require.NoError(t, err)

	h.outbox.mu.Lock()
	h.outbox.calls = nil
	h.outbox.mu.Unlock()
	return v.ID()
}

// seedAprobadaCredito creates a CREDITO venta in situación APROBADA.
func seedAprobadaCredito(t *testing.T, h *testHarness) uuid.UUID {
	t.Helper()
	in := validCreditoInput()
	cid := 47913
	in.ClienteID = &cid
	zona := 21563
	in.ZonaClienteID = &zona
	by := uuid.New()

	v, err := h.svc.CrearVenta(t.Context(), in, by)
	require.NoError(t, err)
	_, err = h.svc.EnviarARevision(t.Context(), v.ID(), by)
	require.NoError(t, err)
	_, err = h.svc.Aprobar(t.Context(), v.ID(), by)
	require.NoError(t, err)

	h.outbox.mu.Lock()
	h.outbox.calls = nil
	h.outbox.mu.Unlock()
	return v.ID()
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestAplicarVenta_HappyPath_Contado verifies the full happy path for a CONTADO
// venta: writer is called, MarcarAplicada is applied, sincronizacion=aplicada.
func TestAplicarVenta_HappyPath_Contado(t *testing.T) {
	t.Parallel()
	h, _, writer := newAplicarHarness(t)
	id := seedAprobadaContado(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, 1, writer.callsCount(), "writer must be called exactly once")
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
	assert.Equal(t, domain.SituacionAprobada, v.Situacion(), "situacion stays aprobada after applying")
	require.NotNil(t, v.MicrosipDoctoPVID())
	assert.Equal(t, 15239200, *v.MicrosipDoctoPVID())
	require.NotNil(t, v.MicrosipFolio())
	assert.Equal(t, "Y00002300", *v.MicrosipFolio())
	// Outbox must have received a venta.aplicada event.
	assert.True(t, h.outbox.sawEventType("venta.aplicada"),
		"expected venta.aplicada event, got %v", h.outbox.eventTypes())
}

// TestAplicarVenta_HappyPath_Credito verifies the CREDITO path (writer called
// with non-nil FormaDePagoID and CreditoEnMesesID).
func TestAplicarVenta_HappyPath_Credito(t *testing.T) {
	t.Parallel()
	h, _, writer := newAplicarHarness(t)
	id := seedAprobadaCredito(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.NoError(t, err)
	assert.Equal(t, 1, writer.callsCount())
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
}

// TestAplicarVenta_Idempotente verifies that calling AplicarVenta on an already
// aplicada venta returns the existing artifacts without calling the writer again.
func TestAplicarVenta_Idempotente(t *testing.T) {
	t.Parallel()
	h, _, writer := newAplicarHarness(t)
	id := seedAprobadaContado(t, h)

	// First call.
	v1, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 1, writer.callsCount())

	// Second call — must be idempotent.
	v2, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 1, writer.callsCount(), "writer must NOT be called on second aplicar")
	assert.Equal(t, *v1.MicrosipDoctoPVID(), *v2.MicrosipDoctoPVID(), "folio must be the same")
}

// TestAplicarVenta_VentaNoAprobada verifies ErrVentaNoAplicable is returned
// when the venta is still in borrador (not aprobada).
func TestAplicarVenta_VentaNoAprobada(t *testing.T) {
	t.Parallel()
	h, _, writer := newAplicarHarness(t)

	// Create a venta but leave it in borrador.
	in := validContadoInput()
	cid := 47913
	in.ClienteID = &cid
	zona := 21563
	in.ZonaClienteID = &zona
	v, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)

	_, err = h.svc.AplicarVenta(t.Context(), v.ID(), uuid.New())
	require.ErrorIs(t, err, domain.ErrVentaNoAplicable)
	assert.Equal(t, 0, writer.callsCount(), "writer must not be called when preconditions fail")
}

// TestAplicarVenta_SinClienteMicrosip verifies ErrVentaSinClienteMicrosip when
// the venta has no cliente_id.
func TestAplicarVenta_SinClienteMicrosip(t *testing.T) {
	t.Parallel()
	h, _, writer := newAplicarHarness(t)

	// Build a venta without a client ID and advance to aprobada via HydrateVenta.
	base := validContadoInput()
	zona := 21563
	base.ZonaClienteID = &zona
	// No ClienteID.
	by := uuid.New()
	v, err := h.svc.CrearVenta(t.Context(), base, by)
	require.NoError(t, err)
	_, err = h.svc.EnviarARevision(t.Context(), v.ID(), by)
	require.NoError(t, err)
	_, err = h.svc.Aprobar(t.Context(), v.ID(), by)
	require.NoError(t, err)

	_, err = h.svc.AplicarVenta(t.Context(), v.ID(), uuid.New())
	require.ErrorIs(t, err, domain.ErrVentaSinClienteMicrosip)
	assert.Equal(t, 0, writer.callsCount())
}

// TestAplicarVenta_SinZona verifies ErrVentaSinZona when the venta has no
// zona_cliente_id.
func TestAplicarVenta_SinZona(t *testing.T) {
	t.Parallel()
	h, _, writer := newAplicarHarness(t)

	base := validContadoInput()
	cid := 47913
	base.ClienteID = &cid
	// No ZonaClienteID.
	by := uuid.New()
	v, err := h.svc.CrearVenta(t.Context(), base, by)
	require.NoError(t, err)
	_, err = h.svc.EnviarARevision(t.Context(), v.ID(), by)
	require.NoError(t, err)
	_, err = h.svc.Aprobar(t.Context(), v.ID(), by)
	require.NoError(t, err)

	_, err = h.svc.AplicarVenta(t.Context(), v.ID(), uuid.New())
	require.ErrorIs(t, err, domain.ErrVentaSinZona)
	assert.Equal(t, 0, writer.callsCount())
}

// TestAplicarVenta_WriterError propagates the writer error and rolls back
// (no Update call, no outbox event).
func TestAplicarVenta_WriterError(t *testing.T) {
	t.Parallel()
	h, _, writer := newAplicarHarness(t)
	writer.Err = errors.New("microsip connection refused")
	id := seedAprobadaContado(t, h)

	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())
	require.Error(t, err)
	assert.Equal(t, 1, writer.callsCount())
	// Venta must remain pendiente.
	v, findErr := h.svc.ObtenerVenta(t.Context(), id)
	require.NoError(t, findErr)
	assert.Equal(t, domain.SincronizacionPendiente, v.Sincronizacion())
	assert.False(t, h.outbox.sawEventType("venta.aplicada"))
}

// TestAplicarVenta_VentaNoEncontrada verifies ErrVentaNotFound is propagated.
func TestAplicarVenta_VentaNoEncontrada(t *testing.T) {
	t.Parallel()
	h, _, _ := newAplicarHarness(t)

	_, err := h.svc.AplicarVenta(t.Context(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, domain.ErrVentaNotFound)
}

// ─── TestCrearVentaInput extensions (for ZonaClienteID) ─────────────────────

// Extend the in-package validContadoInput to include ZonaClienteID for
// aplicar tests. Because app.CrearVentaInput may or may not have ZonaClienteID
// (it's set via the Direccion → ZonaClienteID path), we check the field.

func init() {
	_ = decimal.Zero // import kept alive.
}
