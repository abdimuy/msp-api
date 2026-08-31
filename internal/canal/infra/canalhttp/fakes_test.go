package canalhttp_test

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/canal/domain"
	canaloutbound "github.com/abdimuy/msp-api/internal/canal/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/whatsapp"
)

// errRepoBoom is injected into buzonRepoFake to simulate a repository
// failure in tests that need one.
var errRepoBoom = errors.New("boom: repositorio no disponible")

// Compile-time assertions that the fakes satisfy the ports/interfaces they
// stand in for.
var (
	_ canaloutbound.BuzonRepo = (*buzonRepoFake)(nil)
	_ canaloutbound.Forwarder = (*forwarderFake)(nil)
	_ canaloutbound.Clock     = (*fixedClock)(nil)
	_ whatsapp.Client         = (*waFake)(nil)
)

// ── Clock ────────────────────────────────────────────────────────────────

// fixedClock is a Clock that always returns the value it was built with, so
// RecibidoEn assertions in tests are exact.
type fixedClock struct {
	t time.Time
}

func newFixedClock(t time.Time) *fixedClock { return &fixedClock{t: t} }

func (c *fixedClock) Now() time.Time { return c.t }

// ── BuzonRepo ────────────────────────────────────────────────────────────

// buzonRepoFake is an in-memory outbound.BuzonRepo, keyed by Wamid the same
// way the real mailbox is — Guardar is idempotent on Wamid, which is exactly
// what the webhook's "same wamid twice" test needs to prove end to end.
// Mirrors internal/canal/app/fakes_test.go's buzonRepoFake, trimmed to what
// this package's tests actually exercise.
type buzonRepoFake struct {
	mu sync.Mutex

	byID    map[uuid.UUID]*domain.MensajeEntrante
	byWamid map[string]uuid.UUID

	guardarLlamadas       int
	contarPendientesFalla error
}

func newBuzonRepoFake() *buzonRepoFake {
	return &buzonRepoFake{
		byID:    make(map[uuid.UUID]*domain.MensajeEntrante),
		byWamid: make(map[string]uuid.UUID),
	}
}

func (r *buzonRepoFake) Guardar(_ context.Context, m *domain.MensajeEntrante) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.guardarLlamadas++
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
	var out []*domain.MensajeEntrante
	for _, m := range r.byID {
		if m.Estado() == domain.EstadoReenvioPendiente {
			out = append(out, m)
		}
	}
	if len(out) > limite {
		out = out[:limite]
	}
	return out, nil
}

func (r *buzonRepoFake) MarcarReenviado(_ context.Context, _ uuid.UUID, _ time.Time) error {
	return nil
}

func (r *buzonRepoFake) MarcarFallido(_ context.Context, _ uuid.UUID, _ string, _ time.Time) error {
	return nil
}

func (r *buzonRepoFake) ContarPendientes(_ context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.contarPendientesFalla != nil {
		return 0, r.contarPendientesFalla
	}
	n := 0
	for _, m := range r.byID {
		if m.Estado() == domain.EstadoReenvioPendiente {
			n++
		}
	}
	return n, nil
}

// count returns how many distinct wamid rows are currently stored — the
// duplicate-webhook test's core assertion.
func (r *buzonRepoFake) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byWamid)
}

// llamadasAGuardar returns how many times Guardar was invoked, regardless
// of whether it inserted — proves the handler actually reached the
// service on a valid-signature request, distinct from count() proving
// idempotency.
func (r *buzonRepoFake) llamadasAGuardar() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.guardarLlamadas
}

// mensajePorWamid returns the stored entity for wamid, for field
// assertions.
func (r *buzonRepoFake) mensajePorWamid(wamid string) (*domain.MensajeEntrante, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byWamid[wamid]
	if !ok {
		return nil, false
	}
	m := r.byID[id]
	return m, true
}

// ── Forwarder ────────────────────────────────────────────────────────────

// forwarderFake is never actually invoked by canalhttp's tests (nothing
// here drains the queue), but canalapp.NewService requires one to build a
// real *canalapp.Service to hand the webhook handler.
type forwarderFake struct{}

func (forwarderFake) Reenviar(_ context.Context, _ *domain.MensajeEntrante) error { return nil }

// ── whatsapp.Client ──────────────────────────────────────────────────────

// waFake is a scripted whatsapp.Client: each Send* call returns the next
// scripted (wamid, err) pair, or the last one repeated once the script is
// exhausted. Records every call's (to, body) for assertions.
type waFake struct {
	mu sync.Mutex

	sendTextOutcomes []waOutcome
	idx              int

	sendTextCalls     []waCall
	sendTemplateCalls []waCall
}

type waOutcome struct {
	wamid string
	err   error
}

type waCall struct {
	to   string
	body string
}

func newWAFake(outcomes ...waOutcome) *waFake {
	return &waFake{sendTextOutcomes: outcomes}
}

func (w *waFake) next() waOutcome {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.sendTextOutcomes) == 0 {
		return waOutcome{wamid: "wamid.default"}
	}
	i := w.idx
	if i >= len(w.sendTextOutcomes) {
		i = len(w.sendTextOutcomes) - 1
	}
	w.idx++
	return w.sendTextOutcomes[i]
}

func (w *waFake) SendText(_ context.Context, to, body string) (string, error) {
	w.mu.Lock()
	w.sendTextCalls = append(w.sendTextCalls, waCall{to: to, body: body})
	w.mu.Unlock()
	out := w.next()
	return out.wamid, out.err
}

func (w *waFake) SendTemplate(_ context.Context, to string, tmpl whatsapp.Template) (string, error) {
	w.mu.Lock()
	w.sendTemplateCalls = append(w.sendTemplateCalls, waCall{to: to, body: tmpl.Name})
	w.mu.Unlock()
	out := w.next()
	return out.wamid, out.err
}

func (w *waFake) SendDocumentByMediaID(_ context.Context, _, _, _, _ string) (string, error) {
	return "", nil
}

func (w *waFake) UploadMedia(_ context.Context, _ whatsapp.Media) (string, error) {
	return "", nil
}

func (w *waFake) sendTextCallCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.sendTextCalls)
}
