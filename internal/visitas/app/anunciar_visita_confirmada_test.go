// Tests for the push announcement the visitas SSE stream carries.
//
// The announcement is a WAKEUP, not an authority: it carries no visit ids and
// the client marks nothing on receipt. Task 10 established exactly one write
// path to "synced" — positive confirmation via by-ids — and these tests exist
// partly to keep the announcement from quietly growing into a second one.
//
// They subscribe to the REAL *eventbus.Bus rather than a hand-written double.
// The bus is a pure in-process value with no I/O and its own 100%-covered test
// suite, so asserting against it measures what actually reaches a connected
// phone instead of what a fake was told to record.
//
//nolint:misspell // visitas vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/eventbus"
	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// topicVisitasConfirmadas duplicates visitashttp.TopicVisitasConfirmadas.
// app must not import infra (depguard's app-no-infra rule), so the constant is
// restated and pinned by TestTopicoDeAnuncioCoincideConElContrato below plus
// its infra-side counterpart.
const topicVisitasConfirmadas = "visitas_confirmadas"

// suscribir subscribes to the announcement topic and returns a function that
// reports whether an announcement arrived within a short deadline.
//
// The bus delivers on a buffered channel, so a publish that happened BEFORE
// this is read is still observed — there is no race to lose.
func suscribir(t *testing.T, bus *eventbus.Bus) func() bool {
	t.Helper()
	ch, unsubscribe := bus.Subscribe(topicVisitasConfirmadas)
	t.Cleanup(unsubscribe)
	return func() bool {
		select {
		case <-ch:
			return true
		case <-time.After(500 * time.Millisecond):
			return false
		}
	}
}

// TestAnuncio_SeEmiteAlRegistrarUnaVisitaNueva — the ordinary path. A visita
// lands, connected phones are told to go reconcile.
func TestAnuncio_SeEmiteAlRegistrarUnaVisitaNueva(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	anuncioLlego := suscribir(t, bus)
	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithEventBus(bus))

	_, err := svc.RegistrarVisita(context.Background(), validInput(), uuid.New())
	require.NoError(t, err)

	assert.True(t, anuncioLlego(), "registering a visita must announce it")
}

// TestAnuncio_ElEventoNoLlevaIDs pins the RULING in the one place it can be
// mechanically checked: the payload is empty.
//
// If someone later puts visit ids on the wire, the event stops being a pure
// trigger and starts being evidence — a second write path to "synced" with
// weaker guarantees than by-ids. That is exactly what Task 10 forbade.
func TestAnuncio_ElEventoNoLlevaIDs(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	ch, unsubscribe := bus.Subscribe(topicVisitasConfirmadas)
	t.Cleanup(unsubscribe)

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithEventBus(bus))
	_, err := svc.RegistrarVisita(context.Background(), validInput(), uuid.New())
	require.NoError(t, err)

	select {
	case ids := <-ch:
		assert.Empty(t, ids,
			"the announcement is a wakeup and must carry no ids — it has no authority, "+
				"the reconciler confirms through by-ids")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no announcement arrived")
	}
}

// TestAnuncio_SeEmiteEnElReplayIdempotente — the case the whole push channel
// exists for.
//
// A phone whose upload succeeded but whose RESPONSE was lost retries. The
// retry resolves to a replay. If the replay stayed silent, that phone would
// wait for the 15-minute periodic tick to learn what the server already knew,
// which is the exact latency SSE was added to remove.
func TestAnuncio_SeEmiteEnElReplayIdempotente(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithEventBus(bus))

	in := validInput()
	_, err := svc.RegistrarVisita(context.Background(), in, uuid.New())
	require.NoError(t, err)

	// Now subscribe, so only the REPLAY's announcement can be observed.
	anuncioLlego := suscribir(t, bus)
	repo.insertErr = domain.ErrVisitaYaExiste

	got, err := svc.RegistrarVisita(context.Background(), in, uuid.New())
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.True(t, anuncioLlego(),
		"an idempotent replay must announce too: the retrying phone is the one that "+
			"never saw the original response")
}

// TestAnuncio_NoSeEmiteCuandoElInsertFalla — robustez: a write that did not
// happen must not be announced. A phone told to reconcile a visita the server
// does not have would burn a by-ids round trip and find nothing.
func TestAnuncio_NoSeEmiteCuandoElInsertFalla(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	anuncioLlego := suscribir(t, bus)
	repo := newFakeVisitasRepo()
	repo.insertErr = errors.New("firebird caído")
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithEventBus(bus))

	_, err := svc.RegistrarVisita(context.Background(), validInput(), uuid.New())
	require.Error(t, err)

	assert.False(t, anuncioLlego(), "a failed write must announce nothing")
}

// TestAnuncio_NoSeEmiteCuandoLaValidacionDeDominioFalla — same guarantee one
// layer earlier: nothing was even attempted.
func TestAnuncio_NoSeEmiteCuandoLaValidacionDeDominioFalla(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	anuncioLlego := suscribir(t, bus)
	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithEventBus(bus))

	in := validInput()
	in.ClienteID = -1 // rejected by the domain constructor

	_, err := svc.RegistrarVisita(context.Background(), in, uuid.New())
	require.Error(t, err)

	assert.False(t, anuncioLlego(), "a rejected visita must announce nothing")
}

// TestAnuncio_NoSeEmiteCuandoElReplayNoPuedeLeerse — the replay path can still
// fail at FindByID. It returns an error, so it must stay silent.
func TestAnuncio_NoSeEmiteCuandoElReplayNoPuedeLeerse(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	anuncioLlego := suscribir(t, bus)
	repo := newFakeVisitasRepo()
	repo.insertErr = domain.ErrVisitaYaExiste
	repo.findByIDErr = errors.New("firebird caído")
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithEventBus(bus))

	_, err := svc.RegistrarVisita(context.Background(), validInput(), uuid.New())
	require.Error(t, err)

	assert.False(t, anuncioLlego(), "a replay that could not be read must announce nothing")
}

// TestAnuncio_SinBusElRegistroSigueFuncionando — push is a latency
// optimization, never a correctness dependency. A Service with no bus wired
// registers visits exactly the same way; the reconciler's other three triggers
// still pick them up.
func TestAnuncio_SinBusElRegistroSigueFuncionando(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow}) // no WithEventBus

	got, err := svc.RegistrarVisita(context.Background(), validInput(), uuid.New())

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 1, repo.insertCalls)
}

// TestAnuncio_TopicoDeAnuncioCoincideConElContrato guards the duplicated
// constant against drift on this side of the boundary; the infra package has
// the matching assertion against visitashttp.TopicVisitasConfirmadas.
func TestAnuncio_TopicoDeAnuncioCoincideConElContrato(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	ch, unsubscribe := bus.Subscribe("visitas_confirmadas")
	t.Cleanup(unsubscribe)

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithEventBus(bus))
	_, err := svc.RegistrarVisita(context.Background(), validInput(), uuid.New())
	require.NoError(t, err)

	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal(`the service must publish on the literal topic "visitas_confirmadas"`)
	}
}
