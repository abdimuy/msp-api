package app_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

type fakeTxRunner struct {
	mu         sync.Mutex
	inTx       bool
	calls      int
	commits    int
	rollbacks  int
	errOnRun   error
	onCommit   func()
	onRollback func()
}

func (f *fakeTxRunner) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	f.mu.Lock()
	f.calls++
	f.inTx = true
	errOnRun := f.errOnRun
	f.mu.Unlock()
	if errOnRun != nil {
		f.mu.Lock()
		f.inTx = false
		f.rollbacks++
		onRoll := f.onRollback
		f.mu.Unlock()
		if onRoll != nil {
			onRoll()
		}
		return errOnRun
	}
	err := fn(ctx)
	f.mu.Lock()
	f.inTx = false
	if err != nil {
		f.rollbacks++
		onRoll := f.onRollback
		f.mu.Unlock()
		if onRoll != nil {
			onRoll()
		}
	} else {
		f.commits++
		onComm := f.onCommit
		f.mu.Unlock()
		if onComm != nil {
			onComm()
		}
	}
	return err
}

// enTx reports whether a transaction is currently open.
func (f *fakeTxRunner) enTx() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inTx
}

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	if f.now.IsZero() {
		return time.Now().UTC()
	}
	return f.now
}

type fakeIDGen struct {
	ids []uuid.UUID
	i   int
	mu  sync.Mutex
}

func (f *fakeIDGen) Nuevo() uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.i < len(f.ids) {
		id := f.ids[f.i]
		f.i++
		return id
	}
	return uuid.New()
}

type fakeIdentity struct {
	usuario outbound.Usuario
	denegar *domain.Permiso
	errUser error
	errPerm error
}

func (f *fakeIdentity) UsuarioActual(ctx context.Context) (outbound.Usuario, error) {
	if f.errUser != nil {
		return outbound.Usuario{}, f.errUser
	}
	return f.usuario, nil
}

func (f *fakeIdentity) TienePermiso(ctx context.Context, p domain.Permiso) (bool, error) {
	if f.errPerm != nil {
		return false, f.errPerm
	}
	if f.denegar != nil && *f.denegar == p {
		return false, nil
	}
	return true, nil
}

// clave yields a valid UUID idempotency key, the only shape the commands
// accept since the domain stores the canonical form.
func clave() string {
	return uuid.New().String()
}

// registrador records the order of the repo calls the order test cares about.
// It is shared by the folio repo (obtener_para_actualizar) and the event repo
// (buscar_clave), so the test can assert the lock comes first no matter which
// fake the runtime call reaches.
type registrador struct {
	mu   sync.Mutex
	paso []string
}

func (r *registrador) registra(p string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paso = append(r.paso, p)
}

func (r *registrador) pasos() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.paso))
	copy(out, r.paso)
	return out
}

// primerPaso returns the index of the first occurrence of paso, or -1.
func primerPaso(r *registrador, paso string) int {
	for i, p := range r.pasos() {
		if p == paso {
			return i
		}
	}
	return -1
}

// seedFolio builds a real aggregate with the domain and stores it in the fake
// repo, so the commands under test read a genuine entity rather than a stub.
func seedFolio(t *testing.T, repo *fakeGarantiaRepo, now time.Time, nArts int) *domain.Garantia {
	t.Helper()
	folio, err := domain.NewFolio(2001 + repo.seeded)
	repo.seeded++
	if err != nil {
		t.Fatalf("NewFolio: %v", err)
	}
	g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
		Folio:       folio,
		Origen:      domain.OrigenFolioPiso,
		Description: "silla de oficina desarmada",
		AbiertoPor:  "Juan",
		Now:         now,
		Actor: domain.ActorParams{
			Usuario:           "Juan",
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   now,
		},
	})
	if err != nil {
		t.Fatalf("AbrirGarantia: %v", err)
	}
	for i := range nArts {
		artClave := string(rune('A' + i))
		if err := g.AgregarArticulo(domain.AgregarArticuloParams{
			Clave:       artClave,
			Description: "articulo " + artClave,
		}, domain.ActorParams{
			Usuario:           "Juan",
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   now,
		}, now); err != nil {
			t.Fatalf("AgregarArticulo: %v", err)
		}
	}
	repo.obtained[g.ID()] = g
	repo.forUpdate[g.ID()] = g
	repo.byFolio[g.Folio()] = g
	return g
}

// firstArticuloID returns the id of the first article of the folio.
func firstArticuloID(t *testing.T, g *domain.Garantia) uuid.UUID {
	t.Helper()
	for a := range g.Articulos() {
		return a.ID()
	}
	t.Fatal("folio sin articulos")
	return uuid.Nil
}

// secondArticuloID returns the second article of the folio, for the mismatch
// tests where the stored event names a different article than the retry.
func secondArticuloID(t *testing.T, g *domain.Garantia) uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	for a := range g.Articulos() {
		ids = append(ids, a.ID())
	}
	if len(ids) < 2 {
		t.Fatalf("el folio tiene %d articulos, want >= 2", len(ids))
	}
	return ids[1]
}

// hydrateEvento builds a persisted-shape event for the idempotency fake. The
// key is stored as given — tests pass the canonical form, matching what the
// real repository persists.
func hydrateEvento(garantiaID uuid.UUID, clave string, tipo domain.TipoEvento, now time.Time) *domain.Evento {
	return domain.HydrateEvento(domain.HydrateEventoParams{
		ID:                uuid.New(),
		GarantiaID:        garantiaID,
		Tipo:              tipo,
		Usuario:           "Juan",
		CreatedAt:         now,
		DeviceCreatedAt:   now,
		ClaveIdempotencia: clave,
	})
}

// hydrateEventoDeArticulo is hydrateEvento for the stage-bearing commands: it
// also fills articuloRef and etapaHasta, which is what the real write leaves
// behind and what the content check compares. An evento seeded without them
// is the mutilated shape the review called out: it does not look like what
// the first execution actually stored, so it cannot stand in for a replay.
func hydrateEventoDeArticulo(
	garantiaID uuid.UUID,
	clave string,
	tipo domain.TipoEvento,
	articuloRef uuid.UUID,
	etapaHasta domain.Etapa,
	now time.Time,
) *domain.Evento {
	return domain.HydrateEvento(domain.HydrateEventoParams{
		ID:                uuid.New(),
		GarantiaID:        garantiaID,
		ArticuloRef:       &articuloRef,
		Tipo:              tipo,
		EtapaHasta:        &etapaHasta,
		Usuario:           "Juan",
		CreatedAt:         now,
		DeviceCreatedAt:   now,
		ClaveIdempotencia: clave,
	})
}

// eventoComoElPrimero builds the event a successful first execution left
// behind, taken from the raw aggregate the command handed to Guardar. This is
// the shape a real retry meets in the store — not a hand-written stand-in.
func eventoComoElPrimero(t *testing.T, g *domain.Garantia) *domain.Evento {
	t.Helper()
	return unicoEventoPendiente(t, g)
}

// unicoEventoPendiente returns the single pending event of the raw aggregate a
// command handed to Crear. Pending events only live on that raw aggregate:
// the clones and the re-read answers do not carry them.
func unicoEventoPendiente(t *testing.T, g *domain.Garantia) *domain.Evento {
	t.Helper()
	if g == nil {
		t.Fatal("el repo no recibio ningun agregado")
	}
	var evs []*domain.Evento
	for ev := range g.EventosPendientes() {
		evs = append(evs, ev)
	}
	if len(evs) != 1 {
		t.Fatalf("eventos pendientes = %d, want 1", len(evs))
	}
	return evs[0]
}

// uuidAleatorio returns an id no folio will ever carry.
func uuidAleatorio() uuid.UUID {
	return uuid.New()
}

// uppercaseUUID returns the key in the other canonical-braced-free spelling's
// case, to exercise the normalization path of a resend.
func uppercaseUUID(k string) string {
	return strings.ToUpper(k)
}

// usuarioEventos asserts that every pending event of the aggregate was
// recorded under the caller Identity gave us. The aggregate is the raw one the
// fake received in Guardar, where the pending events of the mutation live.
func usuarioEventos(t *testing.T, g *domain.Garantia, nombre string) {
	t.Helper()
	if g == nil {
		t.Fatal("el repo no recibio ningun agregado")
	}
	n := 0
	for ev := range g.EventosPendientes() {
		n++
		if ev.Usuario() != nombre {
			t.Errorf("evento %q con usuario %q, want %q the one from Identity", ev.Tipo(), ev.Usuario(), nombre)
		}
	}
	if n == 0 {
		t.Error("el agregado no trae ningun evento pendiente")
	}
}
