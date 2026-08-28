//nolint:misspell // ventas vocabulary is Spanish (vendedores, camioneta) per project convention.
package app_test

// Tests for the server-side resolution of a venta's vendedores.
//
// The contract under test, restated so a failure here is readable without
// reading the implementation:
//
//   - the camioneta is the venta's almacén de origen;
//   - the roster wins whenever it yields at least one usuario this API can
//     reference;
//   - EVERY other outcome — no camioneta, ambiguous camioneta, a roster that
//     errors, times out, answers empty, or answers with people who have no
//     MSP_USUARIOS row — keeps the list the client sent;
//   - and whichever path ran leaves one evidence event on the outbox.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── Fakes ──────────────────────────────────────────────────────────────────

// fakeRoster is an in-memory outbound.VendedoresDeCamionetaResolver. It
// records what it was asked and, crucially, whether the context it received
// carried a deadline — the safety net is only real if the caller bounds it.
type fakeRoster struct {
	mu sync.Mutex

	res outbound.VendedoresDeCamioneta
	err error
	// blockUntilDone makes the fake hang until the context is done, standing
	// in for an unresponsive Firestore.
	blockUntilDone bool

	calls         int
	lastCamioneta int
	hadDeadline   bool
	deadline      time.Time
}

func (f *fakeRoster) VendedoresDeCamioneta(
	ctx context.Context, camionetaID int,
) (outbound.VendedoresDeCamioneta, error) {
	f.mu.Lock()
	f.calls++
	f.lastCamioneta = camionetaID
	f.deadline, f.hadDeadline = ctx.Deadline()
	block, res, err := f.blockUntilDone, f.res, f.err
	f.mu.Unlock()

	if block {
		<-ctx.Done()
		return outbound.VendedoresDeCamioneta{}, ctx.Err()
	}
	return res, err
}

// rosterSnapshot is what the fake observed, copied out under the lock.
type rosterSnapshot struct {
	calls       int
	camioneta   int
	hadDeadline bool
	deadline    time.Time
}

func (f *fakeRoster) snapshot() rosterSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return rosterSnapshot{
		calls:       f.calls,
		camioneta:   f.lastCamioneta,
		hadDeadline: f.hadDeadline,
		deadline:    f.deadline,
	}
}

// fakeUsuarioEmails is an in-memory outbound.VendedorUsuarioEmailResolver.
// The map is keyed by canonical email; anything outside it has no
// MSP_USUARIOS row.
type fakeUsuarioEmails struct {
	mu sync.Mutex

	known map[string]outbound.UsuarioDeVendedor
	err   error

	calls     int
	lastQuery []string
}

func (f *fakeUsuarioEmails) UsuariosPorEmail(
	_ context.Context, emails []string,
) (map[string]outbound.UsuarioDeVendedor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastQuery = append([]string{}, emails...)
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]outbound.UsuarioDeVendedor, len(emails))
	for _, e := range emails {
		if u, ok := f.known[e]; ok {
			out[e] = u
		}
	}
	return out, nil
}

// ─── Harness ────────────────────────────────────────────────────────────────

// vendedoresHarness is a testHarness whose Service has the roster wired.
type vendedoresHarness struct {
	*testHarness
	roster   *fakeRoster
	usuarios *fakeUsuarioEmails
}

// newVendedoresHarness wires a Service with the given roster answer and the
// given email → usuario map.
func newVendedoresHarness(t *testing.T, roster *fakeRoster, usuarios *fakeUsuarioEmails) *vendedoresHarness {
	t.Helper()
	h := newHarness(t)
	h.svc = ventasapp.NewService(
		h.ventas, nil, nil, h.storage, h.clock, h.outbox, h.imageProc, nil, nil, nil, nil,
	).WithVendedoresDeCamioneta(roster, usuarios)
	return &vendedoresHarness{testHarness: h, roster: roster, usuarios: usuarios}
}

// camionetaDePrueba is the almacén de origen every input in this file uses,
// and therefore the camioneta the roster must be asked about.
const camionetaDePrueba = 11341

// inputEnCamioneta returns a CONTADO input whose single producto ships from
// camionetaDePrueba, carrying exactly the supplied vendedores.
func inputEnCamioneta(vendedores ...ventasapp.CrearVentaVendedorInput) ventasapp.CrearVentaInput {
	in := validContadoInput()
	origen := camionetaDePrueba
	in.Productos[0].AlmacenOrigen = &origen
	in.Vendedores = vendedores
	return in
}

// vendedorCliente builds one vendedor as the phone would send it.
func vendedorCliente(usuarioID uuid.UUID, email, nombre string) ventasapp.CrearVentaVendedorInput {
	return ventasapp.CrearVentaVendedorInput{
		ID: uuid.New(), UsuarioID: usuarioID, Email: email, Nombre: nombre,
	}
}

// evidencia returns the single vendedores-resueltos payload on the outbox,
// failing when there is not exactly one.
func evidencia(t *testing.T, h *vendedoresHarness) map[string]any {
	t.Helper()
	var found []map[string]any
	for _, c := range h.outbox.snapshot() {
		if c.EventType != domain.EventTypeVentaVendedoresResueltos {
			continue
		}
		payload, ok := c.Payload.(map[string]any)
		require.True(t, ok, "payload must be a map[string]any")
		found = append(found, payload)
	}
	require.Len(t, found, 1, "exactly one evidence event per created venta")
	return found[0]
}

// emailsDeVenta returns the canonical emails the persisted venta carries.
func emailsDeVenta(v *domain.Venta) []string {
	out := make([]string, 0, v.VendedoresCount())
	for vd := range v.Vendedores() {
		out = append(out, vd.Snapshot().Email())
	}
	return out
}

// ─── El roster gana ─────────────────────────────────────────────────────────

// TestVendedores_RosterDevuelveDos_ClienteMandoUno is the case the whole
// change exists for: the phone lost a vendedor, the roster has both, and the
// venta must end up with both — with the discrepancy on the record.
func TestVendedores_RosterDevuelveDos_ClienteMandoUno(t *testing.T) {
	t.Parallel()
	ana, beto := uuid.New(), uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{
			{Email: "ana@muebleriamsp.mx", Nombre: "ANA LAURA MENDEZ"},
			{Email: "beto@muebleriamsp.mx", Nombre: "BETO SUAREZ"},
		},
	}}
	usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{
		"ana@muebleriamsp.mx":  {ID: ana, Nombre: "ANA (BD)"},
		"beto@muebleriamsp.mx": {ID: beto, Nombre: "BETO (BD)"},
	}}
	h := newVendedoresHarness(t, roster, usuarios)

	in := inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA LAURA MENDEZ"))
	venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)

	assert.ElementsMatch(t,
		[]string{"ana@muebleriamsp.mx", "beto@muebleriamsp.mx"},
		emailsDeVenta(venta),
		"the roster's people win over the phone's shorter list")

	obs := roster.snapshot()
	assert.Equal(t, 1, obs.calls, "the roster is consulted exactly once")
	assert.Equal(t, camionetaDePrueba, obs.camioneta,
		"the camioneta asked about is the venta's almacén de origen")

	ev := evidencia(t, h)
	assert.Equal(t, domain.OrigenVendedoresRoster, ev["origen"])
	assert.Equal(t, "ok", ev["motivo"])
	assert.Equal(t, false, ev["coinciden"], "the two sides disagreed and it is recorded")
	assert.Equal(t, []string{"beto@muebleriamsp.mx"}, ev["solo_en_roster"])
	assert.Equal(t, []string{}, ev["solo_en_cliente"])
}

// TestVendedores_ReutilizaElIDDeFilaDelCliente pins that a vendedor the phone
// already sent keeps its row id, so a replayed create writes the same bytes.
func TestVendedores_ReutilizaElIDDeFilaDelCliente(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{{Email: "ana@muebleriamsp.mx", Nombre: "ANA"}},
	}}
	usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{
		"ana@muebleriamsp.mx": {ID: ana, Nombre: "ANA (BD)"},
	}}
	h := newVendedoresHarness(t, roster, usuarios)

	delCliente := vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")
	venta, err := h.svc.CrearVenta(t.Context(), inputEnCamioneta(delCliente), uuid.New())
	require.NoError(t, err)

	for vd := range venta.Vendedores() {
		assert.Equal(t, delCliente.ID, vd.ID(),
			"the row id the phone sent must survive server-side resolution")
	}
}

// ─── El defecto latente: mayúsculas ─────────────────────────────────────────

// TestVendedores_CorreoConMayusculas_SeResuelve covers the defect that made a
// vendedor vanish without a log: the roster holds a capitalized address while
// every id lookup is keyed by the canonical one. Both sides normalize now, so
// the person resolves.
func TestVendedores_CorreoConMayusculas_SeResuelve(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{
			// Exactly what a hand-typed Firestore document looks like.
			{Email: "  Ana.Laura@MuebleriaMSP.MX ", Nombre: "ANA LAURA"},
		},
	}}
	usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{
		"ana.laura@muebleriamsp.mx": {ID: ana, Nombre: "ANA (BD)"},
	}}
	h := newVendedoresHarness(t, roster, usuarios)

	// The phone sent nobody useful — a different person entirely.
	otro := uuid.New()
	in := inputEnCamioneta(vendedorCliente(otro, "OTRO@MuebleriaMSP.MX", "OTRO"))
	venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)

	assert.Equal(t, []string{"ana.laura@muebleriamsp.mx"}, emailsDeVenta(venta),
		"a capitalized roster address must resolve, not disappear")

	ev := evidencia(t, h)
	assert.Equal(t, domain.OrigenVendedoresRoster, ev["origen"])
	assert.Equal(t, []string{"ana.laura@muebleriamsp.mx"}, ev["roster_emails"],
		"the evidence records the canonical address, not the typed one")
	assert.Equal(t, []string{"otro@muebleriamsp.mx"}, ev["cliente_emails"],
		"the client's address is canonicalized on its side too")

	require.Equal(t, 1, h.usuarios.calls)
	assert.Equal(t, []string{"ana.laura@muebleriamsp.mx"}, h.usuarios.lastQuery,
		"the identity lookup is probed with the canonical address")
}

// ─── La red de seguridad ────────────────────────────────────────────────────

// TestVendedores_RosterFalla_UsaLoDelCliente pins the requirement that a
// broken roster costs a vendedor list, never a venta.
func TestVendedores_RosterFalla_UsaLoDelCliente(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{err: assert.AnError}
	usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{}}
	h := newVendedoresHarness(t, roster, usuarios)

	in := inputEnCamioneta(vendedorCliente(ana, "ANA@muebleriamsp.mx", "ANA"))
	venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err, "a roster outage must never fail the venta")
	assert.Equal(t, []string{"ana@muebleriamsp.mx"}, emailsDeVenta(venta))
	assert.Zero(t, h.usuarios.calls, "no identity lookup when the roster never answered")

	ev := evidencia(t, h)
	assert.Equal(t, domain.OrigenVendedoresCliente, ev["origen"])
	assert.Equal(t, "roster_fallo", ev["motivo"])
	assert.Equal(t, camionetaDePrueba, ev["camioneta_id"])
}

// TestVendedores_RosterVacio_UsaLoDelCliente pins the "answered, with nobody"
// case — distinct from an outage and recorded as such.
func TestVendedores_RosterVacio_UsaLoDelCliente(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{}}
	h := newVendedoresHarness(t, roster, &fakeUsuarioEmails{})

	in := inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA"))
	venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, []string{"ana@muebleriamsp.mx"}, emailsDeVenta(venta))

	ev := evidencia(t, h)
	assert.Equal(t, domain.OrigenVendedoresCliente, ev["origen"])
	assert.Equal(t, "roster_vacio", ev["motivo"])
}

// TestVendedores_SinUsuarioEnMSP covers a roster that names somebody this API
// has never provisioned: they cannot become a vendedor row, so they are
// reported by address and the rest still win.
func TestVendedores_SinUsuarioEnMSP(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{
			{Email: "ana@muebleriamsp.mx", Nombre: "ANA"},
			{Email: "fantasma@muebleriamsp.mx", Nombre: "FANTASMA"},
		},
	}}
	usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{
		"ana@muebleriamsp.mx": {ID: ana, Nombre: "ANA (BD)"},
	}}
	h := newVendedoresHarness(t, roster, usuarios)

	venta, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, []string{"ana@muebleriamsp.mx"}, emailsDeVenta(venta))

	ev := evidencia(t, h)
	assert.Equal(t, domain.OrigenVendedoresRoster, ev["origen"])
	assert.Equal(t, []string{"fantasma@muebleriamsp.mx"}, ev["emails_sin_usuario"],
		"an unprovisioned person is named, never silently dropped")
}

// TestVendedores_NingunUsuarioEnMSP_UsaLoDelCliente is the degenerate form of
// the previous case: nothing the roster named can be referenced, so the
// client's list stands.
func TestVendedores_NingunUsuarioEnMSP_UsaLoDelCliente(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{{Email: "fantasma@muebleriamsp.mx", Nombre: "F"}},
	}}
	h := newVendedoresHarness(t, roster, &fakeUsuarioEmails{})

	venta, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, []string{"ana@muebleriamsp.mx"}, emailsDeVenta(venta))

	ev := evidencia(t, h)
	assert.Equal(t, domain.OrigenVendedoresCliente, ev["origen"])
	assert.Equal(t, "sin_usuarios", ev["motivo"])
}

// TestVendedores_UsuariosFalla_UsaLoDelCliente separates a broken identity
// lookup from a broken roster: both fall back, and the evidence says which.
func TestVendedores_UsuariosFalla_UsaLoDelCliente(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{{Email: "ana@muebleriamsp.mx", Nombre: "ANA"}},
	}}
	h := newVendedoresHarness(t, roster, &fakeUsuarioEmails{err: assert.AnError})

	venta, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, []string{"ana@muebleriamsp.mx"}, emailsDeVenta(venta))
	assert.Equal(t, "usuarios_fallo", evidencia(t, h)["motivo"])
}

// ─── El tiempo límite ───────────────────────────────────────────────────────

// TestVendedores_ConsultaLlevaDeadline proves the roster call is bounded
// BEFORE it is made, cheaply. Its companion below proves the bound fires.
func TestVendedores_ConsultaLlevaDeadline(t *testing.T) {
	t.Parallel()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{}}
	h := newVendedoresHarness(t, roster, &fakeUsuarioEmails{})

	antes := time.Now()
	_, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(uuid.New(), "ana@muebleriamsp.mx", "ANA")), uuid.New())
	require.NoError(t, err)

	obs := roster.snapshot()
	require.True(t, obs.hadDeadline, "the roster must never be called on an unbounded context")
	assert.WithinRange(t, obs.deadline, antes, antes.Add(5*time.Second),
		"the deadline must be seconds away, not minutes")
}

// TestVendedores_TimeoutNoCuelgaLaVenta lets the roster hang exactly as an
// unreachable Firestore would. The venta must still be created, from the
// client's list, in bounded time.
func TestVendedores_TimeoutNoCuelgaLaVenta(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{blockUntilDone: true}
	h := newVendedoresHarness(t, roster, &fakeUsuarioEmails{})

	antes := time.Now()
	venta, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
	transcurrido := time.Since(antes)

	require.NoError(t, err, "an unresponsive roster must not fail the venta")
	assert.Equal(t, []string{"ana@muebleriamsp.mx"}, emailsDeVenta(venta))
	assert.Less(t, transcurrido, 30*time.Second,
		"the create must return on the resolver's own deadline, not hang")
	assert.Equal(t, "roster_fallo", evidencia(t, h)["motivo"])
}

// ─── De dónde sale la camioneta ─────────────────────────────────────────────

// TestVendedores_SinAlmacenOrigen_NoConsultaElRoster pins that a venta whose
// lines carry no almacén de origen never costs a roster round trip.
//
// Such a venta cannot exist: the domain requires a positive AlmacenOrigen on
// every stand-alone producto and on every combo, so the create is rejected a
// few lines later. That is precisely the point — the resolver runs BEFORE
// that validation, and a malformed request must not reach out to a remote
// roster on its way to being refused. The "camioneta_ausente" branch in the
// resolver is defence for exactly this ordering, not a reachable outcome for
// a venta that gets created.
func TestVendedores_SinAlmacenOrigen_NoConsultaElRoster(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{}
	h := newVendedoresHarness(t, roster, &fakeUsuarioEmails{})

	in := inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA"))
	in.Productos[0].AlmacenOrigen = nil

	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.ErrorIs(t, err, domain.ErrProductoAlmacenOrigenRequerido)

	assert.Zero(t, roster.snapshot().calls, "no camioneta, no query")
	assert.Zero(t, h.ventas.SaveCalls)
	assert.Empty(t, h.outbox.snapshot(), "a venta that was never created leaves no evidence")
}

// TestVendedores_CamionetaAmbigua_NoConsultaElRoster covers lines that
// disagree about the origen. Picking one would be a guess, so the resolver
// declines instead of inventing an answer.
func TestVendedores_CamionetaAmbigua_NoConsultaElRoster(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{}
	h := newVendedoresHarness(t, roster, &fakeUsuarioEmails{})

	in := inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA"))
	otro := 51058
	segundo := in.Productos[0]
	segundo.ID = uuid.New()
	segundo.AlmacenOrigen = &otro
	in.Productos = append(in.Productos, segundo)

	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)

	assert.Zero(t, roster.snapshot().calls, "ambiguous camioneta, no query")
	assert.Equal(t, "camioneta_ambigua", evidencia(t, h)["motivo"])
}

// ─── Sin cablear: comportamiento idéntico al previo ─────────────────────────

// TestVendedores_SinCablear_NoEmiteEvidencia pins that a deployment (or a
// test) without the roster behaves exactly as it did before the feature: the
// client's list is used and the outbox carries only the creation event.
func TestVendedores_SinCablear_NoEmiteEvidencia(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	_, err := h.svc.CrearVenta(t.Context(), validContadoInput(), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, []string{domain.EventTypeVentaCreada}, h.outbox.eventTypes())
}

// ─── La evidencia, campo por campo ──────────────────────────────────────────

// TestVendedores_EvidenciaCompleta checks every key of the outbox payload
// against a hand-computed expectation. A field the code stops emitting, or
// emits under a different name, fails here rather than being noticed months
// later when somebody tries to count fallbacks.
func TestVendedores_EvidenciaCompleta(t *testing.T) {
	t.Parallel()
	ana, beto := uuid.New(), uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{
			{Email: "beto@muebleriamsp.mx", Nombre: "BETO"},
			{Email: "ana@muebleriamsp.mx", Nombre: "ANA"},
			{Email: "fantasma@muebleriamsp.mx", Nombre: "FANTASMA"},
		},
		ExcluidosSinModuloVentas: 2,
	}}
	usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{
		"ana@muebleriamsp.mx":  {ID: ana, Nombre: "ANA (BD)"},
		"beto@muebleriamsp.mx": {ID: beto, Nombre: "BETO (BD)"},
	}}
	h := newVendedoresHarness(t, roster, usuarios)

	by := uuid.New()
	carlos := uuid.New()
	in := inputEnCamioneta(
		vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA"),
		vendedorCliente(carlos, "carlos@muebleriamsp.mx", "CARLOS"),
	)
	venta, err := h.svc.CrearVenta(t.Context(), in, by)
	require.NoError(t, err)

	ev := evidencia(t, h)
	assert.Equal(t, venta.ID().String(), ev["venta_id"])
	assert.Equal(t, by.String(), ev["by"])
	assert.Equal(t, camionetaDePrueba, ev["camioneta_id"])
	assert.Equal(t, domain.OrigenVendedoresRoster, ev["origen"])
	assert.Equal(t, "ok", ev["motivo"])
	assert.Equal(t, []string{
		"ana@muebleriamsp.mx", "beto@muebleriamsp.mx", "fantasma@muebleriamsp.mx",
	}, ev["roster_emails"], "sorted, canonical, deduplicated")
	assert.Equal(t, 3, ev["roster_total"])
	assert.Equal(t, 2, ev["roster_excluidos"])
	assert.Equal(t, []string{"fantasma@muebleriamsp.mx"}, ev["emails_sin_usuario"])
	assert.Equal(t, []string{"ana@muebleriamsp.mx", "carlos@muebleriamsp.mx"}, ev["cliente_emails"])
	assert.Equal(t, 2, ev["cliente_total"])
	assert.Equal(t, []string{"beto@muebleriamsp.mx", "fantasma@muebleriamsp.mx"}, ev["solo_en_roster"])
	assert.Equal(t, []string{"carlos@muebleriamsp.mx"}, ev["solo_en_cliente"])
	assert.Equal(t, false, ev["coinciden"])
	assert.Equal(t, int64(0), ev["duracion_ms"], "measured with the service clock, frozen in tests")

	// And the venta itself carries only the two the roster named AND this API
	// can reference — carlos, who the phone sent, is gone on purpose.
	assert.ElementsMatch(t,
		[]string{"ana@muebleriamsp.mx", "beto@muebleriamsp.mx"}, emailsDeVenta(venta))
}

// TestVendedores_EvidenciaCoincidenCuandoIguales pins the agreement case: the
// same set on both sides reports coinciden=true with empty differences, so a
// dashboard can count disagreements without inspecting the arrays.
func TestVendedores_EvidenciaCoincidenCuandoIguales(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{{Email: "ana@muebleriamsp.mx", Nombre: "ANA"}},
	}}
	usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{
		"ana@muebleriamsp.mx": {ID: ana, Nombre: "ANA (BD)"},
	}}
	h := newVendedoresHarness(t, roster, usuarios)

	_, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(ana, "Ana@MuebleriaMSP.mx", "ANA")), uuid.New())
	require.NoError(t, err)

	ev := evidencia(t, h)
	assert.Equal(t, true, ev["coinciden"],
		"the same person written in two cases is the SAME person")
	assert.Equal(t, []string{}, ev["solo_en_roster"])
	assert.Equal(t, []string{}, ev["solo_en_cliente"])
}

// ─── El nombre ──────────────────────────────────────────────────────────────

// TestVendedores_NombreCaeALaBDCuandoElRosterNoLoTrae keeps the venta's
// vendedor snapshot from ever being built with an empty name, which the
// domain rejects.
func TestVendedores_NombreCaeALaBDCuandoElRosterNoLoTrae(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{{Email: "ana@muebleriamsp.mx", Nombre: ""}},
	}}
	usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{
		"ana@muebleriamsp.mx": {ID: ana, Nombre: "ANA LAURA (BD)"},
	}}
	h := newVendedoresHarness(t, roster, usuarios)

	venta, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
	require.NoError(t, err)
	for vd := range venta.Vendedores() {
		assert.Equal(t, "ANA LAURA (BD)", vd.Snapshot().Nombre())
	}
}

// ─── El límite de tiempo cubre TODO el camino ───────────────────────────────

// usuariosColgados blocks until the context it is handed is done, standing in
// for the identity lookup that cannot answer.
//
// That is not a hypothetical: MSP_USUARIOS is read through the same Firebird
// pool that has wedged before — a cancelled context poisons a connection
// (reference_firebirdsql_ctx_cancel_poisons_pool) and a zombie transaction
// held a DELETE on MSP_PAGOS_CHANGELOG for ~30 h. The fake ignoring ctx
// entirely, as the plain fakeUsuarioEmails does, cannot express that.
type usuariosColgados struct{}

func (usuariosColgados) UsuariosPorEmail(
	ctx context.Context, _ []string,
) (map[string]outbound.UsuarioDeVendedor, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestVendedores_IdentidadColgada_NoCuelgaLaVenta is the companion the roster
// timeout test needed: the SECOND remote call must be inside the same budget.
//
// Bounding only the roster query leaves this one on the request context, and
// CrearVenta then waits forever — the phone's request dies on the client
// timeout with the venta uncreated, which is precisely the outcome the
// deadline exists to make impossible.
func TestVendedores_IdentidadColgada_NoCuelgaLaVenta(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{{Email: "beto@muebleriamsp.mx", Nombre: "BETO"}},
	}}
	h := newVendedoresHarness(t, roster, &fakeUsuarioEmails{})
	h.svc = h.svc.WithVendedoresDeCamioneta(roster, usuariosColgados{})

	// context.Background, not t.Context(): a test context is cancelled when
	// the test ends, which would make an unbounded call look bounded.
	type resultado struct {
		venta *domain.Venta
		err   error
	}
	done := make(chan resultado, 1)
	go func() {
		v, err := h.svc.CrearVenta(context.Background(),
			inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
		done <- resultado{v, err}
	}()

	select {
	case r := <-done:
		require.NoError(t, r.err, "a hung identity lookup must not fail the venta")
		assert.Equal(t, []string{"ana@muebleriamsp.mx"}, emailsDeVenta(r.venta))
		assert.Equal(t, "usuarios_fallo", evidencia(t, h)["motivo"])
	case <-time.After(10 * time.Second):
		t.Fatal("CrearVenta hung: the deadline does not cover the identity lookup")
	}
}

// ─── Un pánico del adaptador no se lleva la venta ───────────────────────────

// rosterQuePanica / usuariosQuePanican stand in for an adapter defect: a nil
// map, a bad type assertion inside an SDK, a client used after Close.
type rosterQuePanica struct{}

func (rosterQuePanica) VendedoresDeCamioneta(
	context.Context, int,
) (outbound.VendedoresDeCamioneta, error) {
	panic("adaptador del roster reventado")
}

type usuariosQuePanican struct{}

func (usuariosQuePanican) UsuariosPorEmail(
	context.Context, []string,
) (map[string]outbound.UsuarioDeVendedor, error) {
	panic("adaptador de usuarios reventado")
}

// TestVendedores_PanicoDelRoster_NoRompeLaVenta and its usuarios twin pin the
// eighth way this could have cost a sale. An unrecovered panic in either
// adapter unwinds CrearVenta and answers 500 — Firestore preventing a venta,
// which is the one thing the safety net is not allowed to permit. The
// evidence records it as its own motivo so a panicking adapter is countable
// instead of showing up as a mysterious drop in roster usage.
func TestVendedores_PanicoDelRoster_NoRompeLaVenta(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	h := newVendedoresHarness(t, &fakeRoster{}, &fakeUsuarioEmails{})
	h.svc = h.svc.WithVendedoresDeCamioneta(rosterQuePanica{}, &fakeUsuarioEmails{})

	venta, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
	require.NoError(t, err, "a panicking roster must not fail the venta")
	assert.Equal(t, []string{"ana@muebleriamsp.mx"}, emailsDeVenta(venta))
	assert.Equal(t, "resolucion_panico", evidencia(t, h)["motivo"])
}

func TestVendedores_PanicoDeUsuarios_NoRompeLaVenta(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{{Email: "beto@muebleriamsp.mx", Nombre: "BETO"}},
	}}
	h := newVendedoresHarness(t, roster, &fakeUsuarioEmails{})
	h.svc = h.svc.WithVendedoresDeCamioneta(roster, usuariosQuePanican{})

	venta, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
	require.NoError(t, err, "a panicking identity lookup must not fail the venta")
	assert.Equal(t, []string{"ana@muebleriamsp.mx"}, emailsDeVenta(venta))
	assert.Equal(t, "resolucion_panico", evidencia(t, h)["motivo"])
}

// ─── El contenido del roster no puede rechazar una venta ────────────────────

// TestVendedores_NombreImposibleDelRoster_NoRompeLaVenta walks the values a
// hand-typed Firestore document can hold that the venta's own domain
// REFUSES: over 200 characters, a NUL, a control character, nothing at all.
//
// Before the sanitizer these did not degrade — they travelled into
// domain.CrearVenta and rejected the venta with vendedor_nombre_too_long or
// string_unsafe_chars, i.e. a typo in a roster document stopping a sale in a
// customer's living room. The vendedor must survive with a usable name, since
// dropping him silently is the very defect this feature exists to end.
func TestVendedores_NombreImposibleDelRoster_NoRompeLaVenta(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre string
		quiere string
		desc   string
	}{
		{strings.Repeat("A", 300), strings.Repeat("A", 200), "300 caracteres se truncan a 200"},
		{"BETO\x00SUAREZ", "BETOSUAREZ", "el NUL se elimina"},
		{"BETO\x07SUAREZ", "BETOSUAREZ", "el control char se elimina"},
		{"BETO\tSUAREZ", "BETO SUAREZ", "el tab se vuelve espacio"},
		{"   ", "BETO (BD)", "en blanco cae al nombre de MSP_USUARIOS"},
		{"\x01\x02", "BETO (BD)", "solo control chars cae al nombre de MSP_USUARIOS"},
	}
	for _, c := range casos {
		t.Run(c.desc, func(t *testing.T) {
			t.Parallel()
			ana, beto := uuid.New(), uuid.New()
			roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
				Vendedores: []outbound.VendedorDeCamioneta{
					{Email: "beto@muebleriamsp.mx", Nombre: c.nombre},
				},
			}}
			usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{
				"beto@muebleriamsp.mx": {ID: beto, Nombre: "BETO (BD)"},
			}}
			h := newVendedoresHarness(t, roster, usuarios)

			venta, err := h.svc.CrearVenta(t.Context(),
				inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
			require.NoError(t, err, "a roster document must never reject a venta")
			assert.Equal(t, []string{"beto@muebleriamsp.mx"}, emailsDeVenta(venta),
				"the roster still wins; the name degrades, the person does not")
			for vd := range venta.Vendedores() {
				assert.Equal(t, c.quiere, vd.Snapshot().Nombre())
			}
		})
	}
}

// TestVendedores_SinNombreEnNingunLado_UsaElCorreo covers the corner where
// neither the roster nor MSP_USUARIOS has a usable name. The venta still
// carries the person, named by his own address — the domain requires SOME
// name and an unnamed vendedor is not a reason to refuse a sale.
func TestVendedores_SinNombreEnNingunLado_UsaElCorreo(t *testing.T) {
	t.Parallel()
	ana, beto := uuid.New(), uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{{Email: "beto@muebleriamsp.mx", Nombre: ""}},
	}}
	usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{
		"beto@muebleriamsp.mx": {ID: beto, Nombre: "   "},
	}}
	h := newVendedoresHarness(t, roster, usuarios)

	venta, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
	require.NoError(t, err)
	for vd := range venta.Vendedores() {
		assert.Equal(t, "beto@muebleriamsp.mx", vd.Snapshot().Nombre())
	}
}

// ─── El roster no puede exceder lo que Microsip acepta ──────────────────────

// TestVendedores_RosterDeCuatro_UsaLoDelCliente pins the bound that keeps a
// created venta APPLIABLE.
//
// AplicarVenta maps the seller count through MSP_CFG_NUM_VENDEDORES, which
// holds rows for 1, 2 and 3 only, and fills exactly three Microsip columns.
// A venta created with four vendedores is accepted and then fails forever at
// aplicar with num_vendedores_sin_mapeo — discovered hours later, fixable
// only by hand. Since the largest camioneta in the live roster already has
// three people, one new assignment in Firestore is all it would take.
func TestVendedores_RosterDeCuatro_UsaLoDelCliente(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	known := map[string]outbound.UsuarioDeVendedor{}
	var rs []outbound.VendedorDeCamioneta
	for _, e := range []string{"a@msp.com", "b@msp.com", "c@msp.com", "d@msp.com"} {
		known[e] = outbound.UsuarioDeVendedor{ID: uuid.New(), Nombre: strings.ToUpper(e)}
		rs = append(rs, outbound.VendedorDeCamioneta{Email: e, Nombre: strings.ToUpper(e)})
	}
	h := newVendedoresHarness(t,
		&fakeRoster{res: outbound.VendedoresDeCamioneta{Vendedores: rs}},
		&fakeUsuarioEmails{known: known})

	venta, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
	require.NoError(t, err)

	assert.Equal(t, []string{"ana@muebleriamsp.mx"}, emailsDeVenta(venta))
	ev := evidencia(t, h)
	assert.Equal(t, "roster_excede_slots", ev["motivo"])
	assert.Equal(t, domain.OrigenVendedoresCliente, ev["origen"])
	assert.Len(t, ev["roster_emails"], 4,
		"the refused roster stays on the evidence, or the condition is invisible")
}

// TestVendedores_RosterDeTres_Gana is the other side of that bound: three is
// the largest roster Microsip accepts, and it must still win. Without this
// an off-by-one in maxVendedoresDeRoster would silently disable the feature
// for the busiest camionetas and every test above would stay green.
func TestVendedores_RosterDeTres_Gana(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	known := map[string]outbound.UsuarioDeVendedor{}
	var rs []outbound.VendedorDeCamioneta
	for _, e := range []string{"a@msp.com", "b@msp.com", "c@msp.com"} {
		known[e] = outbound.UsuarioDeVendedor{ID: uuid.New(), Nombre: strings.ToUpper(e)}
		rs = append(rs, outbound.VendedorDeCamioneta{Email: e, Nombre: strings.ToUpper(e)})
	}
	h := newVendedoresHarness(t,
		&fakeRoster{res: outbound.VendedoresDeCamioneta{Vendedores: rs}},
		&fakeUsuarioEmails{known: known})

	venta, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a@msp.com", "b@msp.com", "c@msp.com"}, emailsDeVenta(venta))
	assert.Equal(t, "ok", evidencia(t, h)["motivo"])
}

// ─── La camioneta cuando la venta trae combos ───────────────────────────────

// TestVendedores_CamionetaDeUnCombo covers the shape the derivation could
// have missed: a venta whose only line is a COMBO. Combo children carry no
// almacén of their own (they inherit the parent's), so a derivation that only
// looked at productos would find no origen and never consult the roster —
// silently disabling the feature for every venta sold as a set.
func TestVendedores_CamionetaDeUnCombo(t *testing.T) {
	t.Parallel()
	ana, beto := uuid.New(), uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores: []outbound.VendedorDeCamioneta{{Email: "beto@muebleriamsp.mx", Nombre: "BETO"}},
	}}
	usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{
		"beto@muebleriamsp.mx": {ID: beto, Nombre: "BETO (BD)"},
	}}
	h := newVendedoresHarness(t, roster, usuarios)

	in, _ := validContadoInputConCombos(t)
	in.Vendedores = []ventasapp.CrearVentaVendedorInput{
		vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA"),
	}
	// Drop the stand-alone producto so the ONLY origen in the venta is the
	// combo's.
	soloHijos := make([]ventasapp.CrearVentaProductoInput, 0, len(in.Productos))
	for _, p := range in.Productos {
		if p.ComboID != nil {
			soloHijos = append(soloHijos, p)
		}
	}
	in.Productos = soloHijos
	in.Combos[0].AlmacenOrigen = camionetaDePrueba

	venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)

	assert.Equal(t, camionetaDePrueba, roster.snapshot().camioneta,
		"the camioneta of a combo-only venta is the combo's almacén de origen")
	assert.Equal(t, []string{"beto@muebleriamsp.mx"}, emailsDeVenta(venta))
}

// TestVendedores_ComboYProductoEnDistintoAlmacen_NoConsultaElRoster pins that
// the ambiguity check spans BOTH collections: a combo shipping from one
// almacén and a producto from another is two camionetas, and asking about
// either would be a coin flip.
func TestVendedores_ComboYProductoEnDistintoAlmacen_NoConsultaElRoster(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{}
	h := newVendedoresHarness(t, roster, &fakeUsuarioEmails{})

	in, _ := validContadoInputConCombos(t)
	in.Vendedores = []ventasapp.CrearVentaVendedorInput{
		vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA"),
	}
	in.Combos[0].AlmacenOrigen = camionetaDePrueba
	otro := 51058
	for i := range in.Productos {
		if in.Productos[i].ComboID == nil {
			in.Productos[i].AlmacenOrigen = &otro
		}
	}

	_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)
	assert.Zero(t, roster.snapshot().calls, "combo and producto disagree: no query")
	assert.Equal(t, "camioneta_ambigua", evidencia(t, h)["motivo"])
}

// ─── Almacén 19: el roster de un almacén que NO es camioneta ────────────────

// TestVendedores_Almacen19_ElRosterDeOficinaGanaIgual is a CHARACTERIZATION
// test, not an endorsement. It records what the system does today when a
// venta's almacén de origen is 19 (ALMACEN GENERAL, which is not a camioneta
// and has three office people assigned in the live roster): the three of them
// replace whoever the phone said sold, because "the roster wins" is applied
// to every almacén without distinction.
//
// Measured on 2026-08-27: camioneta 19 → noe@gmail.com, gabriel.roque@msp.com,
// lisded@gmail.com. Nothing in the dev data shows a venta originating from
// almacén 19, so this is a latent path, not an active one. If the owner
// decides office almacenes must not answer for a venta, the fix belongs in
// camionetaDeVenta and this test is the one that will say so out loud.
func TestVendedores_Almacen19_ElRosterDeOficinaGanaIgual(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	noe, gabriel, lisded := uuid.New(), uuid.New(), uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{Vendedores: []outbound.VendedorDeCamioneta{
		{Email: "noe@gmail.com", Nombre: "NOE CORTERO"},
		{Email: "gabriel.roque@msp.com", Nombre: "GABRIEL"},
		{Email: "lisded@gmail.com", Nombre: "LISDED MARTINEZ HERNANDEZ"},
	}}}
	usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{
		"noe@gmail.com":         {ID: noe, Nombre: "NOE"},
		"gabriel.roque@msp.com": {ID: gabriel, Nombre: "GABRIEL"},
		"lisded@gmail.com":      {ID: lisded, Nombre: "LISDED"},
	}}
	h := newVendedoresHarness(t, roster, usuarios)

	in := inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA"))
	almacenGeneral := 19
	in.Productos[0].AlmacenOrigen = &almacenGeneral

	venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)

	assert.Equal(t, 19, roster.snapshot().camioneta)
	assert.ElementsMatch(t,
		[]string{"noe@gmail.com", "gabriel.roque@msp.com", "lisded@gmail.com"},
		emailsDeVenta(venta),
		"today the office roster replaces the seller the phone named")
	// The person the phone named is recoverable from the evidence, which is
	// the only reason this is survivable.
	assert.Equal(t, []string{"ana@muebleriamsp.mx"}, evidencia(t, h)["solo_en_cliente"])
}

// ─── El orden decide quién es el vendedor 1 en Microsip ─────────────────────

// TestVendedores_ElOrdenDelClienteMandaEnLosQueCoinciden pins that resolving
// server-side does not quietly reassign the PRIMARY seller.
//
// The slice index becomes MSP_VENTAS_VENDEDORES.POSICION, and AplicarVenta
// maps position k to LIBRES_CARGOS_CC.VENDEDOR_1/2/3 — so order is
// attribution, not presentation. The roster's own order is Firestore's, which
// with no explicit OrderBy is document-id order, i.e. Firebase uids: taking it
// verbatim lets a uid decide whom Microsip records as the first seller of
// every venta from that camioneta.
func TestVendedores_ElOrdenDelClienteMandaEnLosQueCoinciden(t *testing.T) {
	t.Parallel()
	ana, beto, carla := uuid.New(), uuid.New(), uuid.New()
	// The roster answers in an order the phone did not choose — carla first.
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{Vendedores: []outbound.VendedorDeCamioneta{
		{Email: "carla@muebleriamsp.mx", Nombre: "CARLA"},
		{Email: "beto@muebleriamsp.mx", Nombre: "BETO"},
		{Email: "ana@muebleriamsp.mx", Nombre: "ANA"},
	}}}
	usuarios := &fakeUsuarioEmails{known: map[string]outbound.UsuarioDeVendedor{
		"ana@muebleriamsp.mx":   {ID: ana, Nombre: "ANA (BD)"},
		"beto@muebleriamsp.mx":  {ID: beto, Nombre: "BETO (BD)"},
		"carla@muebleriamsp.mx": {ID: carla, Nombre: "CARLA (BD)"},
	}}
	h := newVendedoresHarness(t, roster, usuarios)

	// The phone said Ana sold it, with Beto second.
	venta, err := h.svc.CrearVenta(t.Context(), inputEnCamioneta(
		vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA"),
		vendedorCliente(beto, "beto@muebleriamsp.mx", "BETO"),
	), uuid.New())
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"ana@muebleriamsp.mx", "beto@muebleriamsp.mx", "carla@muebleriamsp.mx"},
		emailsDeVenta(venta),
		"the phone's sellers keep the phone's positions; the roster's extra rides last")
}

// TestVendedores_ExcluidosPorModuloSeCuentanAunqueElRosterQuedeVacio covers
// the shape an over-eager MODULOS filter takes: everybody matched the
// camioneta and everybody was dropped. The outcome is safe (the client's list
// wins) but it is indistinguishable from "nobody rides this truck" unless the
// exclusion count rides on the evidence — which is the only way anyone would
// ever notice the filter had gone wrong.
func TestVendedores_ExcluidosPorModuloSeCuentanAunqueElRosterQuedeVacio(t *testing.T) {
	t.Parallel()
	ana := uuid.New()
	roster := &fakeRoster{res: outbound.VendedoresDeCamioneta{
		Vendedores:               nil,
		ExcluidosSinModuloVentas: 3,
	}}
	h := newVendedoresHarness(t, roster, &fakeUsuarioEmails{})

	venta, err := h.svc.CrearVenta(t.Context(),
		inputEnCamioneta(vendedorCliente(ana, "ana@muebleriamsp.mx", "ANA")), uuid.New())
	require.NoError(t, err)

	assert.Equal(t, []string{"ana@muebleriamsp.mx"}, emailsDeVenta(venta))
	ev := evidencia(t, h)
	assert.Equal(t, "roster_vacio", ev["motivo"])
	assert.Equal(t, 3, ev["roster_excluidos"],
		"an empty roster and a fully-filtered one must not look the same")
}
