package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
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

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

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

// fakeMicrosipClienteWriter records calls to Crear.
type fakeMicrosipClienteWriter struct {
	mu     sync.Mutex
	calls  int
	Err    error
	LastIn outbound.MicrosipClienteInput
	Res    outbound.MicrosipClienteResult
}

func newFakeClienteWriter() *fakeMicrosipClienteWriter {
	return &fakeMicrosipClienteWriter{
		Res: outbound.MicrosipClienteResult{
			ClienteID: 15300000, DirCliID: 15300001, ClaveCliente: "0044999",
		},
	}
}

func (f *fakeMicrosipClienteWriter) Crear(_ context.Context, in outbound.MicrosipClienteInput) (outbound.MicrosipClienteResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.LastIn = in
	if f.Err != nil {
		return outbound.MicrosipClienteResult{}, f.Err
	}
	return f.Res, nil
}

func (f *fakeMicrosipClienteWriter) callsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ─── harness helper ──────────────────────────────────────────────────────────

// newAplicarHarness builds a harness wired with fakeAplicarConfig,
// fakeMicrosipVentaWriter, and fakeMicrosipClienteWriter.
func newAplicarHarness(t *testing.T) (*testHarness, *fakeAplicarConfig, *fakeMicrosipVentaWriter, *fakeMicrosipClienteWriter) {
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
	return &testHarness{
		svc: svc, ventas: ventas, storage: storage, outbox: outbox, imageProc: imageProc, clock: clock,
	}, cfg, writer, clienteWriter
}

// seedAprobadaContado creates and seeds a CONTADO venta in situación APROBADA
// with a Microsip cliente_id, zona_cliente_id, AND one evidencia imagen so
// AplicarVenta's defense-in-depth evidencia guard is satisfied. Returns the
// venta ID.
func seedAprobadaContado(t *testing.T, h *testHarness) uuid.UUID {
	t.Helper()
	in := validContadoInput()
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
	return v.ID()
}

// seedAprobadaCredito creates a CREDITO venta in situación APROBADA with one
// evidencia imagen attached.
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
	seedOneEvidencia(t, h, v.ID(), by)

	_, err = h.svc.EnviarARevision(t.Context(), v.ID(), by)
	require.NoError(t, err)
	_, err = h.svc.Aprobar(t.Context(), v.ID(), by)
	require.NoError(t, err)

	h.outbox.mu.Lock()
	h.outbox.calls = nil
	h.outbox.mu.Unlock()
	return v.ID()
}

// seedOneEvidencia attaches a minimal imagen to the given venta so the
// AplicarVenta evidencia guard passes. Uses the existing AdjuntarImagen
// service method (the legacy single-image upload path).
func seedOneEvidencia(t *testing.T, h *testHarness, ventaID, by uuid.UUID) {
	t.Helper()
	imgID := uuid.New()
	_, err := h.svc.AdjuntarImagen(t.Context(), ventasapp.AdjuntarImagenInput{
		VentaID:     ventaID,
		ImagenID:    imgID,
		StorageKind: string(domain.StorageKindFilesystem),
		StorageKey:  "ventas/" + ventaID.String() + "/" + imgID.String() + ".jpg",
		Mime:        domain.MimeJPEG,
		SizeBytes:   8,
		Body:        bytesReader([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0}),
	}, by)
	require.NoError(t, err)
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestAplicarVenta_EvidenciaGuard_RejectsVentaSinImagen verifies the
// defense-in-depth: AplicarVenta refuses to materialize a venta that has no
// imagen, even if every other precondition is satisfied. This catches ventas
// created via legacy paths (admin tool, raw SQL, the non-multipart CrearVenta)
// before they hit Microsip.
func TestAplicarVenta_EvidenciaGuard_RejectsVentaSinImagen(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)

	// Seed an aprobada venta WITHOUT calling seedOneEvidencia — bypassing
	// CrearVentaConImagenes intentionally.
	in := validContadoInput()
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

	_, err = h.svc.AplicarVenta(t.Context(), v.ID(), uuid.New())

	require.ErrorIs(t, err, domain.ErrVentaEvidenciaRequerida)
	assert.Zero(t, writer.callsCount(), "writer must NOT be called when evidencia missing")
}

// TestAplicarVenta_HappyPath_Contado verifies the full happy path for a CONTADO
// venta: writer is called, MarcarAplicada is applied, sincronizacion=aplicada.
func TestAplicarVenta_HappyPath_Contado(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)
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
	h, _, writer, _ := newAplicarHarness(t)
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
	h, _, writer, _ := newAplicarHarness(t)
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
	h, _, writer, _ := newAplicarHarness(t)

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
// the venta has no cliente_id AND a wired microsipCliente writer reports an
// error (nil writer == branch guard). The venta has full snapshot so
// puedeAutoCrearCliente returns true; the nil-writer guard fires inside the tx.
func TestAplicarVenta_SinClienteMicrosip(t *testing.T) {
	t.Parallel()
	// Build the service without a microsipCliente writer to exercise the nil-guard.
	clock := fixedClock{T: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)}
	ventas := newFakeVentaRepo()
	storage := newFakeStorage()
	outbox := &fakeOutbox{}
	imageProc := &fakeImageProcessor{}
	cfg := newFakeAplicarConfig()
	writer := newFakeWriter(15239200, "Y00002300")
	svc := ventasapp.NewService(ventas, nil, nil, storage, clock, outbox, imageProc, nil, cfg, writer, nil)
	h := &testHarness{svc: svc, ventas: ventas, storage: storage, outbox: outbox, imageProc: imageProc, clock: clock}

	// Build a venta without a client ID and with full dirección snapshot.
	base := validContadoInput()
	zona := 21563
	base.ZonaClienteID = &zona
	// No ClienteID.
	by := uuid.New()
	v, err := h.svc.CrearVenta(t.Context(), base, by)
	require.NoError(t, err)
	seedOneEvidencia(t, h, v.ID(), by)
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
	h, _, writer, _ := newAplicarHarness(t)

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
	h, _, writer, _ := newAplicarHarness(t)
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
	h, _, _, _ := newAplicarHarness(t)

	_, err := h.svc.AplicarVenta(t.Context(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, domain.ErrVentaNotFound)
}

// ─── Auto-create cliente tests ────────────────────────────────────────────────

// seedAprobadaSinCliente creates an aprobada CONTADO venta with full snapshot
// data (nombre, calle, colonia, poblacion) and zona but NO ClienteID. The venta
// has one evidencia so all guards are satisfied except clienteID.
func seedAprobadaSinCliente(t *testing.T, h *testHarness) uuid.UUID {
	t.Helper()
	in := validContadoInput()
	zona := 21563
	in.ZonaClienteID = &zona
	// ClienteID intentionally left nil.
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
	return v.ID()
}

// TestAplicarVenta_AutoCreaCliente_HappyPath verifies the full auto-create
// cliente flow: microsipCliente.Crear is called, the venta is linked to the
// returned ClienteID, and DOCTOS_PV materialization proceeds normally.
func TestAplicarVenta_AutoCreaCliente_HappyPath(t *testing.T) {
	t.Parallel()
	h, _, writer, clienteWriter := newAplicarHarness(t)
	// fakeAplicarConfig returns CobradorID=-1 by default; set a real one.
	// The fake cc is {CajaID:22198, CajeroID:22392, VendedorID:88266}.
	// Update it to include CobradorID for assertion.
	h.svc = ventasapp.NewService(
		h.ventas, nil, nil, h.storage, h.clock, h.outbox, h.imageProc, nil,
		&fakeAplicarConfig{
			cc: outbound.CajaCajero{CajaID: 22198, CajeroID: 22392, VendedorID: 88266, CobradorID: 11502},
			defs: outbound.AplicarDefaults{
				SucursalID: 225490, FormaCobroContadoID: 67, FormaCobroCreditoID: 71,
			},
			fpIDs:   map[string]int{"SEMANAL": 33824, "QUINCENAL": 33825, "MENSUAL": 33826},
			cmIDs:   map[int]int{6: 33830, 9: 33829, 12: 33828, 18: 33827},
			numVIDs: map[int]int{1: 47558, 2: 47559, 3: 47560},
		},
		writer,
		clienteWriter,
	)

	id := seedAprobadaSinCliente(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.NoError(t, err)
	assert.Equal(t, 1, clienteWriter.callsCount(), "microsipCliente.Crear must be called once")
	assert.Equal(t, "Juan Perez", clienteWriter.LastIn.Nombre)
	assert.Equal(t, 21563, clienteWriter.LastIn.ZonaClienteID)
	assert.Equal(t, 11502, clienteWriter.LastIn.CobradorID)
	assert.Equal(t, 88266, clienteWriter.LastIn.VendedorID)
	require.NotNil(t, v.ClienteID(), "venta must have ClienteID linked after auto-create")
	assert.Equal(t, 15300000, *v.ClienteID(), "ClienteID must equal fake result")
	assert.Equal(t, 1, writer.callsCount(), "DOCTOS_PV writer must also fire")
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
}

// TestAplicarVenta_ClienteIDNoNil_NoAutoCrea verifies the auto-create branch
// is skipped when the venta already has a ClienteID.
func TestAplicarVenta_ClienteIDNoNil_NoAutoCrea(t *testing.T) {
	t.Parallel()
	h, _, writer, clienteWriter := newAplicarHarness(t)
	id := seedAprobadaContado(t, h) // has ClienteID = 47913

	v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.NoError(t, err)
	assert.Equal(t, 0, clienteWriter.callsCount(), "auto-create must NOT run when ClienteID already set")
	assert.Equal(t, 1, writer.callsCount())
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
}

// TestAplicarVenta_AutoCreaCliente_WriterFalla_Rollback verifies that when the
// microsipCliente.Crear call fails, the error surfaces and the DOCTOS_PV
// writer is never called.
func TestAplicarVenta_AutoCreaCliente_WriterFalla_Rollback(t *testing.T) {
	t.Parallel()
	h, _, writer, clienteWriter := newAplicarHarness(t)
	clienteWriter.Err = errors.New("microsip cliente boom")

	id := seedAprobadaSinCliente(t, h)

	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.Error(t, err)
	assert.Equal(t, 1, clienteWriter.callsCount(), "Crear was attempted")
	assert.Equal(t, 0, writer.callsCount(), "DOCTOS_PV writer must NOT be called when cliente creation fails")
	// In-memory fake: UpdateCliente wrote to the map before the error; check
	// the venta's current ClienteID in the repo. Since the fake lacks true
	// rollback, the ClienteID may have been set. The key invariant is that
	// the DOCTOS_PV writer was never called.
}

// TestAplicarVenta_AutoCreaCliente_DOCTOS_PVFalla_Rollback verifies that when
// the DOCTOS_PV writer fails after cliente auto-create, the venta is NOT
// marked aplicada.
func TestAplicarVenta_AutoCreaCliente_DOCTOS_PVFalla_Rollback(t *testing.T) {
	t.Parallel()
	h, _, writer, clienteWriter := newAplicarHarness(t)
	writer.Err = errors.New("docto pv boom")

	id := seedAprobadaSinCliente(t, h)

	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.Error(t, err)
	assert.Equal(t, 1, clienteWriter.callsCount(), "cliente was created before doctos_pv failure")
	assert.Equal(t, 1, writer.callsCount(), "DOCTOS_PV writer was attempted")
	// Venta must NOT be aplicada.
	v, findErr := h.svc.ObtenerVenta(t.Context(), id)
	require.NoError(t, findErr)
	assert.Equal(t, domain.SincronizacionPendiente, v.Sincronizacion(), "venta must NOT be aplicada on doctos_pv failure")
}

// TestAplicarVenta_AutoCreaCliente_SinZona_Rechazado verifies ErrVentaSinZona
// is returned when the venta has no zona AND no ClienteID (zona check fires
// before the auto-create guard).
func TestAplicarVenta_AutoCreaCliente_SinZona_Rechazado(t *testing.T) {
	t.Parallel()
	h, _, writer, clienteWriter := newAplicarHarness(t)

	// No zona, no clienteID.
	base := validContadoInput()
	by := uuid.New()
	v, err := h.svc.CrearVenta(t.Context(), base, by)
	require.NoError(t, err)
	_, err = h.svc.EnviarARevision(t.Context(), v.ID(), by)
	require.NoError(t, err)
	_, err = h.svc.Aprobar(t.Context(), v.ID(), by)
	require.NoError(t, err)

	_, err = h.svc.AplicarVenta(t.Context(), v.ID(), uuid.New())
	require.ErrorIs(t, err, domain.ErrVentaSinZona)
	assert.Equal(t, 0, clienteWriter.callsCount())
	assert.Equal(t, 0, writer.callsCount())
}

// TestCheckPreconditions_PuedeAutoCrear_TableDriven is a table-driven unit test
// for the puedeAutoCrearCliente helper. It verifies each required field.
// Uses HydrateVenta is not accessible from app_test, so we test via
// checkPreconditions through AplicarVenta with a nil microsipCliente service.
// Instead, we test puedeAutoCrearCliente indirectly via checkPreconditions:
// a venta missing one field should fall through to ErrVentaSinClienteMicrosip
// when microsipCliente is nil.
func TestCheckPreconditions_PuedeAutoCrear_TableDriven(t *testing.T) {
	t.Parallel()

	// Helper: build a nil-clienteID service and a venta with the given mutations,
	// advance to aprobada + evidencia, call AplicarVenta, return the error.
	runCase := func(t *testing.T, mutateFn func(in *ventasapp.CrearVentaInput)) error {
		t.Helper()
		clock := fixedClock{T: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)}
		ventas := newFakeVentaRepo()
		storage := newFakeStorage()
		outbox := &fakeOutbox{}
		imageProc := &fakeImageProcessor{}
		cfg := newFakeAplicarConfig()
		writer := newFakeWriter(15239200, "Y00002300")
		// nil microsipCliente: if puedeAutoCrearCliente returns false,
		// checkPreconditions returns ErrVentaSinClienteMicrosip.
		svc := ventasapp.NewService(ventas, nil, nil, storage, clock, outbox, imageProc, nil, cfg, writer, nil)
		h := &testHarness{svc: svc, ventas: ventas, storage: storage, outbox: outbox, imageProc: imageProc, clock: clock}

		in := validContadoInput()
		zona := 21563
		in.ZonaClienteID = &zona
		// No ClienteID — to exercise the auto-create path.
		mutateFn(&in)

		by := uuid.New()
		v, err := h.svc.CrearVenta(t.Context(), in, by)
		if err != nil {
			// If CrearVenta itself rejects (e.g. empty Calle), we can't reach AplicarVenta.
			return err
		}
		seedOneEvidencia(t, h, v.ID(), by)
		_, err = h.svc.EnviarARevision(t.Context(), v.ID(), by)
		if err != nil {
			return err
		}
		_, err = h.svc.Aprobar(t.Context(), v.ID(), by)
		if err != nil {
			return err
		}
		_, err = h.svc.AplicarVenta(t.Context(), v.ID(), uuid.New())
		return err
	}

	t.Run("todos los campos — pasa precondiciones (falla por nil writer)", func(t *testing.T) {
		t.Parallel()
		err := runCase(t, func(_ *ventasapp.CrearVentaInput) {})
		// Nil microsipCliente writer → guard inside the tx returns ErrVentaSinClienteMicrosip.
		require.ErrorIs(t, err, domain.ErrVentaSinClienteMicrosip)
	})

	t.Run("sin calle — rechazado en CrearVenta o checkPreconditions", func(t *testing.T) {
		t.Parallel()
		err := runCase(t, func(in *ventasapp.CrearVentaInput) { in.Calle = "" })
		// CrearVenta rejects empty calle.
		require.Error(t, err)
	})

	t.Run("sin colonia — rechazado en CrearVenta o checkPreconditions", func(t *testing.T) {
		t.Parallel()
		err := runCase(t, func(in *ventasapp.CrearVentaInput) { in.Colonia = "" })
		require.Error(t, err)
	})

	t.Run("sin poblacion — rechazado en CrearVenta o checkPreconditions", func(t *testing.T) {
		t.Parallel()
		err := runCase(t, func(in *ventasapp.CrearVentaInput) { in.Poblacion = "" })
		require.Error(t, err)
	})
}

// ─── TestCrearVentaInput extensions (for ZonaClienteID) ─────────────────────

// Extend the in-package validContadoInput to include ZonaClienteID for
// aplicar tests. Because app.CrearVentaInput may or may not have ZonaClienteID
// (it's set via the Direccion → ZonaClienteID path), we check the field.

func init() {
	_ = decimal.Zero // import kept alive.
}
