//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
)

// ─── contador de ticks ───────────────────────────────────────────────────────

// countingPendientesRepo cuenta las llamadas a ListPendientes. Cada vuelta del
// bucle de PagoRetryWorker llama exactamente una vez a ListPendientes, así que
// el contador ES el número de ticks. Devuelve la lista vacía a propósito: con
// cero pendientes tick() retorna antes de tocar el Service, de modo que el
// worker puede construirse con svc == nil y la prueba no depende de nada más.
//
// Es race-safe (atomic) porque el bucle corre en su propia goroutine mientras
// la prueba lee el contador.
type countingPendientesRepo struct {
	ticks atomic.Int64
}

func (r *countingPendientesRepo) ListPendientes(_ context.Context, _, _ int) ([]*domain.PagoRecibido, error) {
	r.ticks.Add(1)
	return nil, nil
}

func (r *countingPendientesRepo) Insert(_ context.Context, _ *domain.PagoRecibido) error { return nil }
func (r *countingPendientesRepo) Update(_ context.Context, _ *domain.PagoRecibido) error { return nil }
func (r *countingPendientesRepo) LockByID(_ context.Context, _ uuid.UUID) error          { return nil }

func (r *countingPendientesRepo) FindByID(_ context.Context, _ uuid.UUID) (*domain.PagoRecibido, error) {
	return nil, domain.ErrPagoNoEncontrado
}

// ─── tiempos de la prueba ────────────────────────────────────────────────────
//
// Se conserva la proporción de producción, donde el intervalo del worker (60s)
// es varias veces mayor que el fx.StartTimeout (15s por defecto). Si el bucle
// hereda la cancelación del contexto de arranque, muere antes de que el ticker
// llegue a dispararse la primera vez y sólo se ve el tick inmediato.
const (
	fxStartTimeoutCorto = 60 * time.Millisecond
	tickIntervaloCorto  = 200 * time.Millisecond
	ventanaObservacion  = 1200 * time.Millisecond
)

// fxStartTimeoutDeadline es el StartTimeout de TestFx_CtxDeOnStartTraeDeadline,
// que NO comparte el de arriba a propósito.
//
// Esa prueba no monta el worker, así que no la ata la relación
// «StartTimeout < intervalo de tick» de la que depende la reproducción; en
// cambio sí la castiga un presupuesto estrecho, porque afirma sobre un contexto
// que se está muriendo solo. Con 60 ms, un stall del planificador entre el
// OnStart y las aserciones —cosa normal bajo -race en un runner cargado— la
// ponía en rojo sin que nada estuviera roto. Medio segundo da holgura y la
// prueba sigue durando eso: el contexto muere igual de solo.
const fxStartTimeoutDeadline = 500 * time.Millisecond

// newCountingRetryWorker construye el worker con el repo contador. svc es nil a
// propósito (ver countingPendientesRepo).
func newCountingRetryWorker(t *testing.T, repo *countingPendientesRepo) *app.PagoRetryWorker {
	t.Helper()
	return app.NewPagoRetryWorker(
		nil,
		repo,
		fixedClock{T: fixedNowRetry},
		app.PagoRetryWorkerConfig{Interval: tickIntervaloCorto, BatchLimit: 10},
		discardLogger(),
	)
}

// ─── 1. La reproducción ──────────────────────────────────────────────────────

// TestPagoRetryWorker_SigueTickeandoTrasElArranqueDeFx monta el worker a través
// de fx exactamente como producción (fx.New + lifecycle.Append + App.Start con
// el startCtx que construye App.run) y comprueba que el bucle sobrevive al
// contexto de arranque.
//
// App.run (go.uber.org/fx@v1.24.0 app.go:606) hace:
//
//	startCtx, cancel := app.clock.WithTimeout(context.Background(), app.StartTimeout())
//	defer cancel()
//	app.Start(startCtx)
//
// es decir, el ctx que llega a OnStart trae un deadline de StartTimeout (15s por
// defecto, app.go:45). PagoRetryWorker.Start deriva su loopCtx de ese ctx, así
// que a los 15s el bucle sale por <-ctx.Done() y no vuelve a tickear nunca.
func TestPagoRetryWorker_SigueTickeandoTrasElArranqueDeFx(t *testing.T) {
	t.Parallel()

	repo := &countingPendientesRepo{}
	worker := newCountingRetryWorker(t, repo)

	fxApp := fx.New(
		fx.NopLogger,
		fx.StartTimeout(fxStartTimeoutCorto),
		fx.Supply(worker),
		fx.Invoke(func(lc fx.Lifecycle, w *app.PagoRetryWorker) {
			lifecycle.Append(lc, "pago-retry-worker", w)
		}),
	)
	require.NoError(t, fxApp.Err(), "el grafo de fx debe construirse sin error")

	// Réplica exacta de App.run: el startCtx lleva el StartTimeout y su cancel
	// queda diferido hasta el final (en producción, hasta que muere el proceso).
	startCtx, cancel := context.WithTimeout(context.Background(), fxApp.StartTimeout())
	defer cancel()

	require.NoError(t, fxApp.Start(startCtx), "fx debe arrancar")
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = fxApp.Stop(stopCtx)
	})

	// Se observa durante 1.2s con un intervalo de 200ms: sin el defecto deben
	// verse ~6 ticks; con el defecto sólo el inmediato, porque el startCtx
	// venció a los 60ms.
	time.Sleep(ventanaObservacion)
	ticks := repo.ticks.Load()

	assert.Greater(t, ticks, int64(1),
		"el worker sólo tickeó %d vez tras el arranque de fx: el bucle heredó la cancelación/deadline del ctx de OnStart", ticks)
	assert.GreaterOrEqual(t, ticks, int64(3),
		"se esperaban al menos 3 ticks en %s con intervalo %s, hubo %d", ventanaObservacion, tickIntervaloCorto, ticks)
}

// ─── 2. El mecanismo, medido sobre fx ────────────────────────────────────────

// TestFx_CtxDeOnStartTraeDeadline demuestra el eslabón que la hipótesis daba
// por supuesto: el contexto que fx entrega a OnStart trae deadline y se cancela
// solo, sin que nadie llame a cancel. No prueba el worker; prueba la librería.
func TestFx_CtxDeOnStartTraeDeadline(t *testing.T) {
	t.Parallel()

	// No se guarda el ctx (fatcontext): basta con su canal Done y un cierre que
	// consulta Err(), que es todo lo que hace falta para observar su muerte.
	var (
		vencido   <-chan struct{}
		errDelFx  func() error
		teniaDL   bool
		deadline  time.Time
		enOnStart time.Time
	)

	fxApp := fx.New(
		fx.NopLogger,
		fx.StartTimeout(fxStartTimeoutDeadline),
		fx.Invoke(func(lc fx.Lifecycle) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					// El instante se toma DENTRO del hook para poder medir el
					// deadline como un intervalo desde aquí (ver abajo).
					enOnStart = time.Now()
					vencido = ctx.Done()
					errDelFx = ctx.Err
					deadline, teniaDL = ctx.Deadline()
					return nil
				},
			})
		}),
	)
	require.NoError(t, fxApp.Err())

	startCtx, cancel := context.WithTimeout(context.Background(), fxApp.StartTimeout())
	defer cancel()
	require.NoError(t, fxApp.Start(startCtx))
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = fxApp.Stop(stopCtx)
	})

	require.True(t, teniaDL, "el ctx de OnStart DEBE traer deadline (fx lo construye con WithTimeout(StartTimeout))")

	// Se afirma un INTERVALO medido contra un instante tomado DENTRO del hook,
	// no un punto medido contra time.Now() en la aserción. La versión anterior
	// («el deadline cae a StartTimeout de ahora, ±40 ms») convertía cualquier
	// pausa entre el OnStart y esta línea en un rojo. Medido desde dentro, el
	// intervalo es exacto por construcción: WithTimeout fijó el deadline ANTES
	// de que corriera el hook, así que lo que queda es forzosamente positivo y
	// como mucho un StartTimeout.
	restante := deadline.Sub(enOnStart)
	assert.Positive(t, restante,
		"dentro del OnStart el deadline aún no puede haber pasado")
	assert.LessOrEqual(t, restante, fxStartTimeoutDeadline,
		"el deadline no puede caer más allá de un StartTimeout (%s) del arranque; quedaban %s",
		fxStartTimeoutDeadline, restante)

	// Cuando OnStart retornó, el ctx seguía vivo — el arranque no lo cancela.
	require.NoError(t, errDelFx(), "recién arrancado, el ctx de OnStart aún no está cancelado")

	// Pero muere solo al vencer el deadline, con la app perfectamente viva.
	select {
	case <-vencido:
	case <-time.After(2 * fxStartTimeoutDeadline):
		t.Fatal("el ctx de OnStart nunca se canceló")
	}
	require.ErrorIs(t, errDelFx(), context.DeadlineExceeded,
		"el ctx de OnStart muere por deadline, no por cancelación explícita")
}

// ─── 3. Refutación de las explicaciones alternativas ─────────────────────────

// TestPagoRetryWorker_ConCtxVivoTickeaVariasVeces aísla la causa: con un ctx que
// nadie cancela, el MISMO worker, el MISMO ticker y el MISMO intervalo tickean
// muchas veces. Descarta ticker mal construido, pánico dentro de tick, la
// guarda `running` y un Stop espurio como explicaciones del tick único.
func TestPagoRetryWorker_ConCtxVivoTickeaVariasVeces(t *testing.T) {
	t.Parallel()

	repo := &countingPendientesRepo{}
	worker := newCountingRetryWorker(t, repo)

	require.NoError(t, worker.Start(context.Background()))
	t.Cleanup(func() { _ = worker.Stop(context.Background()) })

	time.Sleep(ventanaObservacion)

	assert.GreaterOrEqual(t, repo.ticks.Load(), int64(3),
		"con un ctx que nadie cancela el bucle debe tickear repetidamente; si no, el defecto no está en el ctx")
}

// TestPagoRetryWorker_StopSigueDeteniendoElBucle protege el efecto secundario
// del arreglo propuesto: si el bucle deja de heredar la cancelación del ctx de
// arranque, lo único que puede pararlo es Stop. Esta prueba comprueba que el
// OnStop de fx efectivamente lo para — vale antes y después del arreglo, así
// que si alguien desliga el ctx y rompe el apagado ordenado, falla aquí.
func TestPagoRetryWorker_StopSigueDeteniendoElBucle(t *testing.T) {
	t.Parallel()

	repo := &countingPendientesRepo{}
	worker := newCountingRetryWorker(t, repo)

	fxApp := fx.New(
		fx.NopLogger,
		fx.StartTimeout(fxStartTimeoutCorto),
		fx.Supply(worker),
		fx.Invoke(func(lc fx.Lifecycle, w *app.PagoRetryWorker) {
			lifecycle.Append(lc, "pago-retry-worker", w)
		}),
	)
	require.NoError(t, fxApp.Err())

	startCtx, cancel := context.WithTimeout(context.Background(), fxApp.StartTimeout())
	defer cancel()
	require.NoError(t, fxApp.Start(startCtx))

	time.Sleep(tickIntervaloCorto / 2)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	require.NoError(t, fxApp.Stop(stopCtx), "el apagado ordenado debe completar sin error")

	trasStop := repo.ticks.Load()
	time.Sleep(3 * tickIntervaloCorto)

	assert.Equal(t, trasStop, repo.ticks.Load(),
		"tras el OnStop de fx el bucle no debe volver a tickear: el apagado ordenado sigue mandando")
}
