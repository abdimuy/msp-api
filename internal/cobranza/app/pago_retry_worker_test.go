//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// fixedNowRetry is the deterministic clock value used across all retry worker
// lifecycle tests.
var fixedNowRetry = time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

// discardLogger returns a slog.Logger that discards all output, keeping tests
// silent.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newRetryWorker builds a PagoRetryWorker wired against the given fakes for
// lifecycle tests. cfg is applied on top of safe defaults (short interval,
// small batch).
func newRetryWorker(
	t *testing.T,
	repo *fakePagosRecibidosRepo,
	writer *fakeMicrosipPagoWriter,
	cfg app.PagoRetryWorkerConfig,
) *app.PagoRetryWorker {
	t.Helper()
	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNowRetry)
	return app.NewPagoRetryWorker(svc, repo, fixedClock{T: fixedNowRetry}, cfg, discardLogger())
}

// ─── Lifecycle ───────────────────────────────────────────────────────────────

func TestPagoRetryWorker_Start_Idempotent(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	writer := &fakeMicrosipPagoWriter{}
	cfg := app.PagoRetryWorkerConfig{Interval: 10 * time.Millisecond, BatchLimit: 10}
	w := newRetryWorker(t, repo, writer, cfg)

	ctx := context.Background()
	t.Cleanup(func() { _ = w.Stop(context.Background()) })

	err1 := w.Start(ctx)
	require.NoError(t, err1, "first Start must return nil")

	err2 := w.Start(ctx)
	require.NoError(t, err2, "second Start must also return nil (idempotent)")

	// Stop once — must complete cleanly, proving only one goroutine is running.
	stopErr := w.Stop(context.Background())
	require.NoError(t, stopErr)

	// Second Stop after the first already set running=false must be a no-op.
	stopErr2 := w.Stop(context.Background())
	require.NoError(t, stopErr2)
}

func TestPagoRetryWorker_Stop_Idempotent(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	writer := &fakeMicrosipPagoWriter{}
	cfg := app.PagoRetryWorkerConfig{Interval: 10 * time.Millisecond, BatchLimit: 10}
	w := newRetryWorker(t, repo, writer, cfg)

	ctx := context.Background()
	t.Cleanup(func() { _ = w.Stop(context.Background()) })

	require.NoError(t, w.Start(ctx))

	// First Stop — waits for goroutine.
	require.NoError(t, w.Stop(context.Background()), "first Stop must return nil")

	// Second Stop — worker already stopped; must return nil immediately.
	require.NoError(t, w.Stop(context.Background()), "second Stop must return nil (idempotent)")
}

func TestPagoRetryWorker_Stop_WithoutStart(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	writer := &fakeMicrosipPagoWriter{}
	cfg := app.PagoRetryWorkerConfig{Interval: 10 * time.Millisecond, BatchLimit: 10}
	w := newRetryWorker(t, repo, writer, cfg)

	// No Start — Stop must return nil without blocking.
	err := w.Stop(context.Background())
	require.NoError(t, err)
}

func TestPagoRetryWorker_Stop_ContextCancel(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	writer := &fakeMicrosipPagoWriter{}
	// Slow interval so the goroutine stays alive long enough for the test.
	cfg := app.PagoRetryWorkerConfig{Interval: 5 * time.Second, BatchLimit: 10}
	w := newRetryWorker(t, repo, writer, cfg)

	startCtx := context.Background()
	require.NoError(t, w.Start(startCtx))
	t.Cleanup(func() { _ = w.Stop(context.Background()) })

	// Pass a pre-canceled context to Stop — it must return context.Canceled.
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled

	err := w.Stop(canceledCtx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPagoRetryWorker_Lifecycle_StartStop(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo() // empty — tick does nothing
	writer := &fakeMicrosipPagoWriter{}
	cfg := app.PagoRetryWorkerConfig{Interval: 10 * time.Millisecond, BatchLimit: 10}
	w := newRetryWorker(t, repo, writer, cfg)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))

	// Give the first tick time to run (it fires immediately on start).
	time.Sleep(30 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx), "Stop must complete cleanly within 1s")
	// Writer must not have been called: repo is empty.
	assert.Equal(t, 0, writer.callCount)
}
