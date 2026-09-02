//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// This file covers the single invariant that keeps a cobrador from charging a
// client twice: a pago Microsip REJECTED must never be persisted as an
// invisible ESTADO='P' row behind a 2xx. The rejection has to propagate all
// the way out of the service so the HTTP layer can turn it into a failure the
// phone (and MSP_FAILED_INTENTS) actually sees.
//
// Census discipline is borrowed from internal/ventas/infra/ventfb/atomicity_test.go:
// numbered steps, one census before, one after, compared entry by entry.
//
// The rollback double is snapshottingTxRunner, defined once in
// crear_pago_con_imagenes_test.go. It clones pagos in depth, so a write that
// happens inside a failing closure really does disappear.

// ─── census helpers ─────────────────────────────────────────────────────────

// censo is the row count of every store CrearPagoConImagenes can write to.
// The in-memory equivalent of the table-by-table snapshot the ventfb
// atomicity tests take against Firebird.
type censo struct {
	pagos    int
	imagenes int
	blobs    int
}

// tomarCenso counts every store in one pass. Call once before the exercise
// and once after, then compare with assertCensoIgual.
func tomarCenso(
	pagos *fakePagosRecibidosRepo, imgs *fakePagosImagenesRepo, store *fakeStorageProvider,
) censo {
	return censo{
		pagos:    len(pagos.rows),
		imagenes: len(imgs.images),
		blobs:    len(store.objects),
	}
}

// assertCensoIgual compares two censuses store by store, naming the store in
// the failure message so a diff points at the leak instead of at a bare int.
func assertCensoIgual(t *testing.T, antes, despues censo) {
	t.Helper()
	assert.Equal(t, antes.pagos, despues.pagos,
		"MSP_PAGOS_RECIBIDOS: before=%d after=%d", antes.pagos, despues.pagos)
	assert.Equal(t, antes.imagenes, despues.imagenes,
		"MSP_PAGOS_IMAGENES: before=%d after=%d", antes.imagenes, despues.imagenes)
	assert.Equal(t, antes.blobs, despues.blobs,
		"blobs on disk: before=%d after=%d", antes.blobs, despues.blobs)
}

// ─── doubles ────────────────────────────────────────────────────────────────

// rejectingWriterByImporte accepts every pago except the one whose importe
// matches rejectImporte. Models Microsip refusing exactly one document out of
// several sent seconds apart — the shape of the Juana incident.
type rejectingWriterByImporte struct {
	rejectImporte decimal.Decimal
	err           error
	result        outbound.MicrosipPagoResult
	accepted      int
	rejected      int
}

func (w *rejectingWriterByImporte) Aplicar(
	_ context.Context, in outbound.MicrosipPagoInput,
) (outbound.MicrosipPagoResult, error) {
	if in.Importe.Equal(w.rejectImporte) {
		w.rejected++
		return outbound.MicrosipPagoResult{}, w.err
	}
	w.accepted++
	return w.result, nil
}

// pdfUploads builds n valid PDF uploads for pagoID with deterministic keys.
func pdfUploads(pagoID uuid.UUID, n int) []app.ImagenUploadInput {
	imgs := make([]app.ImagenUploadInput, n)
	for i := range imgs {
		imgs[i] = baseImagenUpload(pagoID, domain.MimePDF, bytes.Repeat([]byte{byte('A' + i)}, 64))
		imgs[i].StorageKey = "pagos/" + pagoID.String() + "/" + imgs[i].ImagenID.String() + ".pdf"
	}
	return imgs
}

// ─── 1. rejection propagates — with imagenes ────────────────────────────────

// TestCrearPagoConImagenes_MicrosipRechaza_ConImagenes_Propaga proves the
// rejection reaches the caller and leaves nothing behind: no pago row, no
// imagen row, no blob. Before the fix the writer ran AFTER the transaction
// committed and its error was swallowed, so the pago survived as an
// invisible ESTADO='P' row while the client got a 2xx.
func TestCrearPagoConImagenes_MicrosipRechaza_ConImagenes_Propaga(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	// Step 1: fakes + census BEFORE.
	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imgRepo := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	antes := tomarCenso(pagosRepo, imgRepo, store)

	// Step 2: Microsip rejects.
	writerErr := errors.New("microsip_docto_cc_rechazado")
	writer := &fakeMicrosipPagoWriter{err: writerErr}

	// Step 3: the double that models a real rollback.
	txRunner := newSnapshottingTxRunner(pagosRepo, imgRepo)
	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		pagosRepo, imgRepo, writer, store, nil, txRunner,
	)

	// Step 4: create the pago with two comprobantes.
	in := baseCrearInput(now)
	imgs := pdfUploads(in.ID, 2)
	pago, err := svc.CrearPagoConImagenes(context.Background(), in, imgs, uuid.New())

	// Step 5: the rejection propagates.
	require.Error(t, err, "a Microsip rejection must not answer 2xx")
	require.ErrorIs(t, err, writerErr)
	assert.Nil(t, pago)

	// Step 6: census AFTER, store by store.
	despues := tomarCenso(pagosRepo, imgRepo, store)
	assertCensoIgual(t, antes, despues)

	// Step 7: the writer ran once and the blobs were cleaned up.
	assert.Equal(t, 1, writer.callCount, "the writer runs once, inside the tx")
	assert.Equal(t, 2, store.storeCalls, "both blobs were written before the tx")
	assert.Equal(t, 2, store.deleteCalls, "both blobs cleaned up after the rollback")
}

// ─── 2. rejection propagates — without imagenes (the phone's path) ──────────

// TestCrearPagoConImagenes_MicrosipRechaza_SinImagenes_Propaga covers the
// path the phone actually takes: the multipart endpoint with zero image
// parts. Before the fix that path had no transaction at all — the INSERT
// committed on its own and the writer error was logged and dropped.
func TestCrearPagoConImagenes_MicrosipRechaza_SinImagenes_Propaga(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	// Step 1: fakes + census BEFORE.
	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imgRepo := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	antes := tomarCenso(pagosRepo, imgRepo, store)

	// Step 2: Microsip rejects.
	writerErr := errors.New("microsip_saldo_cc_bloqueado")
	writer := &fakeMicrosipPagoWriter{err: writerErr}

	// Step 3: runner with a real rollback.
	txRunner := newSnapshottingTxRunner(pagosRepo, imgRepo)
	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		pagosRepo, imgRepo, writer, store, nil, txRunner,
	)

	// Step 4: create the pago WITHOUT comprobantes.
	in := baseCrearInput(now)
	pago, err := svc.CrearPagoConImagenes(context.Background(), in, nil, uuid.New())

	// Step 5: the rejection propagates.
	require.Error(t, err, "the no-images path must propagate the rejection too")
	require.ErrorIs(t, err, writerErr)
	assert.Nil(t, pago)

	// Step 6: census AFTER.
	despues := tomarCenso(pagosRepo, imgRepo, store)
	assertCensoIgual(t, antes, despues)

	// Step 7: with no images, storage is never touched.
	assert.Equal(t, 1, writer.callCount)
	assert.Equal(t, 0, store.storeCalls)
	assert.Equal(t, 0, store.deleteCalls)
}

// ─── 3. idempotency survives a Microsip rollback ────────────────────────────

// TestCrearPago_TrasRechazoDeMicrosip_UUIDNoQuemado proves the phone can
// retry: the rolled-back attempt leaves no row, so the same UUID goes through
// on the next try instead of being swallowed by the idempotency fast-path
// and returning a pago that never reached Microsip.
func TestCrearPago_TrasRechazoDeMicrosip_UUIDNoQuemado(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	by := uuid.New()

	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imgRepo := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()

	writerErr := errors.New("microsip_timeout")
	writer := &fakeMicrosipPagoWriter{err: writerErr}
	txRunner := newSnapshottingTxRunner(pagosRepo, imgRepo)
	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		pagosRepo, imgRepo, writer, store, nil, txRunner,
	)

	in := baseCrearInput(now)

	// Step 1: first attempt — Microsip rejects, everything is undone.
	_, err := svc.CrearPagoConImagenes(context.Background(), in, nil, by)
	require.Error(t, err)
	require.ErrorIs(t, err, writerErr)
	assert.Empty(t, pagosRepo.rows, "the rollback must not leave the row behind")

	// Step 2: Microsip comes back.
	writer.err = nil
	writer.result = validWriterResult()

	// Step 3: the phone retries with the SAME UUID.
	pago, err := svc.CrearPagoConImagenes(context.Background(), in, nil, by)
	require.NoError(t, err, "the failed attempt must not burn the UUID")
	require.NotNil(t, pago)
	assert.Equal(t, in.ID, pago.ID())
	assert.True(t, pago.IsAplicada(), "the retry did reach Microsip")

	// Step 4: census — exactly one row, the good one.
	assert.Len(t, pagosRepo.rows, 1)
	assert.Equal(t, 2, writer.callCount, "one rejected attempt + one accepted attempt")
}

// ─── 4. the Juana case ──────────────────────────────────────────────────────

// TestCrearPago_CasoJuana_ElRechazadoAcabaVisible is the incident, verbatim:
// two pagos from the same cliente seconds apart, one accepted by Microsip and
// one rejected. The rejected one must NOT end up as a silent pendiente row
// that nobody looks at — the error has to reach the caller so the HTTP layer
// can capture it, and MSP_PAGOS_RECIBIDOS must be left without its row.
//
// The old behavior is what makes a cobrador charge twice: Juana pays, the
// second pago is rejected, the API answers 2xx, and the row sits invisible in
// ESTADO='P' until somebody re-collects the same money.
func TestCrearPago_CasoJuana_ElRechazadoAcabaVisible(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	by := uuid.New()

	// Step 1: fakes + census BEFORE.
	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imgRepo := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	antes := tomarCenso(pagosRepo, imgRepo, store)

	// Step 2: Microsip accepts the first and rejects the second.
	rechazoErr := errors.New("microsip_docto_cc_duplicado")
	const importeAceptado = 1500
	const importeRechazado = 2300
	writer := &rejectingWriterByImporte{
		rejectImporte: decimal.NewFromInt(importeRechazado),
		err:           rechazoErr,
		result:        validWriterResult(),
	}
	txRunner := newSnapshottingTxRunner(pagosRepo, imgRepo)
	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		pagosRepo, imgRepo, writer, store, nil, txRunner,
	)

	// Step 3: Juana's two pagos, seconds apart.
	primero := baseCrearInput(now)
	primero.Importe = decimal.NewFromInt(importeAceptado)
	primero.FechaHoraPago = now.Add(-2 * time.Minute)

	segundo := baseCrearInput(now)
	segundo.ClienteID = primero.ClienteID
	segundo.CargoDoctoCCID = primero.CargoDoctoCCID
	segundo.Importe = decimal.NewFromInt(importeRechazado)
	segundo.FechaHoraPago = primero.FechaHoraPago.Add(8 * time.Second)

	// Step 4: the first one goes through.
	pagoA, errA := svc.CrearPagoConImagenes(context.Background(), primero, nil, by)
	require.NoError(t, errA, "the pago Microsip accepted must go through")
	require.NotNil(t, pagoA)
	assert.True(t, pagoA.IsAplicada())

	// Step 5: Microsip rejects the second — and the rejection is visible.
	pagoB, errB := svc.CrearPagoConImagenes(context.Background(), segundo, nil, by)
	require.Error(t, errB,
		"the rejected pago must end up visible: the error propagates so the HTTP layer can capture it")
	require.ErrorIs(t, errB, rechazoErr)
	assert.Nil(t, pagoB)

	// Step 6: census AFTER — only the row of the pago that actually landed.
	despues := tomarCenso(pagosRepo, imgRepo, store)
	assertCensoIgual(t, censo{pagos: antes.pagos + 1, imagenes: antes.imagenes, blobs: antes.blobs}, despues)

	// Step 7: the pagos table is left WITHOUT the rejected row.
	guardadoA, findErrA := pagosRepo.FindByID(context.Background(), primero.ID)
	require.NoError(t, findErrA)
	assert.True(t, guardadoA.IsAplicada())

	_, findErrB := pagosRepo.FindByID(context.Background(), segundo.ID)
	require.ErrorIs(t, findErrB, domain.ErrPagoNoEncontrado,
		"the rejected pago must not stay in the invisible queue")

	assert.Equal(t, 1, writer.accepted)
	assert.Equal(t, 1, writer.rejected)
}

// ─── 5. the failure record must survive the rollback ────────────────────────

// TestAplicarPago_WriterFalla_ElFalloSobreviveAlRollback is the direct
// positive control for the second defect: RegistrarFallo + Update used to run
// inside the very transaction that then rolled back, so INTENTOS and
// ULTIMO_ERROR were never persisted and the retry worker kept seeing
// intentos=0 forever (no backoff, no diagnosis).
//
// The rollback double restores deep clones, so a failure record written
// inside the apply transaction disappears. Only a record written in its own
// transaction survives — hence txCount must be 2.
func TestAplicarPago_WriterFalla_ElFalloSobreviveAlRollback(t *testing.T) {
	t.Parallel()

	// Step 1: a pendiente pago already committed to the table (worker path).
	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)
	require.Equal(t, 0, pago.Intentos(), "census BEFORE: intentos=0")

	// Step 2: Microsip rejects.
	writerErr := errors.New("microsip_conexion_perdida")
	writer := &fakeMicrosipPagoWriter{err: writerErr}

	// Step 3: a runner that really undoes what a failing closure wrote.
	runner := newSnapshottingTxRunner(repo, newFakePagosImagenesRepo())
	svc := newAplicarSvc(t, runner, repo, writer, fixedNow)

	// Step 4: apply — must fail.
	_, err := svc.AplicarPago(context.Background(), pago.ID(), uuid.New())
	require.Error(t, err)
	require.ErrorIs(t, err, writerErr)

	// Step 5: census AFTER — the failure record survived.
	guardado, findErr := repo.FindByID(context.Background(), pago.ID())
	require.NoError(t, findErr)
	assert.True(t, guardado.IsPendiente(), "the pago stays pendiente for the worker")
	assert.Equal(t, 1, guardado.Intentos(),
		"INTENTOS must survive the rollback of the apply tx")
	require.NotNil(t, guardado.UltimoError(), "ULTIMO_ERROR must survive the rollback")
	assert.Contains(t, *guardado.UltimoError(), "microsip_conexion_perdida")

	// Step 6: proof of the mechanism — a second transaction was needed, and
	// each of the two took the row lock. Counting the transactions alone does
	// not say the second one is safe: a second transaction that re-read
	// WITHOUT the lock would pass every assertion above and still lose the
	// race described in the next test.
	assert.Equal(t, 2, runner.txCount,
		"the failure record goes in its own transaction, not the one that rolls back")
	assert.Equal(t, 2, repo.lockCnt,
		"both transactions must take the row lock: the apply, and the failure record that follows it")
}

// ─── 6. the failure record must not resurrect an applied pago ───────────────

// TestAplicarPago_FalloNoRevierteUnPagoYaAplicado is the positive control for
// the double-charge this design can produce if the second transaction writes
// blind.
//
// The window: the apply tx rolls back and RELEASES the row lock. Before the
// failure record is written, another caller (AplicarPagoForzar, a second
// worker) takes the lock, applies the pago for real and commits. If the
// failure record then writes back the snapshot it read before the lock was
// released, the repo's blind UPDATE ... WHERE ID resets ESTADO to 'P' and
// wipes DOCTO_CC_ID / IMPTE_DOCTO_CC_ID / FOLIO / APLICADO_AT. The next tick
// sees a pendiente and sends it to Microsip AGAIN — PagoWriter.Aplicar
// deduplicates nothing, so the client is charged twice.
//
// The fix is that the second transaction re-takes the lock, re-reads, and
// writes nothing when the pago is already aplicada.
func TestAplicarPago_FalloNoRevierteUnPagoYaAplicado(t *testing.T) {
	t.Parallel()

	// Step 1: a pendiente pago, and Microsip rejecting it.
	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)
	writerErr := errors.New("microsip_timeout")
	writer := &fakeMicrosipPagoWriter{err: writerErr}

	runner := newSnapshottingTxRunner(repo, newFakePagosImagenesRepo())

	// Step 2: between the apply tx and the failure-record tx, somebody else
	// wins the lock, applies the pago and commits. Modeled as a brand-new
	// aggregate in the repo, the way a competing connection would leave it.
	const doctoCCID = 7777
	const impteDoctoCCID = 7778
	runner.beforeTx = func(txN int) {
		if txN != 2 {
			return
		}
		repo.rows[pago.ID()] = aplicadoTwin(pago, doctoCCID, impteDoctoCCID, "FORZ-001")
	}

	svc := newAplicarSvc(t, runner, repo, writer, fixedNow)

	// Step 3: the worker's apply fails, as before.
	_, err := svc.AplicarPago(context.Background(), pago.ID(), uuid.New())
	require.Error(t, err)
	require.ErrorIs(t, err, writerErr)

	// Step 4: the winner's state must be intact. A blind write would have
	// reset all four of these.
	guardado, findErr := repo.FindByID(context.Background(), pago.ID())
	require.NoError(t, findErr)
	assert.True(t, guardado.IsAplicada(),
		"the failure record must not resurrect a pago somebody else applied")
	require.NotNil(t, guardado.DoctoCCID(), "DOCTO_CC_ID must not be erased")
	assert.Equal(t, doctoCCID, *guardado.DoctoCCID())
	require.NotNil(t, guardado.ImpteDoctoCCID(), "IMPTE_DOCTO_CC_ID must not be erased")
	assert.Equal(t, impteDoctoCCID, *guardado.ImpteDoctoCCID())
	require.NotNil(t, guardado.Folio(), "FOLIO must not be erased")
	assert.Equal(t, "FORZ-001", *guardado.Folio())
	assert.NotNil(t, guardado.AplicadoAt(), "APLICADO_AT must not be erased")

	// Step 5: nothing was written at all — no Update, and no attempt counted
	// against a pago that succeeded.
	assert.Equal(t, 0, repo.updateCnt,
		"an applied pago earns no failure record")
	assert.Equal(t, 0, guardado.Intentos())
	assert.Equal(t, 2, runner.txCount, "the failure path still opened its own tx")

	// Step 6: the lock itself, not just its result. Everything above pins
	// STATE — and a failure path that re-read the row without locking it
	// would produce exactly the same state on this single-threaded fake,
	// while losing the race against a real concurrent AplicarPagoForzar. The
	// count is what makes the serialization claim in AplicarPago's doc
	// comment ("EVERY write to the row happens under the row lock, including
	// the failure record in step 4, which re-acquires it") an assertion
	// instead of prose.
	assert.Equal(t, 2, repo.lockCnt,
		"the failure record must RE-TAKE the lock before re-reading, not read unlocked")
}

// aplicadoTwin returns a NEW aggregate carrying the same identity as p but
// already aplicada — what a competing connection leaves committed in the row.
// A new object, not a mutation of p, because the point of the test is that
// the caller is holding a stale snapshot.
func aplicadoTwin(p *domain.PagoRecibido, doctoCCID, impteDoctoCCID int, folio string) *domain.PagoRecibido {
	aud := p.Audit()
	aplicadoAt := fixedNow
	return domain.HydratePagoRecibido(domain.HydratePagoRecibidoParams{
		ID:             p.ID(),
		CargoDoctoCCID: p.CargoDoctoCCID(),
		ClienteID:      p.ClienteID(),
		CobradorID:     p.CobradorID(),
		Cobrador:       p.Cobrador(),
		Importe:        p.Importe(),
		FormaCobroID:   p.FormaCobroID(),
		ConceptoCCID:   p.ConceptoCCID(),
		FechaHoraPago:  p.FechaHoraPago(),
		Sincronizacion: domain.SincronizacionAplicada,
		DoctoCCID:      &doctoCCID,
		ImpteDoctoCCID: &impteDoctoCCID,
		Folio:          &folio,
		ReceivedAt:     p.ReceivedAt(),
		AplicadoAt:     &aplicadoAt,
		CreatedAt:      aud.CreatedAt(),
		UpdatedAt:      aplicadoAt,
		CreatedBy:      aud.CreatedBy(),
		UpdatedBy:      uuid.New(),
	})
}

// ─── 7. refusing to run inside somebody else's transaction ──────────────────

// TestAplicarPago_DentroDeOtraTx_Rechaza pins the guard that keeps the
// failure record writable. If AplicarPago joined an outer transaction, its
// own "separate" transaction would nest into that one and the failure record
// would roll back with it — silently, which is how this class of defect
// survives.
func TestAplicarPago_DentroDeOtraTx_Rechaza(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)
	writer := &fakeMicrosipPagoWriter{result: validWriterResult()}

	svc := newAplicarSvc(t, insideTxRunner{}, repo, writer, fixedNow)

	_, err := svc.AplicarPago(context.Background(), pago.ID(), uuid.New())

	require.ErrorIs(t, err, app.ErrAplicarPagoDentroDeTx)
	assert.Equal(t, 0, writer.callCount, "it must refuse before touching Microsip")
	assert.Equal(t, 0, repo.updateCnt)
}
