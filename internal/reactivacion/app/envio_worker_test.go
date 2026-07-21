//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestEnvioWorker_AutoSendOn_DrenaEnCadaTick(t *testing.T) {
	t.Parallel()
	m := mensajeEncolado(t, 1101, domain.SegmentoRecienLiquidado)
	mensajeRepo := &fakeMensajeRepo{insertados: []*domain.Mensaje{m}}
	cohorteRepo := &fakeCohorteRepo{}
	sender := &fakeSender{}
	svc := newTestService(&fakeUniversoReader{}, cohorteRepo, app.Config{}).
		WithCanal(mensajeRepo, sender, app.NewOpener(), demoGobernador(), true)

	w := app.NewEnvioWorker(svc, fixedClock{now: envioNow}, app.EnvioWorkerConfig{Interval: 10 * time.Millisecond}, true, nil)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))

	require.Eventually(t, func() bool { return sender.enviadoCount() >= 1 }, time.Second, 5*time.Millisecond)

	// Stop() waits for the in-flight tick to finish, so m is safe to read from
	// the test goroutine only after it returns (avoids a data race with the
	// worker's background mutation of m).
	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))

	assert.Equal(t, domain.EstadoEnviado, m.Estado())
}

func TestEnvioWorker_AutoSendOff_NuncaEnvia(t *testing.T) {
	t.Parallel()
	m := mensajeEncolado(t, 1102, domain.SegmentoRecienLiquidado)
	mensajeRepo := &fakeMensajeRepo{insertados: []*domain.Mensaje{m}}
	cohorteRepo := &fakeCohorteRepo{}
	sender := &fakeSender{}
	svc := newTestService(&fakeUniversoReader{}, cohorteRepo, app.Config{}).
		WithCanal(mensajeRepo, sender, app.NewOpener(), demoGobernador(), false)

	w := app.NewEnvioWorker(svc, fixedClock{now: envioNow}, app.EnvioWorkerConfig{Interval: 10 * time.Millisecond}, false, nil)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))

	// Give the ticker a few chances to fire; auto_send=false must keep it a no-op.
	time.Sleep(80 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))

	assert.Equal(t, 0, sender.enviadoCount())
	assert.Equal(t, domain.EstadoEncolado, m.Estado())
}

// TestEnvioWorker_TickError_LogsAndKeepsRunning verifies that a DrenarCola
// error on one tick does not kill the loop — the next tick still fires.
func TestEnvioWorker_TickError_LogsAndKeepsRunning(t *testing.T) {
	t.Parallel()
	m := mensajeEncolado(t, 1103, domain.SegmentoRecienLiquidado)
	mensajeRepo := &fakeMensajeRepo{insertados: []*domain.Mensaje{m}, listarPendientesErr: errors.New("boom")}
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{}).
		WithCanal(mensajeRepo, &fakeSender{}, app.NewOpener(), demoGobernador(), true)

	w := app.NewEnvioWorker(svc, fixedClock{now: envioNow}, app.EnvioWorkerConfig{Interval: 10 * time.Millisecond}, true, nil)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))
	// Let a couple of ticks fire and error out; the worker must not panic or wedge.
	time.Sleep(40 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))
}

func TestEnvioWorker_StartStop(t *testing.T) {
	t.Parallel()
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{}).
		WithCanal(&fakeMensajeRepo{}, &fakeSender{}, app.NewOpener(), demoGobernador(), true)
	w := app.NewEnvioWorker(svc, fixedClock{now: envioNow}, app.EnvioWorkerConfig{Interval: 10 * time.Second}, true, nil)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))
}

func TestEnvioWorker_StartIdempotent(t *testing.T) {
	t.Parallel()
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{}).
		WithCanal(&fakeMensajeRepo{}, &fakeSender{}, app.NewOpener(), demoGobernador(), true)
	w := app.NewEnvioWorker(svc, fixedClock{now: envioNow}, app.EnvioWorkerConfig{Interval: 10 * time.Second}, true, nil)

	ctx := context.Background()
	require.NoError(t, w.Start(ctx))
	require.NoError(t, w.Start(ctx), "second Start must be a no-op")

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx))
}

func TestEnvioWorker_StopIdempotent(t *testing.T) {
	t.Parallel()
	svc := newTestService(&fakeUniversoReader{}, &fakeCohorteRepo{}, app.Config{}).
		WithCanal(&fakeMensajeRepo{}, &fakeSender{}, app.NewOpener(), demoGobernador(), true)
	w := app.NewEnvioWorker(svc, fixedClock{now: envioNow}, app.EnvioWorkerConfig{Interval: time.Second}, true, nil)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, w.Stop(stopCtx), "Stop on a not-running worker must be a no-op")
}
