//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
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

// ─── fakePagosImagenesRepo ────────────────────────────────────────────────────

// fakePagosImagenesRepo is an in-memory outbound.PagosImagenesRepo for unit
// tests. It stores imagenes by ID and preserves insertion order via byPago.
type fakePagosImagenesRepo struct {
	images    map[uuid.UUID]*domain.Imagen
	byPago    map[uuid.UUID][]uuid.UUID
	insertErr error
	deleteErr error
	findErr   error
}

// compile-time check: fakePagosImagenesRepo must satisfy the port.
var _ outbound.PagosImagenesRepo = (*fakePagosImagenesRepo)(nil)

func newFakePagosImagenesRepo() *fakePagosImagenesRepo {
	return &fakePagosImagenesRepo{
		images: map[uuid.UUID]*domain.Imagen{},
		byPago: map[uuid.UUID][]uuid.UUID{},
	}
}

func (f *fakePagosImagenesRepo) InsertImagen(_ context.Context, pagoID uuid.UUID, img *domain.Imagen) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.images[img.ID()] = img
	f.byPago[pagoID] = append(f.byPago[pagoID], img.ID())
	return nil
}

func (f *fakePagosImagenesRepo) DeleteImagen(_ context.Context, imagenID uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	img, ok := f.images[imagenID]
	if !ok {
		return domain.ErrImagenNoEncontrada
	}
	// Remove from byPago slices.
	for pagoID, ids := range f.byPago {
		for i, id := range ids {
			if id == imagenID {
				f.byPago[pagoID] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
	}
	delete(f.images, img.ID())
	return nil
}

func (f *fakePagosImagenesRepo) FindImagenByID(_ context.Context, imagenID uuid.UUID) (*domain.Imagen, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	img, ok := f.images[imagenID]
	if !ok {
		return nil, domain.ErrImagenNoEncontrada
	}
	return img, nil
}

func (f *fakePagosImagenesRepo) ListImagenes(_ context.Context, pagoID uuid.UUID) ([]*domain.Imagen, error) {
	ids := f.byPago[pagoID]
	out := make([]*domain.Imagen, 0, len(ids))
	for _, id := range ids {
		if img, ok := f.images[id]; ok {
			out = append(out, img)
		}
	}
	return out, nil
}

// ─── fakeStorageProvider ─────────────────────────────────────────────────────

// fakeStorageProvider is an in-memory outbound.StorageProvider for unit tests.
type fakeStorageProvider struct {
	objects     map[string][]byte
	mimes       map[string]string
	storeErr    error
	getErr      error
	deleteErr   error
	storeCalls  int
	deleteCalls int
}

// compile-time check.
var _ outbound.StorageProvider = (*fakeStorageProvider)(nil)

func newFakeStorageProvider() *fakeStorageProvider {
	return &fakeStorageProvider{
		objects: map[string][]byte{},
		mimes:   map[string]string{},
	}
}

func (f *fakeStorageProvider) Store(_ context.Context, key, contentType string, _ int64, body io.Reader) error {
	f.storeCalls++
	if f.storeErr != nil {
		return f.storeErr
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.objects[key] = data
	f.mimes[key] = contentType
	return nil
}

func (f *fakeStorageProvider) Get(_ context.Context, key string) (outbound.StorageObject, error) {
	if f.getErr != nil {
		return outbound.StorageObject{}, f.getErr
	}
	data, ok := f.objects[key]
	if !ok {
		return outbound.StorageObject{}, errors.New("key not found: " + key)
	}
	return outbound.StorageObject{
		Body:        io.NopCloser(bytes.NewReader(data)),
		ContentType: f.mimes[key],
		SizeBytes:   int64(len(data)),
	}, nil
}

func (f *fakeStorageProvider) Delete(_ context.Context, key string) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.objects, key)
	delete(f.mimes, key)
	return nil
}

// ─── fakeImageProcessor ──────────────────────────────────────────────────────

// fakeImageProcessor satisfies outbound.ImageProcessor (= imageprocessor.Processor).
type fakeImageProcessor struct {
	out outbound.ImageProcessorOutput
	err error
}

// compile-time check.
var _ outbound.ImageProcessor = (*fakeImageProcessor)(nil)

func (f *fakeImageProcessor) Process(_ context.Context, _ outbound.ImageProcessorInput) (outbound.ImageProcessorOutput, error) {
	if f.err != nil {
		return outbound.ImageProcessorOutput{}, f.err
	}
	return f.out, nil
}

// ─── newImagenSvc helper ─────────────────────────────────────────────────────

// newImagenSvc builds a Service wired with the four imagen-specific fakes plus
// an existing pagosRecibidosRepo for pago lookup. saldos/pagos are no-ops;
// txMgr is the synchronous fakeTxRunner{}.
// All parameters are interface types so callers can pass typed nil as a real
// nil interface (important for deps-missing tests).
func newImagenSvc(
	t *testing.T,
	pagosRecibidos outbound.PagosRecibidosRepo,
	pagosImagenes outbound.PagosImagenesRepo,
	storage outbound.StorageProvider,
	imageProc outbound.ImageProcessor,
) *app.Service {
	t.Helper()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	return app.NewService(
		newFakeSaldosRepo(),
		newFakePagosRepo(),
		nil, // ventas not needed
		fixedClock{T: now},
		pagosRecibidos,
		pagosImagenes,
		nil, // microsipPago not needed
		storage,
		imageProc,
		fakeTxRunner{},
	)
}

// seedPago inserts a minimal PagoRecibido into pagosRecibidos and returns its ID.
func seedPago(t *testing.T, repo *fakePagosRecibidosRepo) uuid.UUID {
	t.Helper()
	pagoID := uuid.New()
	pago, err := domain.NewPagoRecibido(domain.CrearPagoRecibidoParams{
		ID:             pagoID,
		CargoDoctoCCID: 5000,
		ClienteID:      100,
		CobradorID:     200,
		Cobrador:       "García López, Martín",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   1,
		FechaHoraPago:  time.Date(2026, 5, 15, 11, 30, 0, 0, time.UTC),
		CreatedBy:      uuid.New(),
		Now:            time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.NoError(t, repo.Insert(context.Background(), pago))
	return pagoID
}

// baseImagenInput returns a valid AdjuntarImagenPagoInput for the given pago.
func baseImagenInput(pagoID uuid.UUID, mime string, sizeBytes int64, body io.Reader) app.AdjuntarImagenPagoInput {
	return app.AdjuntarImagenPagoInput{
		PagoID:      pagoID,
		ImagenID:    uuid.New(),
		StorageKind: domain.StorageKindFilesystem,
		StorageKey:  "cobranza/" + pagoID.String() + "/recibo.jpg",
		Mime:        mime,
		SizeBytes:   sizeBytes,
		Body:        body,
	}
}

// ─── AdjuntarImagenPago tests ────────────────────────────────────────────────

func TestAdjuntarImagenPago_HappyPath_PDF(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	pagoID := seedPago(t, pagosRepo)
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	payload := bytes.Repeat([]byte("A"), 1000)
	in := baseImagenInput(pagoID, domain.MimePDF, 1000, bytes.NewReader(payload))
	in.StorageKey = "cobranza/" + pagoID.String() + "/recibo.pdf"

	by := uuid.New()
	img, err := svc.AdjuntarImagenPago(context.Background(), in, by)

	require.NoError(t, err)
	require.NotNil(t, img)
	assert.Equal(t, in.ImagenID, img.ID())
	assert.Equal(t, domain.MimePDF, img.Mime())
	assert.Equal(t, int64(1000), img.SizeBytes())
	assert.Equal(t, 1, storage.storeCalls)
	assert.Equal(t, in.StorageKey, img.Storage().Key())
	assert.Len(t, imagenes.images, 1)
}

func TestAdjuntarImagenPago_HappyPath_PDF_SizeZero(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	pagoID := seedPago(t, pagosRepo)
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	payload := bytes.Repeat([]byte("B"), 512)
	in := baseImagenInput(pagoID, domain.MimePDF, 0, bytes.NewReader(payload))
	in.StorageKey = "cobranza/" + pagoID.String() + "/recibo.pdf"

	img, err := svc.AdjuntarImagenPago(context.Background(), in, uuid.New())

	require.NoError(t, err)
	require.NotNil(t, img)
	// SizeBytes must reflect the actual buffered content, not the declared 0.
	assert.Equal(t, int64(512), img.SizeBytes())
	assert.Equal(t, 1, storage.storeCalls)
}

func TestAdjuntarImagenPago_HappyPath_JPEG_RunsProcessor(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	processed := bytes.Repeat([]byte("P"), 512)
	proc := &fakeImageProcessor{
		out: outbound.ImageProcessorOutput{
			ContentType: domain.MimeJPEG,
			SizeBytes:   512,
			Body:        bytes.NewReader(processed),
		},
	}

	pagoID := seedPago(t, pagosRepo)
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, proc)

	in := baseImagenInput(pagoID, domain.MimeJPEG, 1024, bytes.NewReader(bytes.Repeat([]byte("X"), 1024)))
	in.StorageKey = "cobranza/" + pagoID.String() + "/foto.jpg"

	img, err := svc.AdjuntarImagenPago(context.Background(), in, uuid.New())

	require.NoError(t, err)
	require.NotNil(t, img)
	assert.Equal(t, domain.MimeJPEG, img.Mime())
	assert.Equal(t, int64(512), img.SizeBytes())
	assert.Equal(t, 1, storage.storeCalls)
	assert.Equal(t, domain.MimeJPEG, storage.mimes[in.StorageKey])
}

func TestAdjuntarImagenPago_PagoNotFound(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	// No pago seeded — FindByID will return ErrPagoNoEncontrado.
	in := baseImagenInput(uuid.New(), domain.MimePDF, 100, bytes.NewReader([]byte("x")))
	in.StorageKey = "cobranza/notfound/recibo.pdf"

	_, err := svc.AdjuntarImagenPago(context.Background(), in, uuid.New())

	require.ErrorIs(t, err, domain.ErrPagoNoEncontrado)
	assert.Equal(t, 0, storage.storeCalls)
}

func TestAdjuntarImagenPago_StorageKeyInvalid(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	pagoID := seedPago(t, pagosRepo)

	in := baseImagenInput(pagoID, domain.MimePDF, 100, bytes.NewReader([]byte("x")))
	in.StorageKey = "../etc/passwd" // path traversal

	_, err := svc.AdjuntarImagenPago(context.Background(), in, uuid.New())

	require.ErrorIs(t, err, domain.ErrStorageKeyInvalida)
	assert.Equal(t, 0, storage.storeCalls)
}

func TestAdjuntarImagenPago_ImageProcessor_Missing(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	// imageProc wired as nil — non-PDF MIME must fail.
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	pagoID := seedPago(t, pagosRepo)

	in := baseImagenInput(pagoID, domain.MimeJPEG, 100, bytes.NewReader(bytes.Repeat([]byte("I"), 100)))

	_, err := svc.AdjuntarImagenPago(context.Background(), in, uuid.New())

	require.Error(t, err)
	assert.Equal(t, 0, storage.storeCalls)
}

func TestAdjuntarImagenPago_ImageProcessor_Fails(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	proc := &fakeImageProcessor{err: errors.New("decode_failed")}

	pagoID := seedPago(t, pagosRepo)
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, proc)

	in := baseImagenInput(pagoID, domain.MimeJPEG, 200, bytes.NewReader(bytes.Repeat([]byte("J"), 200)))

	_, err := svc.AdjuntarImagenPago(context.Background(), in, uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode_failed")
	assert.Equal(t, 0, storage.storeCalls)
}

func TestAdjuntarImagenPago_StorageStore_Fails(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	storage.storeErr = errors.New("disk_full")

	pagoID := seedPago(t, pagosRepo)
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	in := baseImagenInput(pagoID, domain.MimePDF, 100, bytes.NewReader(bytes.Repeat([]byte("D"), 100)))
	in.StorageKey = "cobranza/" + pagoID.String() + "/recibo.pdf"

	_, err := svc.AdjuntarImagenPago(context.Background(), in, uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk_full")
	// No InsertImagen called; no best-effort delete (store never succeeded).
	assert.Empty(t, imagenes.images)
	assert.Equal(t, 0, storage.deleteCalls)
}

func TestAdjuntarImagenPago_DomainAdjuntarFails_OrphanCleanup(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	pagoID := seedPago(t, pagosRepo)
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	// descripcion > 200 runes triggers ErrImagenDescripcionDemasiadoLarga in
	// domain.AdjuntarImagen AFTER storage.Store succeeds.
	longDesc := strings.Repeat("á", 201)
	in := baseImagenInput(pagoID, domain.MimePDF, 100, bytes.NewReader(bytes.Repeat([]byte("E"), 100)))
	in.StorageKey = "cobranza/" + pagoID.String() + "/recibo.pdf"
	in.Descripcion = &longDesc

	_, err := svc.AdjuntarImagenPago(context.Background(), in, uuid.New())

	require.ErrorIs(t, err, domain.ErrImagenDescripcionDemasiadoLarga)
	// Storage store happened (before domain call).
	assert.Equal(t, 1, storage.storeCalls)
	// Best-effort blob cleanup must have fired.
	assert.Equal(t, 1, storage.deleteCalls)
	// Row must NOT have been persisted.
	assert.Empty(t, imagenes.images)
}

func TestAdjuntarImagenPago_InsertImagen_Fails_OrphanCleanup(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	imagenes.insertErr = errors.New("constraint_violation")
	storage := newFakeStorageProvider()

	pagoID := seedPago(t, pagosRepo)
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	in := baseImagenInput(pagoID, domain.MimePDF, 100, bytes.NewReader(bytes.Repeat([]byte("F"), 100)))
	in.StorageKey = "cobranza/" + pagoID.String() + "/recibo.pdf"

	_, err := svc.AdjuntarImagenPago(context.Background(), in, uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "constraint_violation")
	// Storage store happened, then best-effort blob delete must have fired.
	assert.Equal(t, 1, storage.storeCalls)
	assert.Equal(t, 1, storage.deleteCalls)
}

func TestAdjuntarImagenPago_DepsMissing_PagosImagenes(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	storage := newFakeStorageProvider()
	// pagosImagenes = nil
	svc := newImagenSvc(t, pagosRepo, nil, storage, nil)

	pagoID := seedPago(t, pagosRepo)
	in := baseImagenInput(pagoID, domain.MimePDF, 10, bytes.NewReader([]byte("x")))
	in.StorageKey = "cobranza/" + pagoID.String() + "/r.pdf"

	_, err := svc.AdjuntarImagenPago(context.Background(), in, uuid.New())
	require.Error(t, err)
}

func TestAdjuntarImagenPago_DepsMissing_Storage(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	// storage = nil
	svc := newImagenSvc(t, pagosRepo, imagenes, nil, nil)

	pagoID := seedPago(t, pagosRepo)
	in := baseImagenInput(pagoID, domain.MimePDF, 10, bytes.NewReader([]byte("x")))
	in.StorageKey = "cobranza/" + pagoID.String() + "/r.pdf"

	_, err := svc.AdjuntarImagenPago(context.Background(), in, uuid.New())
	require.Error(t, err)
}

func TestAdjuntarImagenPago_DepsMissing_PagosRecibidos(t *testing.T) {
	t.Parallel()

	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	// pagosRecibidos = nil
	svc := newImagenSvc(t, nil, imagenes, storage, nil)

	in := baseImagenInput(uuid.New(), domain.MimePDF, 10, bytes.NewReader([]byte("x")))
	in.StorageKey = "cobranza/test/r.pdf"

	_, err := svc.AdjuntarImagenPago(context.Background(), in, uuid.New())
	require.Error(t, err)
}

// ─── EliminarImagenPago tests ────────────────────────────────────────────────

// seedImagen inserts a pre-built Imagen into the fake repo and returns it.
func seedImagen(t *testing.T, imagenes *fakePagosImagenesRepo, pagoID uuid.UUID) *domain.Imagen {
	t.Helper()
	imagenID := uuid.New()
	storage, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "cobranza/"+pagoID.String()+"/img.jpg")
	require.NoError(t, err)

	img := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:        imagenID,
		Storage:   storage,
		Mime:      domain.MimeJPEG,
		SizeBytes: 256,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		CreatedBy: uuid.New(),
		UpdatedBy: uuid.New(),
	})
	require.NoError(t, imagenes.InsertImagen(context.Background(), pagoID, img))
	// Also seed the blob in any storage passed to the test.
	return img
}

func TestEliminarImagenPago_HappyPath(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	pagoID := seedPago(t, pagosRepo)
	img := seedImagen(t, imagenes, pagoID)
	// Seed the blob so Delete doesn't miss.
	storage.objects[img.Storage().Key()] = []byte("data")
	storage.mimes[img.Storage().Key()] = domain.MimeJPEG

	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	err := svc.EliminarImagenPago(context.Background(), pagoID, img.ID(), uuid.New())

	require.NoError(t, err)
	assert.Empty(t, imagenes.images)
	assert.Equal(t, 1, storage.deleteCalls)
}

func TestEliminarImagenPago_NotFound(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	err := svc.EliminarImagenPago(context.Background(), uuid.New(), uuid.New(), uuid.New())

	require.ErrorIs(t, err, domain.ErrImagenNoEncontrada)
	assert.Equal(t, 0, storage.deleteCalls)
}

func TestEliminarImagenPago_DeleteRowFails(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	imagenes.deleteErr = errors.New("fk_constraint")
	storage := newFakeStorageProvider()

	pagoID := seedPago(t, pagosRepo)
	img := seedImagen(t, imagenes, pagoID)
	// Reset deleteErr AFTER seeding (seedImagen calls InsertImagen, not Delete).
	// deleteErr is already set above — it will apply to DeleteImagen call in svc.
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	err := svc.EliminarImagenPago(context.Background(), pagoID, img.ID(), uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "fk_constraint")
	// Storage delete must NOT have been called — row delete failed first.
	assert.Equal(t, 0, storage.deleteCalls)
}

func TestEliminarImagenPago_BlobDeleteFails_NotPropagated(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	storage.deleteErr = errors.New("disk_io")

	pagoID := seedPago(t, pagosRepo)
	img := seedImagen(t, imagenes, pagoID)
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	// DeleteImagen row succeeds, but blob delete will fail.
	err := svc.EliminarImagenPago(context.Background(), pagoID, img.ID(), uuid.New())

	// Best-effort: blob failure must NOT propagate.
	require.NoError(t, err)
	// The row is gone.
	assert.Empty(t, imagenes.images)
	// Delete was called (best-effort).
	assert.Equal(t, 1, storage.deleteCalls)
}

func TestEliminarImagenPago_DepsMissing_Storage(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	svc := newImagenSvc(t, pagosRepo, imagenes, nil, nil)

	err := svc.EliminarImagenPago(context.Background(), uuid.New(), uuid.New(), uuid.New())
	require.Error(t, err)
}

func TestEliminarImagenPago_DepsMissing_PagosImagenes(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	storage := newFakeStorageProvider()
	svc := newImagenSvc(t, pagosRepo, nil, storage, nil)

	err := svc.EliminarImagenPago(context.Background(), uuid.New(), uuid.New(), uuid.New())
	require.Error(t, err)
}

// ─── ObtenerImagenPago tests ──────────────────────────────────────────────────

func TestObtenerImagenPago_HappyPath(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	pagoID := seedPago(t, pagosRepo)
	img := seedImagen(t, imagenes, pagoID)
	blobData := []byte("blob_content")
	storage.objects[img.Storage().Key()] = blobData
	storage.mimes[img.Storage().Key()] = domain.MimeJPEG

	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	result, err := svc.ObtenerImagenPago(context.Background(), pagoID, img.ID())

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, img.ID(), result.Imagen.ID())
	assert.NotNil(t, result.Object.Body)
	assert.Equal(t, domain.MimeJPEG, result.Object.ContentType)
	assert.Equal(t, int64(len(blobData)), result.Object.SizeBytes)

	// Verify body readable.
	body, readErr := io.ReadAll(result.Object.Body)
	require.NoError(t, readErr)
	assert.Equal(t, blobData, body)
}

func TestObtenerImagenPago_ImagenNotFound(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	_, err := svc.ObtenerImagenPago(context.Background(), uuid.New(), uuid.New())

	require.ErrorIs(t, err, domain.ErrImagenNoEncontrada)
}

func TestObtenerImagenPago_StorageGetFails(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	storage.getErr = errors.New("storage_unavailable")

	pagoID := seedPago(t, pagosRepo)
	img := seedImagen(t, imagenes, pagoID)
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	_, err := svc.ObtenerImagenPago(context.Background(), pagoID, img.ID())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage_unavailable")
}

func TestObtenerImagenPago_DepsMissing_PagosImagenes(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	storage := newFakeStorageProvider()
	svc := newImagenSvc(t, pagosRepo, nil, storage, nil)

	_, err := svc.ObtenerImagenPago(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)
}

func TestObtenerImagenPago_DepsMissing_Storage(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	svc := newImagenSvc(t, pagosRepo, imagenes, nil, nil)

	_, err := svc.ObtenerImagenPago(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)
}

// ─── ListarImagenesPago tests ─────────────────────────────────────────────────

func TestListarImagenesPago_HappyPath_Ordered(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	pagoID := seedPago(t, pagosRepo)

	// Insert 3 imagenes in a deterministic order.
	var expectedIDs []uuid.UUID
	for i := range 3 {
		storageKey := "cobranza/" + pagoID.String() + "/img" + string(rune('A'+i)) + ".jpg"
		s, err := domain.NewImagenStorage(domain.StorageKindFilesystem, storageKey)
		require.NoError(t, err)
		imgID := uuid.New()
		img := domain.HydrateImagen(domain.HydrateImagenParams{
			ID:        imgID,
			Storage:   s,
			Mime:      domain.MimeJPEG,
			SizeBytes: int64(100 + i),
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			CreatedBy: uuid.New(),
			UpdatedBy: uuid.New(),
		})
		require.NoError(t, imagenes.InsertImagen(context.Background(), pagoID, img))
		expectedIDs = append(expectedIDs, imgID)
	}

	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	list, err := svc.ListarImagenesPago(context.Background(), pagoID)

	require.NoError(t, err)
	require.Len(t, list, 3)
	for i, img := range list {
		assert.Equal(t, expectedIDs[i], img.ID(), "insertion order must be preserved at index %d", i)
	}
}

func TestListarImagenesPago_Empty(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	pagoID := seedPago(t, pagosRepo)
	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)

	list, err := svc.ListarImagenesPago(context.Background(), pagoID)

	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestListarImagenesPago_DepsMissing(t *testing.T) {
	t.Parallel()

	pagosRepo := newFakePagosRecibidosRepo()
	storage := newFakeStorageProvider()
	// pagosImagenes = nil
	svc := newImagenSvc(t, pagosRepo, nil, storage, nil)

	_, err := svc.ListarImagenesPago(context.Background(), uuid.New())
	require.Error(t, err)
}
