// Package app_test exercises the analytics app layer using hand-written fakes
// for the outbound ports. No database connection is required.
//
//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"sync"
	"time"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// ─── Fake Clock ───────────────────────────────────────────────────────────────

// fixedClock always returns the same deterministic time.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// testNow is the deterministic "now" used across all tests.
var testNow = time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

// ─── Fake WinbackRepo ─────────────────────────────────────────────────────────

// fakeWinbackRepo is an in-memory WinbackRepo for tests.
// It records call order for UpsertCandidatos / SaveRefreshState to enable
// ordering assertions.
type fakeWinbackRepo struct {
	mu sync.Mutex

	// seed data for ListCandidatos
	candidates []*domain.WinbackCandidato

	// ExistingControlFlags map — set per test
	controlFlags map[int]bool

	// Recorded calls
	upserted       []*domain.WinbackCandidato
	savedState     *outbound.RefreshState
	callOrder      []string                         // "upsert" | "save_state"
	refreshStateBy map[string]outbound.RefreshState // keyed by job name
}

func newFakeWinbackRepo() *fakeWinbackRepo {
	return &fakeWinbackRepo{
		controlFlags:   make(map[int]bool),
		refreshStateBy: make(map[string]outbound.RefreshState),
	}
}

func (r *fakeWinbackRepo) UpsertCandidatos(_ context.Context, candidatos []*domain.WinbackCandidato) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upserted = append(r.upserted, candidatos...)
	r.callOrder = append(r.callOrder, "upsert")
	return nil
}

func (r *fakeWinbackRepo) ListCandidatos(_ context.Context, p outbound.ListWinbackParams) (outbound.Page[*domain.WinbackCandidato], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.WinbackCandidato
	for _, c := range r.candidates {
		if p.ExcluirControl && c.EnControl() {
			continue
		}
		if p.Zona != "" && c.Zona() != p.Zona {
			continue
		}
		out = append(out, c)
	}
	if p.Limit > 0 && len(out) > p.Limit {
		out = out[:p.Limit]
	}
	return outbound.Page[*domain.WinbackCandidato]{Items: out, Total: len(out)}, nil
}

func (r *fakeWinbackRepo) GetRefreshState(_ context.Context, job string) (outbound.RefreshState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.refreshStateBy[job]
	if !ok {
		return outbound.RefreshState{}, domain.ErrRefreshStateNotFound
	}
	return st, nil
}

func (r *fakeWinbackRepo) SaveRefreshState(_ context.Context, st outbound.RefreshState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshStateBy[st.Job] = st
	r.savedState = &st
	r.callOrder = append(r.callOrder, "save_state")
	return nil
}

func (r *fakeWinbackRepo) ExistingControlFlags(_ context.Context) (map[int]bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[int]bool, len(r.controlFlags))
	for k, v := range r.controlFlags {
		out[k] = v
	}
	return out, nil
}

func (r *fakeWinbackRepo) GetCandidato(_ context.Context, clienteID int) (*domain.WinbackCandidato, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.candidates {
		if c.ClienteID() == clienteID {
			return c, nil
		}
	}
	return nil, domain.ErrWinbackCandidatoNotFound
}

func (r *fakeWinbackRepo) ListCandidatosByClienteIDs(_ context.Context, clienteIDs []int) ([]*domain.WinbackCandidato, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make(map[int]struct{}, len(clienteIDs))
	for _, id := range clienteIDs {
		ids[id] = struct{}{}
	}
	var result []*domain.WinbackCandidato
	for _, c := range r.candidates {
		if _, ok := ids[c.ClienteID()]; ok {
			result = append(result, c)
		}
	}
	return result, nil
}

// ─── Fake MicrosipReader ──────────────────────────────────────────────────────

type fakeMicrosipReader struct {
	mu     sync.Mutex
	anclas []outbound.AnclaCliente
	since  *time.Time // last since passed to LeerAnclasDesde
}

func (m *fakeMicrosipReader) LeerAnclasDesde(_ context.Context, since *time.Time) ([]outbound.AnclaCliente, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.since = since
	return m.anclas, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// mustCandidato builds a WinbackCandidato or panics. Used only in test helpers
// to construct seed data; production code never uses panic here.
func mustCandidato(params domain.CrearWinbackCandidatoParams) *domain.WinbackCandidato {
	c, err := domain.CrearWinbackCandidato(params)
	if err != nil {
		panic("mustCandidato: " + err.Error())
	}
	return c
}
