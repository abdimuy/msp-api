package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	canalapp "github.com/abdimuy/msp-api/internal/canal/app"
)

// workerSobre wires a ReenvioWorker over freshly built fakes and returns the
// repo too, so a test can assert on it after the worker has ticked.
// listarPendientesLlamadas — not the forwarder's call count — is the
// reliable "did a tick actually run" signal: ListarPendientes is called on
// every tick regardless of whether the mailbox holds anything to forward,
// while the forwarder is only reached when it does.
func workerSobre(interval time.Duration, batch int) (*canalapp.ReenvioWorker, *buzonRepoFake) {
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := canalapp.NewService(repo, fwd, clock, nil, canalapp.ReenvioConfig{}, nil)
	w := canalapp.NewReenvioWorker(svc, canalapp.ReenvioWorkerConfig{Interval: interval, Batch: batch}, nil)
	return w, repo
}

// esperarHasta polls cond until it holds or the deadline passes. Used
// instead of a fixed sleep so the tests are neither flaky nor slow.
func esperarHasta(t *testing.T, cond func() bool) bool {
	t.Helper()
	limite := time.Now().Add(2 * time.Second)
	for time.Now().Before(limite) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

func TestReenvioWorker_TickDrenaLaCola(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake(nil)
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := canalapp.NewService(repo, fwd, clock, nil, canalapp.ReenvioConfig{}, nil)
	_, err := svc.RecibirMensaje(context.Background(), mensajeParams("wamid-worker", clock.Now()))
	require.NoError(t, err)

	w := canalapp.NewReenvioWorker(svc, canalapp.ReenvioWorkerConfig{Interval: 5 * time.Millisecond, Batch: 10}, nil)

	require.NoError(t, w.Start(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		assert.NoError(t, w.Stop(ctx))
	})

	assert.True(t, esperarHasta(t, func() bool { return fwd.llamadasTotales() >= 1 }),
		"the worker must drain the pendiente on its own, without anything calling DrenarCola directly")
}

func TestReenvioWorker_ApagadoLimpio(t *testing.T) {
	t.Parallel()
	w, repo := workerSobre(5*time.Millisecond, 10)

	require.NoError(t, w.Start(context.Background()))
	require.True(t, esperarHasta(t, func() bool { return repo.listarPendientesLlamadas() >= 1 }),
		"the worker must have ticked at least once before Stop is exercised")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(ctx), "Stop must return before its own timeout")

	// The loop goroutine must actually be gone: no further ticks after Stop
	// returns, even if we wait past several more intervals.
	llamadasAlParar := repo.listarPendientesLlamadas()
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, llamadasAlParar, repo.listarPendientesLlamadas(), "no tick may fire after Stop has returned")
}

func TestReenvioWorker_StartEsIdempotente(t *testing.T) {
	t.Parallel()
	w, repo := workerSobre(5*time.Millisecond, 10)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))
	require.NoError(t, w.Start(ctx), "a second Start must be a no-op")

	// A single goroutine must still be the one ticking: give it a moment,
	// then stop and confirm ticks actually happened (proving Start worked)
	// without a second overlapping loop having been spawned (which a
	// non-idempotent Start would do, though this alone cannot distinguish
	// one loop from two ticking at the same cadence — the mutex-guarded
	// running flag is what actually prevents that; see Start's own code).
	require.True(t, esperarHasta(t, func() bool { return repo.listarPendientesLlamadas() >= 1 }))

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))
}

func TestReenvioWorker_StopSinStart(t *testing.T) {
	t.Parallel()
	w, _ := workerSobre(time.Hour, 10)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, w.Stop(ctx), "Stop on a worker that never started must return immediately")
}

func TestReenvioWorker_StopEsIdempotente(t *testing.T) {
	t.Parallel()
	w, _ := workerSobre(time.Hour, 10)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))
	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))
	require.NoError(t, w.Stop(stopCtx), "a second Stop must be harmless")
}

// TestReenvioWorker_LoopSobreviveAlCierreDelContextoDeArranque pins the
// constraint most likely to be silently wrong: fx cancels the OnStart
// context once the start phase ends, so a loop that inherited it would die
// on the very first tick. Cancelling the context handed to Start must
// change nothing about the loop's lifetime.
func TestReenvioWorker_LoopSobreviveAlCierreDelContextoDeArranque(t *testing.T) {
	t.Parallel()
	w, repo := workerSobre(5*time.Millisecond, 10)

	startCtx, cancelStart := context.WithCancel(context.Background())
	require.NoError(t, w.Start(startCtx))
	cancelStart() // fx does exactly this once the start phase completes.

	llamadasTrasCancelar := repo.listarPendientesLlamadas()
	assert.True(t, esperarHasta(t, func() bool { return repo.listarPendientesLlamadas() > llamadasTrasCancelar }),
		"the loop must not die when fx cancels the OnStart context")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))
}

// TestReenvioWorker_StopRespectsDeadline verifies Stop returns ctx.Err()
// rather than blocking forever when the caller's own deadline is already
// gone — Stop must be a bounded wait, not an unconditional one.
func TestReenvioWorker_StopRespectsDeadline(t *testing.T) {
	t.Parallel()
	w, _ := workerSobre(time.Hour, 10)

	require.NoError(t, w.Start(context.Background()))

	// An already-expired deadline forces the ctx.Done() branch in Stop.
	expired, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	time.Sleep(time.Millisecond)
	err := w.Stop(expired)
	// The loop drains almost instantly (nothing pending, 1h tick interval),
	// so Stop can legitimately win the race against the already-expired
	// deadline; either outcome is correct as long as it is prompt.
	if err != nil {
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}

	// Clean up regardless of which branch fired above.
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCleanup()
	_ = w.Stop(cleanupCtx)
}

// TestNewReenvioWorker_ConfigCero_NoFallaAlConstruirNiArrancar exercises
// ReenvioWorkerConfig's zero-value defaults (Interval and Batch both
// unset). It cannot observe a 30-second tick within a test's budget, but
// applyDefaults runs synchronously inside the constructor, so a lifecycle
// that starts and stops cleanly is enough to prove a zero config never
// panics or hangs.
func TestNewReenvioWorker_ConfigCero_NoFallaAlConstruirNiArrancar(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Now().UTC())
	svc := canalapp.NewService(repo, fwd, clock, nil, canalapp.ReenvioConfig{}, nil)
	w := canalapp.NewReenvioWorker(svc, canalapp.ReenvioWorkerConfig{}, nil)

	require.NoError(t, w.Start(context.Background()))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(ctx))
}

// TestReenvioWorker_BatchPorDefecto proves the Batch default (50) actually
// reaches ListarPendientes, not just that applyDefaults ran.
func TestReenvioWorker_BatchPorDefecto(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Now().UTC())
	svc := canalapp.NewService(repo, fwd, clock, nil, canalapp.ReenvioConfig{}, nil)
	// Batch left unset (0): must default to 50.
	w := canalapp.NewReenvioWorker(svc, canalapp.ReenvioWorkerConfig{Interval: 5 * time.Millisecond}, nil)

	require.NoError(t, w.Start(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		assert.NoError(t, w.Stop(ctx))
	})

	require.True(t, esperarHasta(t, func() bool { return repo.listarPendientesLlamadas() >= 1 }))
	assert.Equal(t, 50, repo.ultimoLimite())
}

// TestReenvioWorker_TickFallaYSigueFuncionando verifies the loop survives a
// failed pass, mirroring flota's SnapshotWorker test for the same property:
// a repository outage must degrade to "try again next tick", never to a
// dead worker that stops draining the mailbox for good.
func TestReenvioWorker_TickFallaYSigueFuncionando(t *testing.T) {
	t.Parallel()
	w, repo := workerSobre(5*time.Millisecond, 10)
	errRepo := errors.New("sqlite: database is locked")
	repo.fallarCon("ListarPendientes", errRepo)

	require.NoError(t, w.Start(context.Background()))
	require.True(t, esperarHasta(t, func() bool { return repo.listarPendientesLlamadas() >= 2 }),
		"the worker must keep trying after a failed tick")

	repo.fallarCon("", nil)
	llamadasAntes := repo.listarPendientesLlamadas()
	assert.True(t, esperarHasta(t, func() bool { return repo.listarPendientesLlamadas() > llamadasAntes }),
		"once the repository recovers, the next tick must run normally")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(ctx))
}
