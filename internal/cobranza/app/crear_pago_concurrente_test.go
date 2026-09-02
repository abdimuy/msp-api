//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// The scenario these tests exist for is the phone posting the same pago
// twice at once: a flaky connection, the cobrador tapping again, a retry
// firing while the first request is still open. The UUID is the same on both.
//
// It matters more now than before task 2. The Microsip writer used to run
// after the commit; today it runs INSIDE the transaction that inserts the
// pago, so "how many times did we insert" and "how many times did we charge
// the client in Microsip" are the same question. Two writer calls for one
// UUID is a double charge.
//
// All three tests here run on in-memory doubles with snapshottingTxRunner,
// which serializes whole transactions (see its comment). What that buys and
// what it does not:
//
//	DETECTS: a second concurrent request reaching the writer instead of
//	taking the idempotency fast-path; a rejected attempt leaving a row
//	behind; a rejected attempt burning the UUID so the phone's retry is
//	answered with a pago that never reached Microsip; and — via -race — any
//	unsynchronized state the service itself keeps across calls.
//
//	DOES NOT DETECT: anything that needs two transactions overlapping in
//	time. The serialized runner cannot produce a Firebird primary-key
//	conflict raised at INSERT time against an UNCOMMITTED row, nor a
//	lock wait. Those live in
//	internal/cobranza/infra/ventfb/pagos_recibidos_concurrency_test.go,
//	against real Firebird.

// ─── doubles ────────────────────────────────────────────────────────────────

// concurrentSafeMicrosipWriter counts Aplicar calls under a mutex so a
// parallel test can assert on the count without tripping -race. rechazo, when
// set, makes every call fail — the shape of Microsip refusing the document.
type concurrentSafeMicrosipWriter struct {
	mu        sync.Mutex
	calls     int
	rechazo   error
	resultado outbound.MicrosipPagoResult
}

func (w *concurrentSafeMicrosipWriter) Aplicar(
	_ context.Context, _ outbound.MicrosipPagoInput,
) (outbound.MicrosipPagoResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	if w.rechazo != nil {
		return outbound.MicrosipPagoResult{}, w.rechazo
	}
	return w.resultado, nil
}

func (w *concurrentSafeMicrosipWriter) callCountSafe() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

// setRechazo swaps the rejection after the fact, so a test can let Microsip
// come back without rebuilding the service.
func (w *concurrentSafeMicrosipWriter) setRechazo(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.rechazo = err
}

// ─── 1. two simultaneous POSTs, Microsip accepting ──────────────────────────

// TestConcurrente_MismoUUID_UnSoloEnvioAMicrosip fires N goroutines at the
// same pago UUID with the writer accepting. One row must exist and the writer
// must have been called EXACTLY once.
//
// The row count alone was already asserted by
// TestConcurrent_CrearPagoConImagenes_SameUUID_OnePagoOneImagenSet. What is
// new — and what task 2 made load-bearing — is the writer count: with the
// push inside the transaction, a second call is a second DOCTOS_CC document
// for money that was collected once.
func TestConcurrente_MismoUUID_UnSoloEnvioAMicrosip(t *testing.T) {
	t.Parallel()

	const goroutines = 8
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newConcurrentSafePagosRecibidosRepo()
	imgRepo := newConcurrentSafePagosImagenesRepo()
	store := newConcurrentSafeStorage()
	writer := &concurrentSafeMicrosipWriter{resultado: validWriterResult()}
	runner := newSnapshottingTxRunner(pagosRepo.fakePagosRecibidosRepo, imgRepo.fakePagosImagenesRepo)

	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		pagosRepo, imgRepo, writer, store, nil, runner,
	)

	in := baseCrearInput(now)

	// Census BEFORE.
	require.Equal(t, 0, pagosRepo.rowCount())
	require.Equal(t, 0, writer.callCountSafe())

	// A starting gate, not a for-loop. Launching in sequence lets the first
	// goroutine finish its pre-transactional work (validateCargo,
	// storeAllBlobs) before the last one is even scheduled, which is where
	// -race would have something to find. The gate makes them collide.
	arranque := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			<-arranque
			_, err := svc.CrearPagoConImagenes(context.Background(), in, nil, uuid.New())
			errs <- err
		}()
	}
	close(arranque)
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err, "every concurrent POST of an accepted pago must answer 2xx")
	}

	// Census AFTER.
	assert.Equal(t, 1, pagosRepo.rowCount(), "exactly one pago row for one UUID")
	assert.Equal(t, 1, writer.callCountSafe(),
		"Microsip must be written ONCE: a second push is a second charge for the same money")
}

// ─── 2. two simultaneous POSTs, Microsip rejecting ──────────────────────────

// TestConcurrente_MismoUUID_MicrosipRechaza_NoDejaFilaNiQuemaElUUID is the
// chaos case. Both requests are rejected; neither may leave a row behind, and
// the UUID must survive so the phone's next retry can still land.
//
// The failure this guards against is specific: if the rolled-back attempt
// left the row, the SECOND request would see ErrPagoYaExiste, take the
// idempotency fast-path, and answer 2xx for a pago Microsip never accepted —
// the exact invisible ESTADO='P' row the whole plan exists to remove, now
// reachable through a race instead of through the old post-commit writer.
func TestConcurrente_MismoUUID_MicrosipRechaza_NoDejaFilaNiQuemaElUUID(t *testing.T) {
	t.Parallel()

	const goroutines = 4
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newConcurrentSafePagosRecibidosRepo()
	imgRepo := newConcurrentSafePagosImagenesRepo()
	store := newConcurrentSafeStorage()
	rechazo := errors.New("microsip_docto_cc_rechazado")
	writer := &concurrentSafeMicrosipWriter{rechazo: rechazo, resultado: validWriterResult()}
	runner := newSnapshottingTxRunner(pagosRepo.fakePagosRecibidosRepo, imgRepo.fakePagosImagenesRepo)

	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		pagosRepo, imgRepo, writer, store, nil, runner,
	)

	in := baseCrearInput(now)

	arranque := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			<-arranque
			_, err := svc.CrearPagoConImagenes(context.Background(), in, nil, uuid.New())
			errs <- err
		}()
	}
	close(arranque)
	wg.Wait()
	close(errs)

	// Every caller sees the rejection. Not one of them may be told 2xx by
	// riding the idempotency fast-path over another caller's rolled-back row.
	rechazados := 0
	for err := range errs {
		require.Error(t, err, "no concurrent caller may be answered 2xx for a rejected pago")
		require.ErrorIs(t, err, rechazo)
		rechazados++
	}
	assert.Equal(t, goroutines, rechazados)

	// Census AFTER: nothing survived.
	assert.Equal(t, 0, pagosRepo.rowCount(),
		"a rejected pago must leave no row, however many callers raced for it")
	assert.Equal(t, goroutines, writer.callCountSafe(),
		"each rejected attempt is a genuine attempt: the rollback frees the UUID for the next one")

	// The UUID is not burned: Microsip comes back and the same ID goes
	// through. This is what makes the phone's retry work.
	writer.setRechazo(nil)
	pago, err := svc.CrearPagoConImagenes(context.Background(), in, nil, uuid.New())
	require.NoError(t, err, "after the rejections the same UUID must still be usable")
	require.NotNil(t, pago)
	assert.Equal(t, in.ID, pago.ID())
	assert.True(t, pago.IsAplicada())
	assert.Equal(t, 1, pagosRepo.rowCount())
}

// ─── 3. lock conflict on the apply path ─────────────────────────────────────

// TestAplicarPago_ConflictoDeBloqueo_SeCuentaComoIntentoYNoCancelaElContexto
// covers what the worker sees when Cxc.exe holds the rows the writer needs.
//
// Two things must hold, and only one of them is about the error:
//
//  1. The conflict is counted as an intento and its message is persisted, so
//     the pago backs off and support can read why. A lock conflict that is
//     not recorded means the worker hammers Microsip every tick.
//  2. The writer is called on a context with NO deadline and NO cancellation.
//     That is not decoration: the firebirdsql driver, handed a cancelled
//     context, writes op_cancel and stops reading, desyncing the wire and
//     leaking the connection permanently. Wrapping the writer call in a
//     timeout to "escape" a lock is the intuitive fix and it is the wrong
//     one — internal/platform/firebird/driverwrap.go strips cancellation at
//     the driver boundary for exactly this reason, so the timeout would not
//     even work, it would only look like it did.
//
// DOES NOT DETECT: whether Firebird really emits this GDS code under
// contention with Cxc.exe, or what the driver does with the socket. Those are
// below this layer.
func TestAplicarPago_ConflictoDeBloqueo_SeCuentaComoIntentoYNoCancelaElContexto(t *testing.T) {
	t.Parallel()

	// The error the platform mapper produces for GDS 335544345 / 335544510 /
	// 335544336 — built through the same constructor MapError uses so the
	// two cannot drift apart silently.
	bloqueo := apperror.NewConflict("firebird_lock_conflict",
		"operación bloqueada, intente de nuevo").WithSource("firebird")

	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)
	require.Equal(t, 0, pago.Intentos(), "census BEFORE: intentos=0")

	writer := &ctxCapturingWriter{err: bloqueo}
	runner := newSnapshottingTxRunner(repo, newFakePagosImagenesRepo())
	svc := newAplicarSvc(t, runner, repo, writer, fixedNow)

	_, err := svc.AplicarPago(context.Background(), pago.ID(), uuid.New())

	// 1. The conflict propagates with its kind intact, so the HTTP layer can
	//    answer 409 and the retry worker can classify it.
	require.Error(t, err)
	var ae *apperror.Error
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "firebird_lock_conflict", ae.Code)
	assert.Equal(t, apperror.KindConflict, ae.Kind)

	// 2. Census AFTER: the attempt was counted and the reason stored.
	guardado, findErr := repo.FindByID(context.Background(), pago.ID())
	require.NoError(t, findErr)
	assert.True(t, guardado.IsPendiente(), "the pago waits for the lock holder to let go")
	assert.Equal(t, 1, guardado.Intentos(), "a lock conflict is an intento like any other")
	require.NotNil(t, guardado.UltimoError())
	assert.Contains(t, *guardado.UltimoError(), "firebird_lock_conflict")

	// 3. Nobody tried to escape the lock by cancelling.
	require.Equal(t, 1, writer.calls)
	assert.False(t, writer.tuvoDeadline,
		"the writer must not run under a deadline: cancelling to escape a lock desyncs the firebirdsql driver")
	require.NoError(t, writer.ctxErr,
		"the writer must not run on a cancelled context")
	// The test hands AplicarPago a context.Background(), which has no Done
	// channel. A cancellable context reaching the writer can therefore only
	// have been created INSIDE the service. In production the caller's
	// context is of course cancellable — this assertion is about what the
	// service adds, not about what it receives.
	assert.False(t, writer.cancelable,
		"AplicarPago must not wrap the writer call in a cancellation of its own")
}

// ctxCapturingWriter records the shape of the context AplicarPago hands the
// Microsip writer. It is the only way to assert on a negative — "nobody
// attached a deadline here" — which is otherwise invisible until a
// connection leaks in production.
type ctxCapturingWriter struct {
	err          error
	resultado    outbound.MicrosipPagoResult
	calls        int
	tuvoDeadline bool
	cancelable   bool
	ctxErr       error
}

func (w *ctxCapturingWriter) Aplicar(
	ctx context.Context, _ outbound.MicrosipPagoInput,
) (outbound.MicrosipPagoResult, error) {
	w.calls++
	_, w.tuvoDeadline = ctx.Deadline()
	w.cancelable = ctx.Done() != nil
	w.ctxErr = ctx.Err()
	if w.err != nil {
		return outbound.MicrosipPagoResult{}, w.err
	}
	return w.resultado, nil
}

// compile-time assertion: the capturing double really is a writer.
var _ outbound.MicrosipPagoWriter = (*ctxCapturingWriter)(nil)

// compile-time assertion for the counting writer above.
var _ outbound.MicrosipPagoWriter = (*concurrentSafeMicrosipWriter)(nil)
