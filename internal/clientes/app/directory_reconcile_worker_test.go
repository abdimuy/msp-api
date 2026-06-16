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
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// countingDirectoryIndex is an outbound.DirectoryIndex that counts Reconciliar calls.
type countingDirectoryIndex struct {
	calls        atomic.Int32
	reconcileErr error
}

func (f *countingDirectoryIndex) Buscar(_ context.Context, _ outbound.DirectorioQuery) (outbound.DirectorioResultado, error) {
	return outbound.DirectorioResultado{Items: []outbound.DirectorioDoc{}, Total: 0}, nil
}

func (f *countingDirectoryIndex) Reconciliar(_ context.Context, _ []outbound.DirectorioDoc) error {
	f.calls.Add(1)
	return f.reconcileErr
}

// itemDirectorioRepo embeds countingRepo but overrides ListarDirectorioCompleto
// to return a fixed list of DirectorioItems so the reconcile loop has work to do.
type itemDirectorioRepo struct {
	*countingRepo
	items []outbound.DirectorioItem
}

func (r *itemDirectorioRepo) ListarDirectorioCompleto(_ context.Context, _ outbound.FiltroDirectorio) ([]outbound.DirectorioItem, error) {
	return r.items, nil
}

// erroringDirectorioRepo embeds countingRepo but overrides ListarDirectorioCompleto
// to return an error, simulating a DB failure during the warm-up reconcile.
type erroringDirectorioRepo struct {
	*countingRepo
}

func (r *erroringDirectorioRepo) ListarDirectorioCompleto(_ context.Context, _ outbound.FiltroDirectorio) ([]outbound.DirectorioItem, error) {
	return nil, errors.New("firebird down")
}

// buildReconcileWorker wires a DirectoryReconcileWorker against any ClientesRepo
// and countingDirectoryIndex (for Reconciliar).
func buildReconcileWorker(repo outbound.ClientesRepo, dirIdx *countingDirectoryIndex, interval time.Duration) *app.DirectoryReconcileWorker {
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &controlledSearchIndex{}, dirIdx, fixedClock{T: fixedTime})
	return app.NewDirectoryReconcileWorker(svc, app.DirectoryReconcileWorkerConfig{Interval: interval}, nil)
}

// TestDirectoryReconcileWorker_StartStop verifies that Start launches the goroutine and
// Stop returns promptly without panic or hang.
func TestDirectoryReconcileWorker_StartStop(t *testing.T) {
	t.Parallel()

	// itemDirectorioRepo returns one item so ReconciliarDirectorio calls dirIdx.Reconciliar.
	repo := &itemDirectorioRepo{
		countingRepo: &countingRepo{},
		items: []outbound.DirectorioItem{
			{Cliente: newCliente(1, "García López")},
		},
	}
	dirIdx := &countingDirectoryIndex{}
	w := buildReconcileWorker(repo, dirIdx, 10*time.Second)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))

	time.Sleep(30 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))

	assert.GreaterOrEqual(t, int(dirIdx.calls.Load()), 1,
		"expected at least one warm-up reconcile call")
}

// TestDirectoryReconcileWorker_StartIdempotent verifies a second Start is a no-op.
func TestDirectoryReconcileWorker_StartIdempotent(t *testing.T) {
	t.Parallel()

	repo := &countingRepo{docs: []outbound.SearchDoc{}}
	dirIdx := &countingDirectoryIndex{}
	w := buildReconcileWorker(repo, dirIdx, 10*time.Second)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))
	require.NoError(t, w.Start(ctx), "second Start must be a no-op")

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))
}

// TestDirectoryReconcileWorker_StopWithoutStart verifies Stop on a never-started worker
// returns nil immediately.
func TestDirectoryReconcileWorker_StopWithoutStart(t *testing.T) {
	t.Parallel()

	repo := &countingRepo{}
	dirIdx := &countingDirectoryIndex{}
	w := buildReconcileWorker(repo, dirIdx, time.Second)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx), "Stop on idle worker must not error")
}

// TestDirectoryReconcileWorker_WarmupFailsDegradesSafely verifies that when
// ListarDirectorioCompleto fails the worker still starts and stops cleanly.
func TestDirectoryReconcileWorker_WarmupFailsDegradesSafely(t *testing.T) {
	t.Parallel()

	repo := &erroringDirectorioRepo{countingRepo: &countingRepo{}}
	dirIdx := &countingDirectoryIndex{}
	svc := app.NewService(repo, &fakeAnalyticsClient{}, &controlledSearchIndex{}, dirIdx, fixedClock{T: fixedTime})
	w := app.NewDirectoryReconcileWorker(svc, app.DirectoryReconcileWorkerConfig{Interval: 10 * time.Second}, nil)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))

	time.Sleep(30 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx), "worker must survive a failing warm-up")
	// dirIdx should never have been called (reconcile aborted early on repo error)
	assert.Equal(t, int32(0), dirIdx.calls.Load())
}
