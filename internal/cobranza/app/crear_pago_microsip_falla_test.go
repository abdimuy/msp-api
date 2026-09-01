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
		"MSP_PAGOS_RECIBIDOS: antes=%d despues=%d", antes.pagos, despues.pagos)
	assert.Equal(t, antes.imagenes, despues.imagenes,
		"MSP_PAGOS_IMAGENES: antes=%d despues=%d", antes.imagenes, despues.imagenes)
	assert.Equal(t, antes.blobs, despues.blobs,
		"blobs en disco: antes=%d despues=%d", antes.blobs, despues.blobs)
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

// clonePagoRecibido deep-copies a pago through the hydration constructor.
// Needed because the in-memory repo stores the very pointer the service
// mutates: restoring the map alone would not undo a RegistrarFallo, so a
// rollback double that only swaps maps would silently "pass".
func clonePagoRecibido(p *domain.PagoRecibido) *domain.PagoRecibido {
	aud := p.Audit()
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
		Lat:            p.Lat(),
		Lon:            p.Lon(),
		Sincronizacion: p.Sincronizacion(),
		Intentos:       p.Intentos(),
		UltimoError:    p.UltimoError(),
		DoctoCCID:      p.DoctoCCID(),
		ImpteDoctoCCID: p.ImpteDoctoCCID(),
		Folio:          p.Folio(),
		ReceivedAt:     p.ReceivedAt(),
		AplicadoAt:     p.AplicadoAt(),
		CreatedAt:      aud.CreatedAt(),
		UpdatedAt:      aud.UpdatedAt(),
		CreatedBy:      aud.CreatedBy(),
		UpdatedBy:      aud.UpdatedBy(),
		Imagenes:       p.ImagenesForRepo(),
	})
}

// rollbackPagosTxRunner models a REAL Firebird rollback over the pagos repo:
// it deep-clones every row before fn and restores the clones when fn returns
// an error. Anything written (or mutated) inside a failing closure vanishes,
// exactly like a rolled-back transaction.
//
// txCount is the load-bearing observation: persisting a failure record after
// the apply transaction rolls back requires a SECOND transaction.
type rollbackPagosTxRunner struct {
	repo    *fakePagosRecibidosRepo
	txCount int
}

func (r *rollbackPagosTxRunner) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	r.txCount++
	snap := make(map[uuid.UUID]*domain.PagoRecibido, len(r.repo.rows))
	for k, v := range r.repo.rows {
		snap[k] = clonePagoRecibido(v)
	}
	if err := fn(ctx); err != nil {
		r.repo.rows = snap
		return err
	}
	return nil
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

	// Paso 1: fakes + censo ANTES.
	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imgRepo := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	antes := tomarCenso(pagosRepo, imgRepo, store)

	// Paso 2: Microsip rechaza.
	writerErr := errors.New("microsip_docto_cc_rechazado")
	writer := &fakeMicrosipPagoWriter{err: writerErr}

	// Paso 3: el único doble que modela el rollback de verdad.
	txRunner := newSnapshottingTxRunner(pagosRepo, imgRepo)
	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		pagosRepo, imgRepo, writer, store, nil, txRunner,
	)

	// Paso 4: crear el pago con dos comprobantes.
	in := baseCrearInput(now)
	imgs := pdfUploads(in.ID, 2)
	pago, err := svc.CrearPagoConImagenes(context.Background(), in, imgs, uuid.New())

	// Paso 5: el rechazo propaga.
	require.Error(t, err, "un rechazo de Microsip no puede devolver 2xx")
	require.ErrorIs(t, err, writerErr)
	assert.Nil(t, pago)

	// Paso 6: censo DESPUÉS, tienda por tienda.
	despues := tomarCenso(pagosRepo, imgRepo, store)
	assertCensoIgual(t, antes, despues)

	// Paso 7: el escritor corrió una sola vez y los blobs se limpiaron.
	assert.Equal(t, 1, writer.callCount, "el escritor corre una vez, dentro de la tx")
	assert.Equal(t, 2, store.storeCalls, "ambos blobs se escribieron antes de la tx")
	assert.Equal(t, 2, store.deleteCalls, "ambos blobs se limpiaron tras el rollback")
}

// ─── 2. rejection propagates — without imagenes (the phone's path) ──────────

// TestCrearPagoConImagenes_MicrosipRechaza_SinImagenes_Propaga covers the
// path the phone actually takes: the multipart endpoint with zero image
// parts. Before the fix that path had no transaction at all — the INSERT
// committed on its own and the writer error was logged and dropped.
func TestCrearPagoConImagenes_MicrosipRechaza_SinImagenes_Propaga(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	// Paso 1: fakes + censo ANTES.
	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imgRepo := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	antes := tomarCenso(pagosRepo, imgRepo, store)

	// Paso 2: Microsip rechaza.
	writerErr := errors.New("microsip_saldo_cc_bloqueado")
	writer := &fakeMicrosipPagoWriter{err: writerErr}

	// Paso 3: runner con rollback real.
	txRunner := newSnapshottingTxRunner(pagosRepo, imgRepo)
	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		pagosRepo, imgRepo, writer, store, nil, txRunner,
	)

	// Paso 4: crear el pago SIN comprobantes.
	in := baseCrearInput(now)
	pago, err := svc.CrearPagoConImagenes(context.Background(), in, nil, uuid.New())

	// Paso 5: el rechazo propaga.
	require.Error(t, err, "el camino sin imágenes también debe propagar el rechazo")
	require.ErrorIs(t, err, writerErr)
	assert.Nil(t, pago)

	// Paso 6: censo DESPUÉS.
	despues := tomarCenso(pagosRepo, imgRepo, store)
	assertCensoIgual(t, antes, despues)

	// Paso 7: sin imágenes no se toca el almacenamiento.
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

	// Paso 1: primer intento — Microsip rechaza, todo se deshace.
	_, err := svc.CrearPagoConImagenes(context.Background(), in, nil, by)
	require.Error(t, err)
	require.ErrorIs(t, err, writerErr)
	assert.Empty(t, pagosRepo.rows, "el rollback no puede dejar la fila")

	// Paso 2: Microsip vuelve en sí.
	writer.err = nil
	writer.result = validWriterResult()

	// Paso 3: el teléfono reintenta con el MISMO UUID.
	pago, err := svc.CrearPagoConImagenes(context.Background(), in, nil, by)
	require.NoError(t, err, "el UUID no quedó quemado por el intento fallido")
	require.NotNil(t, pago)
	assert.Equal(t, in.ID, pago.ID())
	assert.True(t, pago.IsAplicada(), "el reintento sí llegó a Microsip")

	// Paso 4: censo — exactamente una fila, la buena.
	assert.Len(t, pagosRepo.rows, 1)
	assert.Equal(t, 2, writer.callCount, "un intento rechazado + un intento aceptado")
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

	// Paso 1: fakes + censo ANTES.
	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imgRepo := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	antes := tomarCenso(pagosRepo, imgRepo, store)

	// Paso 2: Microsip acepta el primero y rechaza el segundo.
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

	// Paso 3: los dos pagos de Juana, con segundos de diferencia.
	primero := baseCrearInput(now)
	primero.Importe = decimal.NewFromInt(importeAceptado)
	primero.FechaHoraPago = now.Add(-2 * time.Minute)

	segundo := baseCrearInput(now)
	segundo.ClienteID = primero.ClienteID
	segundo.CargoDoctoCCID = primero.CargoDoctoCCID
	segundo.Importe = decimal.NewFromInt(importeRechazado)
	segundo.FechaHoraPago = primero.FechaHoraPago.Add(8 * time.Second)

	// Paso 4: el primero entra.
	pagoA, errA := svc.CrearPagoConImagenes(context.Background(), primero, nil, by)
	require.NoError(t, errA, "el pago aceptado por Microsip debe entrar")
	require.NotNil(t, pagoA)
	assert.True(t, pagoA.IsAplicada())

	// Paso 5: al segundo Microsip lo rechaza — y el rechazo se ve.
	pagoB, errB := svc.CrearPagoConImagenes(context.Background(), segundo, nil, by)
	require.Error(t, errB,
		"el pago rechazado debe acabar visible: el error propaga para que la capa HTTP lo capture")
	require.ErrorIs(t, errB, rechazoErr)
	assert.Nil(t, pagoB)

	// Paso 6: censo DESPUÉS — sólo la fila del pago que sí llegó.
	despues := tomarCenso(pagosRepo, imgRepo, store)
	assert.Equal(t, antes.pagos+1, despues.pagos,
		"MSP_PAGOS_RECIBIDOS: sólo el pago aceptado deja fila (antes=%d despues=%d)",
		antes.pagos, despues.pagos)
	assert.Equal(t, antes.imagenes, despues.imagenes, "MSP_PAGOS_IMAGENES sin cambios")
	assert.Equal(t, antes.blobs, despues.blobs, "sin blobs")

	// Paso 7: la tabla de pagos queda SIN la fila del rechazado.
	guardadoA, findErrA := pagosRepo.FindByID(context.Background(), primero.ID)
	require.NoError(t, findErrA)
	assert.True(t, guardadoA.IsAplicada())

	_, findErrB := pagosRepo.FindByID(context.Background(), segundo.ID)
	require.ErrorIs(t, findErrB, domain.ErrPagoNoEncontrado,
		"el pago rechazado no puede quedarse en la cola invisible")

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

	// Paso 1: un pago pendiente ya confirmado en la tabla (camino worker).
	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)
	require.Equal(t, 0, pago.Intentos(), "censo ANTES: intentos=0")

	// Paso 2: Microsip rechaza.
	writerErr := errors.New("microsip_conexion_perdida")
	writer := &fakeMicrosipPagoWriter{err: writerErr}

	// Paso 3: runner que deshace de verdad lo escrito en una clausura fallida.
	runner := &rollbackPagosTxRunner{repo: repo}
	svc := newAplicarSvc(t, runner, repo, writer, fixedNow)

	// Paso 4: aplicar — debe fallar.
	_, err := svc.AplicarPago(context.Background(), pago.ID(), uuid.New())
	require.Error(t, err)
	require.ErrorIs(t, err, writerErr)

	// Paso 5: censo DESPUÉS — el registro del fallo sobrevivió.
	guardado, findErr := repo.FindByID(context.Background(), pago.ID())
	require.NoError(t, findErr)
	assert.True(t, guardado.IsPendiente(), "el pago sigue pendiente para el worker")
	assert.Equal(t, 1, guardado.Intentos(),
		"INTENTOS debe sobrevivir al rollback de la tx de aplicación")
	require.NotNil(t, guardado.UltimoError(), "ULTIMO_ERROR debe sobrevivir al rollback")
	assert.Contains(t, *guardado.UltimoError(), "microsip_conexion_perdida")

	// Paso 6: la prueba del mecanismo — hizo falta una segunda transacción.
	assert.Equal(t, 2, runner.txCount,
		"el registro del fallo va en su propia transacción, no en la que se deshace")
}
