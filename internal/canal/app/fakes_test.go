package app_test

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	canalapp "github.com/abdimuy/msp-api/internal/canal/app"
	"github.com/abdimuy/msp-api/internal/canal/domain"
	canaloutbound "github.com/abdimuy/msp-api/internal/canal/ports/outbound"
)

// Compile-time assertions that the fakes satisfy the ports they stand in
// for — catches a signature drift against the real ports immediately,
// rather than as a confusing failure deep in a test.
var (
	_ canaloutbound.BuzonRepo = (*buzonRepoFake)(nil)
	_ canaloutbound.Forwarder = (*forwarderFake)(nil)
	_ canaloutbound.Clock     = (*fixedClock)(nil)
	_ canalapp.TxRunner       = (*txFake)(nil)
)

// errNoEncontrado stands in for a repo miss — should never happen against
// these fakes, since the service always looks up ids it just handed the
// repo, but kept explicit rather than panicking so a broken test fails with
// a readable error instead of a nil-pointer dereference.
var errNoEncontrado = errors.New("mensaje no encontrado")

// mensajeParams builds a valid NewMensajeEntranteParams for wamid, so tests
// read as "the message with this wamid" rather than repeating every field.
func mensajeParams(wamid string, recibidoEn time.Time) domain.NewMensajeEntranteParams {
	return domain.NewMensajeEntranteParams{
		Wamid:         wamid,
		Remitente:     "5215500000000",
		PhoneNumberID: "1234567890",
		Tipo:          "text",
		Contenido:     "hola",
		TimestampMeta: recibidoEn,
		RecibidoEn:    recibidoEn,
	}
}

// ── clock ────────────────────────────────────────────────────────────────

// fixedClock is a Clock that returns a value the test controls, so every
// assertion about a timestamp is exact instead of approximate.
type fixedClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFixedClock(t time.Time) *fixedClock {
	return &fixedClock{t: t}
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fixedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// ── BuzonRepo ────────────────────────────────────────────────────────────

// buzonRepoFake is an in-memory outbound.BuzonRepo, keyed by Wamid the same
// way the real mailbox is: Guardar is idempotent on Wamid, exactly the
// invariant the receive-path tests exist to prove.
type buzonRepoFake struct {
	mu sync.Mutex

	byID    map[uuid.UUID]*domain.MensajeEntrante
	byWamid map[string]uuid.UUID

	fallarEn  string // method name that should fail on next call, or "".
	fallarErr error

	guardarLlamadas         int
	listarPendientesCount   int
	ultimoLimiteVisto       int
	marcarReenviadoLlamadas int
	marcarFallidoLlamadas   int
}

func newBuzonRepoFake() *buzonRepoFake {
	return &buzonRepoFake{
		byID:    make(map[uuid.UUID]*domain.MensajeEntrante),
		byWamid: make(map[string]uuid.UUID),
	}
}

// fallarCon makes the named method fail with err on every subsequent call.
func (r *buzonRepoFake) fallarCon(metodo string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallarEn = metodo
	r.fallarErr = err
}

func (r *buzonRepoFake) falla(metodo string) error {
	if r.fallarEn == metodo {
		return r.fallarErr
	}
	return nil
}

func (r *buzonRepoFake) Guardar(_ context.Context, m *domain.MensajeEntrante) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.guardarLlamadas++
	if err := r.falla("Guardar"); err != nil {
		return false, err
	}
	if _, exists := r.byWamid[m.Wamid()]; exists {
		return false, nil
	}
	r.byWamid[m.Wamid()] = m.ID()
	r.byID[m.ID()] = m
	return true, nil
}

func (r *buzonRepoFake) ListarPendientes(_ context.Context, limite int) ([]*domain.MensajeEntrante, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listarPendientesCount++
	r.ultimoLimiteVisto = limite
	if err := r.falla("ListarPendientes"); err != nil {
		return nil, err
	}
	var out []*domain.MensajeEntrante
	for _, m := range r.byID {
		if m.Estado() == domain.EstadoReenvioPendiente {
			out = append(out, m)
			if len(out) >= limite {
				break
			}
		}
	}
	return out, nil
}

// rehydrateEstado replaces the stored row for id with a copy carrying
// estado/motivo/now, WITHOUT going through the domain entity's own
// transition methods. This mirrors what a real repository does: the
// service already validated and applied the transition on its in-memory
// domain.MensajeEntrante before calling MarcarReenviado/MarcarFallido, so
// persistence here is a blind write of the given final state (an
// UPDATE ... SET estado=?, ... WHERE id=?, in a real SQL repo), never a
// second call into the entity's own (already-consumed) transition guard.
func (r *buzonRepoFake) rehydrateEstado(id uuid.UUID, estado domain.EstadoReenvio, motivo string, now time.Time) {
	m := r.byID[id]
	r.byID[id] = domain.RehydrateMensajeEntrante(domain.RehydrateMensajeEntranteParams{
		ID:            m.ID(),
		Wamid:         m.Wamid(),
		Remitente:     m.Remitente(),
		PhoneNumberID: m.PhoneNumberID(),
		Tipo:          m.Tipo(),
		Contenido:     m.Contenido(),
		TimestampMeta: m.TimestampMeta(),
		RecibidoEn:    m.RecibidoEn(),
		Estado:        estado,
		MotivoFallo:   motivo,
		CreatedAt:     m.CreatedAt(),
		UpdatedAt:     now,
	})
}

func (r *buzonRepoFake) MarcarReenviado(_ context.Context, id uuid.UUID, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.marcarReenviadoLlamadas++
	if err := r.falla("MarcarReenviado"); err != nil {
		return err
	}
	m, ok := r.byID[id]
	if !ok {
		return errNoEncontrado
	}
	r.rehydrateEstado(id, domain.EstadoReenvioReenviado, m.MotivoFallo(), now)
	return nil
}

func (r *buzonRepoFake) MarcarFallido(_ context.Context, id uuid.UUID, motivo string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.marcarFallidoLlamadas++
	if err := r.falla("MarcarFallido"); err != nil {
		return err
	}
	_, ok := r.byID[id]
	if !ok {
		return errNoEncontrado
	}
	r.rehydrateEstado(id, domain.EstadoReenvioFallido, motivo, now)
	return nil
}

func (r *buzonRepoFake) ContarPendientes(_ context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.falla("ContarPendientes"); err != nil {
		return 0, err
	}
	n := 0
	for _, m := range r.byID {
		if m.Estado() == domain.EstadoReenvioPendiente {
			n++
		}
	}
	return n, nil
}

// listarPendientesLlamadas returns how many times ListarPendientes was
// called — a tick fires this every time, regardless of whether the mailbox
// held anything to forward, which makes it the reliable "did a tick
// actually run" signal for the worker's lifecycle tests.
func (r *buzonRepoFake) listarPendientesLlamadas() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listarPendientesCount
}

// ultimoLimite returns the limite argument ListarPendientes last saw, so a
// test can prove the worker's Batch default actually reached the repo.
func (r *buzonRepoFake) ultimoLimite() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ultimoLimiteVisto
}

// estado returns the persisted estado for id, and whether the row exists.
func (r *buzonRepoFake) estado(id uuid.UUID) (domain.EstadoReenvio, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.byID[id]
	if !ok {
		return "", false
	}
	return m.Estado(), true
}

// motivoFallo returns the persisted failure reason for id.
func (r *buzonRepoFake) motivoFallo(id uuid.UUID) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.byID[id]
	if !ok {
		return ""
	}
	return m.MotivoFallo()
}

func (r *buzonRepoFake) countByWamid(wamid string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byWamid[wamid]; ok {
		return 1
	}
	return 0
}

// ── Forwarder ────────────────────────────────────────────────────────────

// forwarderFake serves a scripted sequence of outcomes per call, in order.
// Once the script is exhausted, the last outcome repeats — mirroring
// flota's rosterFake so a worker ticking freely does not run off the end.
type forwarderFake struct {
	mu sync.Mutex

	outcomes         []error
	idx              int
	llamadas         int
	llamadasPorWamid map[string]int
}

func newForwarderFake(outcomes ...error) *forwarderFake {
	return &forwarderFake{outcomes: outcomes, llamadasPorWamid: make(map[string]int)}
}

func (f *forwarderFake) Reenviar(_ context.Context, m *domain.MensajeEntrante) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.llamadas++
	f.llamadasPorWamid[m.Wamid()]++
	if len(f.outcomes) == 0 {
		return nil
	}
	outcome := f.outcomes[min(f.idx, len(f.outcomes)-1)]
	f.idx++
	return outcome
}

func (f *forwarderFake) llamadasTotales() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.llamadas
}

func (f *forwarderFake) llamadasPara(wamid string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.llamadasPorWamid[wamid]
}

// ── TxRunner ─────────────────────────────────────────────────────────────

// txFake stands in for canalapp.TxRunner. It actually invokes fn (so tests
// using it exercise the exact same code path a real transaction manager
// would drive), counts how many times it ran, and can be made to fail
// before ever calling fn — simulating a transaction that could not even
// begin (e.g. the database is locked).
type txFake struct {
	mu        sync.Mutex
	ejecutada int
	fallarCon error
}

func (tx *txFake) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx.mu.Lock()
	tx.ejecutada++
	fallar := tx.fallarCon
	tx.mu.Unlock()
	if fallar != nil {
		return fallar
	}
	return fn(ctx)
}

func (tx *txFake) veces() int {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	return tx.ejecutada
}
