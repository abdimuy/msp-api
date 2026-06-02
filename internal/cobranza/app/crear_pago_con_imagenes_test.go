//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
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

// ─── helpers ─────────────────────────────────────────────────────────────────

// newCrearConImagenesSvc wires every fake the new CrearPagoConImagenes flow
// touches. Pass writer=nil to leave AplicarPago in the legacy "best-effort
// fails, pago stays pendiente" mode; pass a real fake to exercise the
// full happy-path including Microsip apply.
func newCrearConImagenesSvc(
	t *testing.T,
	now time.Time,
	saldos *fakeSaldosRepo,
	pagosRecibidos *fakePagosRecibidosRepo,
	pagosImagenes *fakePagosImagenesRepo,
	storage *fakeStorageProvider,
	imageProc outbound.ImageProcessor,
	writer outbound.MicrosipPagoWriter,
) *app.Service {
	t.Helper()
	return app.NewService(
		saldos,
		newFakePagosRepo(),
		nil, // ventas — unused
		fixedClock{T: now},
		pagosRecibidos,
		pagosImagenes,
		writer,
		storage,
		imageProc,
		fakeTxRunner{},
	)
}

// baseCrearInput returns a CrearPagoInput consistent with cargo 5000 / saldo 5000.
func baseCrearInput(now time.Time) app.CrearPagoInput {
	return app.CrearPagoInput{
		ID:             uuid.New(),
		CargoDoctoCCID: 5000,
		ClienteID:      100,
		CobradorID:     200,
		Cobrador:       "Mendoza Torres, Ana",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   1,
		FechaHoraPago:  now.Add(-30 * time.Minute),
	}
}

// baseImagenUpload returns a valid ImagenUploadInput. Callers customize
// ImagenID / StorageKey / Mime / Body as needed.
func baseImagenUpload(pagoID uuid.UUID, mime string, payload []byte) app.ImagenUploadInput {
	id := uuid.New()
	return app.ImagenUploadInput{
		ImagenID:    id,
		StorageKind: domain.StorageKindFilesystem,
		StorageKey:  "pagos/" + pagoID.String() + "/" + id.String() + ".bin",
		Mime:        mime,
		SizeBytes:   int64(len(payload)),
		Body:        bytes.NewReader(payload),
	}
}

// seedCargoSaldo populates the fake saldos repo with cargo 5000 / saldo 5000.
func seedCargoSaldo(t *testing.T) *fakeSaldosRepo {
	t.Helper()
	saldos := newFakeSaldosRepo()
	s := makeSaldoForCargo(5000, decimal.NewFromInt(5000), false)
	saldos.byCargo[5000] = &s
	return saldos
}

// ─── happy paths ─────────────────────────────────────────────────────────────

func TestCrearPagoConImagenes_HappyPath_SinImagenes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	svc := newCrearConImagenesSvc(t, now, saldos, pagosRepo, imagenes, storage, nil, nil)

	in := baseCrearInput(now)
	pago, err := svc.CrearPagoConImagenes(context.Background(), in, nil, uuid.New())

	require.NoError(t, err)
	require.NotNil(t, pago)
	assert.Equal(t, in.ID, pago.ID())
	assert.Len(t, pagosRepo.rows, 1)
	assert.Empty(t, imagenes.images)
	// No blobs written because there were no imagenes.
	assert.Equal(t, 0, storage.storeCalls)
	assert.Equal(t, 0, storage.deleteCalls)
}

func TestCrearPagoConImagenes_HappyPath_1Imagen_PDF(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	svc := newCrearConImagenesSvc(t, now, saldos, pagosRepo, imagenes, storage, nil, nil)

	in := baseCrearInput(now)
	payload := bytes.Repeat([]byte("P"), 512)
	img := baseImagenUpload(in.ID, domain.MimePDF, payload)
	img.StorageKey = "pagos/" + in.ID.String() + "/" + img.ImagenID.String() + ".pdf"

	pago, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img}, uuid.New())

	require.NoError(t, err)
	require.NotNil(t, pago)
	assert.Len(t, pagosRepo.rows, 1, "exactly one pago persisted")
	assert.Len(t, imagenes.images, 1, "exactly one imagen persisted")
	assert.Equal(t, 1, storage.storeCalls, "one blob stored")
	assert.Equal(t, 0, storage.deleteCalls, "no cleanup on happy path")
	assert.Contains(t, storage.objects, img.StorageKey)
	assert.Equal(t, domain.MimePDF, storage.mimes[img.StorageKey])
}

func TestCrearPagoConImagenes_HappyPath_3Imagenes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	proc := &fakeImageProcessor{
		out: outbound.ImageProcessorOutput{
			ContentType: domain.MimeJPEG,
			SizeBytes:   256,
			Body:        bytes.NewReader(bytes.Repeat([]byte("Q"), 256)),
		},
	}
	svc := newCrearConImagenesSvc(t, now, saldos, pagosRepo, imagenes, storage, proc, nil)

	in := baseCrearInput(now)
	imgs := []app.ImagenUploadInput{
		baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("A"), 100)),
		baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("B"), 200)),
		baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("C"), 300)),
	}
	// Override storage keys to deterministic .pdf extensions.
	for i := range imgs {
		imgs[i].StorageKey = "pagos/" + in.ID.String() + "/" + imgs[i].ImagenID.String() + ".pdf"
	}

	pago, err := svc.CrearPagoConImagenes(context.Background(), in, imgs, uuid.New())

	require.NoError(t, err)
	require.NotNil(t, pago)
	assert.Len(t, pagosRepo.rows, 1)
	assert.Len(t, imagenes.images, 3)
	assert.Equal(t, 3, storage.storeCalls)
	assert.Equal(t, 0, storage.deleteCalls)
	for i := range imgs {
		assert.Contains(t, storage.objects, imgs[i].StorageKey, "imagen %d not in storage", i)
	}
}

// ─── rollback scenarios ─────────────────────────────────────────────────────

func TestCrearPagoConImagenes_RollbackOnInsertPagoFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	pagosRepo.insertErr = errors.New("fk_violation_clientes")
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	svc := newCrearConImagenesSvc(t, now, saldos, pagosRepo, imagenes, storage, nil, nil)

	in := baseCrearInput(now)
	imgs := []app.ImagenUploadInput{
		baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("X"), 50)),
		baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("Y"), 50)),
	}
	for i := range imgs {
		imgs[i].StorageKey = "pagos/" + in.ID.String() + "/" + imgs[i].ImagenID.String() + ".pdf"
	}

	_, err := svc.CrearPagoConImagenes(context.Background(), in, imgs, uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "fk_violation_clientes")
	// Atomicity invariant: 0 pagos, 0 imagenes, 0 blobs remaining.
	assert.Empty(t, pagosRepo.rows, "pago must NOT have been persisted")
	assert.Empty(t, imagenes.images, "no imagenes persisted")
	assert.Equal(t, 2, storage.storeCalls, "both blobs were written before tx")
	assert.Equal(t, 2, storage.deleteCalls, "both blobs cleaned up after rollback")
	assert.Empty(t, storage.objects, "no blobs remain in storage")
}

// snapshottingTxRunner snapshots the fake repos before fn runs and restores
// them on error — modeling real Firebird tx rollback. Used by atomicity tests
// so an InsertImagen failure mid-loop leaves no trace in either repo.
type snapshottingTxRunner struct {
	pagos    *fakePagosRecibidosRepo
	imagenes *fakePagosImagenesRepo
}

func newSnapshottingTxRunner(p *fakePagosRecibidosRepo, i *fakePagosImagenesRepo) *snapshottingTxRunner {
	return &snapshottingTxRunner{pagos: p, imagenes: i}
}

func (r *snapshottingTxRunner) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	pagoSnap := make(map[uuid.UUID]*domain.PagoRecibido, len(r.pagos.rows))
	for k, v := range r.pagos.rows {
		pagoSnap[k] = v
	}
	imgSnap := make(map[uuid.UUID]*domain.Imagen, len(r.imagenes.images))
	for k, v := range r.imagenes.images {
		imgSnap[k] = v
	}
	byPagoSnap := make(map[uuid.UUID][]uuid.UUID, len(r.imagenes.byPago))
	for k, v := range r.imagenes.byPago {
		cp := make([]uuid.UUID, len(v))
		copy(cp, v)
		byPagoSnap[k] = cp
	}

	err := fn(ctx)
	if err != nil {
		r.pagos.rows = pagoSnap
		r.imagenes.images = imgSnap
		r.imagenes.byPago = byPagoSnap
	}
	return err
}

func TestCrearPagoConImagenes_RollbackOnInsertImagenFails_LastImgErrors(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imgRepo := &countingImagenRepo{
		fakePagosImagenesRepo: newFakePagosImagenesRepo(),
		failOnCall:            3, // 1-indexed: third InsertImagen fails
		failErr:               errors.New("constraint_violation_imagen"),
	}
	store := newFakeStorageProvider()
	txRunner := newSnapshottingTxRunner(pagosRepo, imgRepo.fakePagosImagenesRepo)

	svc := app.NewService(
		saldos,
		newFakePagosRepo(),
		nil,
		fixedClock{T: now},
		pagosRepo,
		imgRepo,
		nil,
		store,
		nil, // imageProc not needed for PDF
		txRunner,
	)

	in := baseCrearInput(now)
	imgs := []app.ImagenUploadInput{
		baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("A"), 100)),
		baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("B"), 100)),
		baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("C"), 100)),
	}
	for i := range imgs {
		imgs[i].StorageKey = "pagos/" + in.ID.String() + "/" + imgs[i].ImagenID.String() + ".pdf"
	}

	_, err := svc.CrearPagoConImagenes(context.Background(), in, imgs, uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "constraint_violation_imagen")
	// Atomicity: pago + ALL imagenes rolled back; all 3 blobs cleaned up.
	assert.Empty(t, pagosRepo.rows, "pago rolled back")
	assert.Empty(t, imgRepo.images, "all imagenes rolled back, including the 2 that succeeded")
	assert.Equal(t, 3, store.storeCalls, "all 3 blobs were stored before tx")
	assert.Equal(t, 3, store.deleteCalls, "all 3 blobs cleaned up after rollback")
	assert.Empty(t, store.objects, "no blobs remain in storage")
}

// countingImagenRepo fails the n-th InsertImagen call.
type countingImagenRepo struct {
	*fakePagosImagenesRepo
	failOnCall int // 1-indexed
	failErr    error
	insertN    int
}

func (c *countingImagenRepo) InsertImagen(ctx context.Context, pagoID uuid.UUID, img *domain.Imagen) error {
	c.insertN++
	if c.insertN == c.failOnCall {
		return c.failErr
	}
	return c.fakePagosImagenesRepo.InsertImagen(ctx, pagoID, img)
}

// ─── storage-side failures ───────────────────────────────────────────────────

func TestCrearPagoConImagenes_StorageStoreFails_NoTxStarted(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	store := &flakyStorage{
		fakeStorageProvider: newFakeStorageProvider(),
		failOnCall:          2, // 1-indexed: 2nd Store fails (1st succeeded)
		failErr:             errors.New("disk_full"),
	}
	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		pagosRepo, imagenes, nil, store, nil, fakeTxRunner{},
	)

	in := baseCrearInput(now)
	imgs := []app.ImagenUploadInput{
		baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("A"), 100)),
		baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("B"), 100)),
	}
	for i := range imgs {
		imgs[i].StorageKey = "pagos/" + in.ID.String() + "/" + imgs[i].ImagenID.String() + ".pdf"
	}

	_, err := svc.CrearPagoConImagenes(context.Background(), in, imgs, uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk_full")
	// Tx never opened: pago not persisted, imagenes not persisted.
	assert.Empty(t, pagosRepo.rows)
	assert.Empty(t, imagenes.images)
	// 1st Store succeeded, 2nd failed. The succeeded blob must be cleaned up.
	assert.Equal(t, 2, store.storeCalls)
	assert.Equal(t, 1, store.deleteCalls, "only the successfully-stored blob is cleaned up")
}

// flakyStorage fails the n-th Store call.
type flakyStorage struct {
	*fakeStorageProvider
	failOnCall int // 1-indexed
	failErr    error
	storeN     int
}

func (f *flakyStorage) Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error {
	f.storeN++
	if f.storeN == f.failOnCall {
		// Still count it as a Store call so the underlying counter increments.
		f.storeCalls++
		return f.failErr
	}
	return f.fakeStorageProvider.Store(ctx, key, contentType, sizeBytes, body)
}

func TestCrearPagoConImagenes_CleanupBestEffort_DeleteFailsLogged(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	pagosRepo.insertErr = errors.New("oops")
	imagenes := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	store.deleteErr = errors.New("disk_io_during_cleanup")

	svc := newCrearConImagenesSvc(t, now, saldos, pagosRepo, imagenes, store, nil, nil)

	in := baseCrearInput(now)
	img := baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("Z"), 50))
	img.StorageKey = "pagos/" + in.ID.String() + "/" + img.ImagenID.String() + ".pdf"

	_, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img}, uuid.New())

	require.Error(t, err)
	// Original error is preserved — cleanup failure does not mask it.
	assert.Contains(t, err.Error(), "oops")
	assert.NotContains(t, err.Error(), "disk_io_during_cleanup")
	assert.Equal(t, 1, store.deleteCalls, "delete was attempted")
}

// ─── idempotency ────────────────────────────────────────────────────────────

func TestCrearPagoConImagenes_Idempotent_FastPath(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	svc := newCrearConImagenesSvc(t, now, saldos, pagosRepo, imagenes, store, nil, nil)

	in := baseCrearInput(now)
	img1 := baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("A"), 100))
	img1.StorageKey = "pagos/" + in.ID.String() + "/" + img1.ImagenID.String() + ".pdf"

	// First call: inserts pago + 1 imagen.
	first, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img1}, uuid.New())
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, 1, store.storeCalls)
	assert.Equal(t, 0, store.deleteCalls)

	// Second call: same pago UUID, different (fresh) imagen UUID. Idempotency
	// kicks in at the pago level — existing pago is returned, and the new
	// imagen blob must be cleaned up (we cannot attach to an existing pago via
	// this endpoint; the caller must use AdjuntarImagenPago).
	img2 := baseImagenUpload(in.ID, domain.MimePDF, bytes.Repeat([]byte("B"), 100))
	img2.StorageKey = "pagos/" + in.ID.String() + "/" + img2.ImagenID.String() + ".pdf"

	second, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img2}, uuid.New())
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, first.ID(), second.ID(), "same pago returned")

	// Still exactly 1 pago, 1 imagen in the repo.
	assert.Len(t, pagosRepo.rows, 1)
	assert.Len(t, imagenes.images, 1)
	// The 2nd Store happened, then was cleaned up.
	assert.Equal(t, 2, store.storeCalls)
	assert.Equal(t, 1, store.deleteCalls)
}

// ─── PDF + image processor paths ────────────────────────────────────────────

func TestCrearPagoConImagenes_PDFShortCircuit(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	// imageProc nil — PDF must NOT touch it.
	svc := newCrearConImagenesSvc(t, now, saldos, pagosRepo, imagenes, store, nil, nil)

	in := baseCrearInput(now)
	payload := bytes.Repeat([]byte("P"), 1024)
	img := baseImagenUpload(in.ID, domain.MimePDF, payload)
	img.StorageKey = "pagos/" + in.ID.String() + "/" + img.ImagenID.String() + ".pdf"

	pago, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img}, uuid.New())

	require.NoError(t, err)
	require.NotNil(t, pago)
	assert.Equal(t, domain.MimePDF, store.mimes[img.StorageKey])
	assert.Equal(t, payload, store.objects[img.StorageKey], "PDF stored verbatim, processor bypassed")
}

func TestCrearPagoConImagenes_ProcessorFails_NoSideEffects(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	proc := &fakeImageProcessor{err: errors.New("decode_failed")}
	svc := newCrearConImagenesSvc(t, now, saldos, pagosRepo, imagenes, store, proc, nil)

	in := baseCrearInput(now)
	img := baseImagenUpload(in.ID, domain.MimeJPEG, bytes.Repeat([]byte("J"), 100))
	img.StorageKey = "pagos/" + in.ID.String() + "/" + img.ImagenID.String() + ".jpg"

	_, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img}, uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode_failed")
	assert.Empty(t, pagosRepo.rows)
	assert.Empty(t, imagenes.images)
	assert.Equal(t, 0, store.storeCalls, "store never called when processor fails")
	assert.Equal(t, 0, store.deleteCalls)
}

// ─── duplicate detection ────────────────────────────────────────────────────

func TestCrearPagoConImagenes_DuplicateImagenIDs_Rejected(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	svc := newCrearConImagenesSvc(t, now, saldos, pagosRepo, imagenes, store, nil, nil)

	in := baseCrearInput(now)
	dupID := uuid.New()
	img1 := baseImagenUpload(in.ID, domain.MimePDF, []byte("A"))
	img1.ImagenID = dupID
	img1.StorageKey = "pagos/" + in.ID.String() + "/" + dupID.String() + "-1.pdf"
	img2 := baseImagenUpload(in.ID, domain.MimePDF, []byte("B"))
	img2.ImagenID = dupID
	img2.StorageKey = "pagos/" + in.ID.String() + "/" + dupID.String() + "-2.pdf"

	_, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img1, img2}, uuid.New())

	require.ErrorIs(t, err, domain.ErrImagenIDDuplicado)
	assert.Empty(t, pagosRepo.rows)
	assert.Empty(t, imagenes.images)
	assert.Equal(t, 0, store.storeCalls, "no side effects when duplicates detected up-front")
}

// ─── deps-missing ───────────────────────────────────────────────────────────

func TestCrearPagoConImagenes_DepsMissing_PagosRecibidos(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	saldos := seedCargoSaldo(t)
	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		nil, // pagosRecibidos
		newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{},
	)
	_, err := svc.CrearPagoConImagenes(context.Background(), baseCrearInput(now), nil, uuid.New())
	require.Error(t, err)
}

func TestCrearPagoConImagenes_DepsMissing_PagosImagenes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	saldos := seedCargoSaldo(t)
	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		newFakePagosRecibidosRepo(),
		nil, // pagosImagenes
		nil, newFakeStorageProvider(), nil, fakeTxRunner{},
	)
	in := baseCrearInput(now)
	img := baseImagenUpload(in.ID, domain.MimePDF, []byte("x"))
	img.StorageKey = "pagos/" + in.ID.String() + "/img.pdf"
	_, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img}, uuid.New())
	require.Error(t, err)
}

func TestCrearPagoConImagenes_DepsMissing_Storage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	saldos := seedCargoSaldo(t)
	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil,
		nil, // storage
		nil, fakeTxRunner{},
	)
	in := baseCrearInput(now)
	img := baseImagenUpload(in.ID, domain.MimePDF, []byte("x"))
	img.StorageKey = "pagos/" + in.ID.String() + "/img.pdf"
	_, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img}, uuid.New())
	require.Error(t, err)
}

func TestCrearPagoConImagenes_DepsMissing_TxRunner(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	saldos := seedCargoSaldo(t)
	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil,
		nil, // txMgr
	)
	in := baseCrearInput(now)
	img := baseImagenUpload(in.ID, domain.MimePDF, []byte("x"))
	img.StorageKey = "pagos/" + in.ID.String() + "/img.pdf"
	_, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img}, uuid.New())
	require.Error(t, err)
}

// ─── validation propagation ─────────────────────────────────────────────────

func TestCrearPagoConImagenes_FechaHoraFutura_RejectedBeforeAnyWrite(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	svc := newCrearConImagenesSvc(t, now, saldos, pagosRepo, imagenes, store, nil, nil)

	in := baseCrearInput(now)
	in.FechaHoraPago = now.Add(10 * time.Minute)
	img := baseImagenUpload(in.ID, domain.MimePDF, []byte("x"))
	img.StorageKey = "pagos/" + in.ID.String() + "/img.pdf"

	_, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img}, uuid.New())

	require.ErrorIs(t, err, domain.ErrPagoFechaFutura)
	assert.Equal(t, 0, store.storeCalls, "validation must run before any side effect")
	assert.Empty(t, pagosRepo.rows)
}

func TestCrearPagoConImagenes_StorageKeyInvalid_OnFirstImg_NoBlobs(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	saldos := seedCargoSaldo(t)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	svc := newCrearConImagenesSvc(t, now, saldos, pagosRepo, imagenes, store, nil, nil)

	in := baseCrearInput(now)
	img := baseImagenUpload(in.ID, domain.MimePDF, []byte("x"))
	img.StorageKey = "../etc/passwd"

	_, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img}, uuid.New())

	require.ErrorIs(t, err, domain.ErrStorageKeyInvalida)
	assert.Equal(t, 0, store.storeCalls)
	assert.Empty(t, pagosRepo.rows)
}
