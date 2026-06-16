//nolint:misspell // clientes vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// ─── countingRepo ────────────────────────────────────────────────────────────

// countingRepo is a ClientesRepo that counts how many times
// LeerDocumentosBusqueda is called and can inject a fixed error or doc list.
type countingRepo struct {
	calls   atomic.Int32
	docs    []outbound.SearchDoc
	docsErr error
}

func (r *countingRepo) LeerDocumentosBusqueda(_ context.Context) ([]outbound.SearchDoc, error) {
	r.calls.Add(1)
	if r.docsErr != nil {
		return nil, r.docsErr
	}
	return r.docs, nil
}

// Unused methods — implement the full ClientesRepo interface so the fake
// satisfies it at compile time.
func (r *countingRepo) ObtenerCliente(_ context.Context, _ int) (*domain.Cliente, error) {
	return nil, domain.ErrClienteNotFound
}

func (r *countingRepo) ListarDirectorio(_ context.Context, _ outbound.ListParams, _ outbound.FiltroDirectorio) (outbound.Page[outbound.DirectorioItem], error) {
	return outbound.Page[outbound.DirectorioItem]{}, nil
}

func (r *countingRepo) ListarDirectorioCompleto(_ context.Context, _ outbound.FiltroDirectorio) ([]outbound.DirectorioItem, error) {
	return nil, nil
}

func (r *countingRepo) ObtenerResumenFicha(_ context.Context, _ int) (outbound.ResumenFicha, error) {
	return outbound.ResumenFicha{}, nil
}

func (r *countingRepo) ListarVentas(_ context.Context, _ int, _ outbound.ListParams) (outbound.Page[*domain.VentaCliente], error) {
	return outbound.Page[*domain.VentaCliente]{}, nil
}

func (r *countingRepo) ObtenerVentaDetalle(_ context.Context, _ int) (outbound.VentaDetalle, error) {
	return outbound.VentaDetalle{}, domain.ErrVentaNotFound
}

func (r *countingRepo) BuscarClienteIDsBasico(_ context.Context, _ string, _ int) ([]int, error) {
	return nil, nil
}

// ─── buildWorkerFromCountingRepo ─────────────────────────────────────────────

// buildWorkerFromCountingRepo wires a ReindexWorker against countingRepo so
// that LeerDocumentosBusqueda calls are tracked through the counting wrapper.
func buildWorkerFromCountingRepo(repo *countingRepo, idx *controlledSearchIndex, interval time.Duration) *app.ReindexWorker {
	svc := app.NewService(repo, &fakeAnalyticsClient{}, idx, fixedClock{T: fixedTime})
	return app.NewReindexWorker(svc, fixedClock{T: fixedTime}, app.ReindexWorkerConfig{Interval: interval}, nil)
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestReindexWorker_StartStop verifies that Start launches the goroutine and
// Stop returns promptly without panic or hang. Interval is very long so the
// ticker never fires — only the initial warm-up reindex runs.
func TestReindexWorker_StartStop(t *testing.T) {
	t.Parallel()

	docs := []outbound.SearchDoc{{ClienteID: 1, Texto: "García"}}
	repo := &countingRepo{docs: docs}
	idx := &controlledSearchIndex{}
	w := buildWorkerFromCountingRepo(repo, idx, 10*time.Second)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))

	// Give the goroutine a moment to run the warm-up.
	time.Sleep(30 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))

	// Warm-up must have fired at least once.
	assert.GreaterOrEqual(t, int(repo.calls.Load()), 1,
		"expected at least one warm-up reindex call")
}

// TestReindexWorker_StartIdempotent verifies that a second Start while already
// running is a no-op (returns nil, does not spawn a second goroutine).
func TestReindexWorker_StartIdempotent(t *testing.T) {
	t.Parallel()

	repo := &countingRepo{docs: []outbound.SearchDoc{}}
	idx := &controlledSearchIndex{}
	w := buildWorkerFromCountingRepo(repo, idx, 10*time.Second)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))
	require.NoError(t, w.Start(ctx), "second Start must be a no-op")

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))
}

// TestReindexWorker_StopWithoutStart verifies that Stop on a never-started
// worker returns nil immediately.
func TestReindexWorker_StopWithoutStart(t *testing.T) {
	t.Parallel()

	repo := &countingRepo{}
	idx := &controlledSearchIndex{}
	w := buildWorkerFromCountingRepo(repo, idx, time.Second)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx), "Stop on idle worker must not error")
}

// TestReindexWorker_WarmupFailsDegradesSafely verifies that when the DB is
// unavailable on warm-up (repo returns an error) the worker still starts,
// logs the warning, and stops cleanly — the app must boot without search.
func TestReindexWorker_WarmupFailsDegradesSafely(t *testing.T) {
	t.Parallel()

	repo := &countingRepo{docsErr: errors.New("firebird down")}
	idx := &controlledSearchIndex{}
	w := buildWorkerFromCountingRepo(repo, idx, 10*time.Second)

	ctx := context.Background()
	// Start must succeed even when the warm-up reindex fails.
	require.NoError(t, w.Start(ctx))

	// Give the goroutine a moment to attempt the warm-up.
	time.Sleep(30 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))

	// The repo was still called (warm-up was attempted).
	assert.GreaterOrEqual(t, int(repo.calls.Load()), 1)
}
