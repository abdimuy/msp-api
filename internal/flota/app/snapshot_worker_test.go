//nolint:misspell // flota vocabulary is Spanish (camioneta, roster) per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	flotaapp "github.com/abdimuy/msp-api/internal/flota/app"
	"github.com/abdimuy/msp-api/internal/flota/ports/outbound"
)

// workerSobre wires a SnapshotWorker over an escenario.
func workerSobre(e *escenario, intervalo time.Duration, habilitado bool) *flotaapp.SnapshotWorker {
	return flotaapp.NewSnapshotWorker(e.svc, flotaapp.SnapshotWorkerConfig{
		Intervalo:  intervalo,
		Habilitado: habilitado,
	}, nil)
}

// esperarHasta polls cond until it holds or the deadline passes. Used instead
// of a fixed sleep so the tests are neither flaky nor slow.
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

// TestSnapshotWorker_ArranqueTomaFotoDeInmediato verifies the warm-up pass.
// After a deploy the first thing wanted is a fresh photograph, and on a
// virgin database that pass is what lays down the baseline.
func TestSnapshotWorker_ArranqueTomaFotoDeInmediato(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake([]outbound.RosterUsuario{
		usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
	}))
	w := workerSobre(e, time.Hour, true)

	require.NoError(t, w.Start(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		assert.NoError(t, w.Stop(ctx))
	})

	assert.True(t, esperarHasta(t, func() bool { return len(e.repo.Fotos()) == 1 }),
		"el arranque debe fotografiar sin esperar al primer tick")
	assert.True(t, e.repo.Fotos()[0].EsLineaBase())
}

// TestSnapshotWorker_DeshabilitadoNoLeeElRoster is the kill switch, and the
// assertion that matters is the read count: a disabled worker must not spend
// a single Firestore read. It is also the state a dev machine boots in, where
// Firebase is unconfigured.
func TestSnapshotWorker_DeshabilitadoNoLeeElRoster(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake([]outbound.RosterUsuario{
		usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
	}))
	w := workerSobre(e, time.Millisecond, false)

	require.NoError(t, w.Start(context.Background()))
	time.Sleep(30 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, w.Stop(ctx))

	assert.Equal(t, 0, e.roster.Leidas(), "deshabilitado no puede costar ni una lectura")
	assert.Empty(t, e.repo.Fotos())
}

// TestSnapshotWorker_FalloDeFirestoreNoMataElWorker verifies the loop survives
// a failed pass. A transient outage must degrade to a longer window, never to
// a dead worker that stops recording for good.
func TestSnapshotWorker_FalloDeFirestoreNoMataElWorker(t *testing.T) {
	t.Parallel()

	roster := nuevoRosterFake([]outbound.RosterUsuario{
		usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
	})
	roster.fallarCon(errFirestore)
	e := nuevoEscenario(roster)
	w := workerSobre(e, 5*time.Millisecond, true)

	require.NoError(t, w.Start(context.Background()))
	require.True(t, esperarHasta(t, func() bool { return roster.Leidas() >= 2 }),
		"el worker debe seguir intentando tras un fallo")

	roster.fallarCon(nil)
	assert.True(t, esperarHasta(t, func() bool { return len(e.repo.Fotos()) >= 1 }),
		"al restablecerse firestore, la siguiente pasada retrata")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(ctx))
}

// TestSnapshotWorker_StartIdempotente verifies a second Start is a no-op, so a
// double lifecycle registration cannot double the Firestore bill.
func TestSnapshotWorker_StartIdempotente(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake([]outbound.RosterUsuario{
		usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
	}))
	w := workerSobre(e, time.Hour, true)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))
	require.NoError(t, w.Start(ctx), "el segundo Start debe ser no-op")

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))

	assert.LessOrEqual(t, e.roster.Leidas(), 1, "un solo goroutine, una sola lectura de arranque")
}

// TestSnapshotWorker_StopSinStart verifies Stop on a worker that never ran
// returns immediately rather than blocking shutdown.
func TestSnapshotWorker_StopSinStart(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake())
	w := workerSobre(e, time.Hour, true)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, w.Stop(ctx))
}

// TestSnapshotWorker_StopEsIdempotente verifies a second Stop is harmless —
// fx can call it on a lifecycle that already unwound.
func TestSnapshotWorker_StopEsIdempotente(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake([]outbound.RosterUsuario{
		usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
	}))
	w := workerSobre(e, time.Hour, true)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))
	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))
	require.NoError(t, w.Stop(stopCtx))
}

// TestSnapshotWorker_LoopSobreviveAlCierreDelContextoDeArranque pins the
// detail that has bitten this codebase before: fx cancels the OnStart context
// once the start phase ends, so a loop inheriting it would die on the first
// tick. Cancelling the context handed to Start must change nothing.
func TestSnapshotWorker_LoopSobreviveAlCierreDelContextoDeArranque(t *testing.T) {
	t.Parallel()

	e := nuevoEscenario(nuevoRosterFake([]outbound.RosterUsuario{
		usuario("uid-1", "eliseo@msp.com", "Eliseo", intPtr(11341)),
	}))
	w := workerSobre(e, 5*time.Millisecond, true)

	startCtx, cancelStart := context.WithCancel(context.Background())
	require.NoError(t, w.Start(startCtx))
	cancelStart() // fx does exactly this when the start phase finishes.

	leidasTrasCancelar := e.roster.Leidas()
	assert.True(t, esperarHasta(t, func() bool { return e.roster.Leidas() > leidasTrasCancelar }),
		"el loop no debe morir cuando fx cancela el contexto de arranque")

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))
}
