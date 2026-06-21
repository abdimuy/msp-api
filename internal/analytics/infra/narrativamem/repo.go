// Package narrativamem provides an in-memory outbound.NarrativaRepo for tests
// (worker, read-path). It is concurrency-safe and mirrors the Firebird adapter's
// contract: GetNarrativa miss returns (nil, nil); Encolar is idempotent by
// CLIENTE_ID; Upsert is one row per CLIENTE_ID.
//
//nolint:misspell // Spanish domain vocabulary per project convention.
package narrativamem

import (
	"context"
	"sort"
	"sync"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// compile-time assertion: *Repo must satisfy outbound.NarrativaRepo.
var _ outbound.NarrativaRepo = (*Repo)(nil)

// Repo is an in-memory NarrativaRepo for tests. All methods are safe for
// concurrent use. It is NOT suitable for production — use the Firebird adapter.
type Repo struct {
	mu         sync.Mutex
	rows       map[int]*outbound.NarrativaRow // keyed by clienteID
	pendientes map[int]outbound.PendienteRow  // keyed by clienteID
}

// New creates an empty, ready-to-use Repo.
func New() *Repo {
	return &Repo{
		rows:       make(map[int]*outbound.NarrativaRow),
		pendientes: make(map[int]outbound.PendienteRow),
	}
}

// GetNarrativa returns the cached row for a client, or (nil, nil) if absent.
//
//nolint:nilnil // (nil, nil) means "not found" per NarrativaRepo contract.
func (r *Repo) GetNarrativa(_ context.Context, clienteID int) (*outbound.NarrativaRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	row, ok := r.rows[clienteID]
	if !ok {
		return nil, nil
	}
	// Return a shallow copy so callers cannot mutate the stored pointer.
	cp := *row
	cp.Rasgos = append([]string(nil), row.Rasgos...)
	return &cp, nil
}

// UpsertNarrativa inserts or replaces the cached row (one per CLIENTE_ID).
func (r *Repo) UpsertNarrativa(_ context.Context, n domain.Narrativa) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rows[n.ClienteID] = &outbound.NarrativaRow{
		ClienteID: n.ClienteID,
		Texto:     n.Texto,
		Rasgos:    append([]string(nil), n.Rasgos...),
		InputHash: n.InputHash,
		Modelo:    n.Modelo,
	}
	return nil
}

// Encolar idempotently enqueues a client for generation (PK CLIENTE_ID).
// A second call with the same clienteID overwrites the prior inputHash.
func (r *Repo) Encolar(_ context.Context, clienteID int, inputHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pendientes[clienteID] = outbound.PendienteRow{
		ClienteID: clienteID,
		InputHash: inputHash,
	}
	return nil
}

// ListarPendientes returns up to limit queued clients in deterministic order
// (ascending by clienteID). A limit of 0 returns all pending rows.
func (r *Repo) ListarPendientes(_ context.Context, limit int) ([]outbound.PendienteRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ids := make([]int, 0, len(r.pendientes))
	for id := range r.pendientes {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
	}

	rows := make([]outbound.PendienteRow, len(ids))
	for i, id := range ids {
		rows[i] = r.pendientes[id]
	}
	return rows, nil
}

// BorrarPendiente removes a client from the queue. A no-op if absent.
func (r *Repo) BorrarPendiente(_ context.Context, clienteID int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.pendientes, clienteID)
	return nil
}

// NarrativaCount returns the number of cached narrativa rows. Useful for test
// assertions without exposing internal state directly.
func (r *Repo) NarrativaCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rows)
}

// PendientesCount returns the number of queued pending rows. Useful for test
// assertions.
func (r *Repo) PendientesCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pendientes)
}
