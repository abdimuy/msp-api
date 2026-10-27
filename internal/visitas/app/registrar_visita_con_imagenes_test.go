//nolint:misspell // visitas vocabulary is Spanish (imagen, descripcion) per project convention.
package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
	"github.com/abdimuy/msp-api/internal/visitas/ports/outbound"
)

// ─── Hand-written fakes (mockery/gomock are forbidden in this repo) ─────────

// fakeImagenesRepo is an in-memory stand-in for
// outbound.VisitasImagenesRepo. It records what it was asked to insert so a
// test can assert on the exact rows, and can be made to fail on demand.
type fakeImagenesRepo struct {
	porVisita map[uuid.UUID][]*domain.Imagen

	insertErr  error
	findIDsErr error
	// ocultarEnFindIDs makes FindImagenIDs answer "nothing attached" while
	// InsertImagen still collides — the shape of a concurrent request that
	// commits between this one's pre-flight read and its insert.
	ocultarEnFindIDs bool

	insertCalls  int
	findIDsCalls int
}

func newFakeImagenesRepo() *fakeImagenesRepo {
	return &fakeImagenesRepo{porVisita: make(map[uuid.UUID][]*domain.Imagen)}
}

func (r *fakeImagenesRepo) InsertImagen(_ context.Context, visitaID uuid.UUID, img *domain.Imagen) error {
	r.insertCalls++
	if r.insertErr != nil {
		return r.insertErr
	}
	for _, existing := range r.porVisita[visitaID] {
		if existing.ID() == img.ID() {
			return domain.ErrImagenYaExiste
		}
	}
	r.porVisita[visitaID] = append(r.porVisita[visitaID], img)
	return nil
}

func (r *fakeImagenesRepo) FindImagenIDs(_ context.Context, visitaID uuid.UUID) ([]uuid.UUID, error) {
	r.findIDsCalls++
	if r.findIDsErr != nil {
		return nil, r.findIDsErr
	}
	if r.ocultarEnFindIDs {
		return nil, nil
	}
	out := make([]uuid.UUID, 0, len(r.porVisita[visitaID]))
	for _, img := range r.porVisita[visitaID] {
		out = append(out, img.ID())
	}
	return out, nil
}

func (r *fakeImagenesRepo) ListImagenes(_ context.Context, visitaID uuid.UUID) ([]*domain.Imagen, error) {
	return append([]*domain.Imagen{}, r.porVisita[visitaID]...), nil
}

// FindImagenDeVisita replica el WHERE ID = ? AND VISITA_ID = ? del repo real.
func (r *fakeImagenesRepo) FindImagenDeVisita(_ context.Context, visitaID, imagenID uuid.UUID) (*domain.Imagen, error) {
	for _, img := range r.porVisita[visitaID] {
		if img.ID() == imagenID {
			return img, nil
		}
	}
	return nil, domain.ErrImagenNoEncontrada
}

func (r *fakeImagenesRepo) count(visitaID uuid.UUID) int { return len(r.porVisita[visitaID]) }

// fakeStorage is an in-memory blob store that records every Store and Delete
// so a test can prove no blob was orphaned or wrongly removed.
type fakeStorage struct {
	blobs map[string][]byte

	storeCalls  int
	deleteCalls int
	deletedKeys []string
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{blobs: make(map[string][]byte)}
}

func (s *fakeStorage) Store(_ context.Context, key, _ string, _ int64, body io.Reader) error {
	s.storeCalls++
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.blobs[key] = data
	return nil
}

func (s *fakeStorage) Get(_ context.Context, key string) (outbound.StorageObject, error) {
	data, ok := s.blobs[key]
	if !ok {
		return outbound.StorageObject{}, errNotFoundBlob
	}
	return outbound.StorageObject{
		Body:      io.NopCloser(bytes.NewReader(data)),
		SizeBytes: int64(len(data)),
	}, nil
}

func (s *fakeStorage) Delete(_ context.Context, key string) error {
	s.deleteCalls++
	s.deletedKeys = append(s.deletedKeys, key)
	delete(s.blobs, key)
	return nil
}

func (s *fakeStorage) keys() []string { return slices.Sorted(maps.Keys(s.blobs)) }

var errNotFoundBlob = errors.New("fake storage: blob not found")

// fakeTxRunner runs the closure in-process. commits/rollbacks counts let a
// test prove the transaction was actually entered.
type fakeTxRunner struct {
	calls int
	// failAfterFn, when set, returns this error after fn succeeds — the shape
	// of a commit that fails, which is the nastiest rollback case.
	failAfterFn error
}

func (r *fakeTxRunner) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	r.calls++
	if err := fn(ctx); err != nil {
		return err
	}
	return r.failAfterFn
}

// ─── Builders ──────────────────────────────────────────────────────────────

type imagenesFixture struct {
	svc     *app.Service
	repo    *fakeVisitasRepo
	imgRepo *fakeImagenesRepo
	store   *fakeStorage
	tx      *fakeTxRunner
}

func newImagenesFixture(t *testing.T) imagenesFixture {
	t.Helper()
	repo := newFakeVisitasRepo()
	imgRepo := newFakeImagenesRepo()
	store := newFakeStorage()
	tx := &fakeTxRunner{}
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithImagenes(imgRepo, store, tx))
	return imagenesFixture{svc: svc, repo: repo, imgRepo: imgRepo, store: store, tx: tx}
}

func imagenesInput(id uuid.UUID) app.RegistrarVisitaInput {
	return app.RegistrarVisitaInput{
		ID:            id,
		Cobrador:      "Ramírez García, Jorge",
		CobradorID:    42,
		Fecha:         fixedNow.Add(-time.Hour),
		FormaCobroID:  87327,
		Lat:           19.432608,
		Lng:           -99.133209,
		TipoVisita:    string(domain.TipoVisitaNoSeEncontraba),
		ZonaClienteID: 7,
		ClienteID:     11486,
	}
}

// upload builds an ImagenUploadInput with the storage-key convention this
// module uses: "visitas/<visita_id>/<imagen_id><ext>".
func upload(visitaID, imagenID uuid.UUID, body string, opts ...func(*app.ImagenUploadInput)) app.ImagenUploadInput {
	in := app.ImagenUploadInput{
		ImagenID:    imagenID,
		StorageKind: domain.StorageKindFilesystem,
		StorageKey:  "visitas/" + visitaID.String() + "/" + imagenID.String() + ".jpg",
		Mime:        domain.MimeJPEG,
		SizeBytes:   int64(len(body)),
		Body:        strings.NewReader(body),
	}
	for _, o := range opts {
		o(&in)
	}
	return in
}

// ─── No images: the pre-existing path is untouched ─────────────────────────

func TestRegistrarVisitaConImagenes_SinImagenesDelegaEnRegistrarVisita(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()

	v, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id), nil, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, id, v.ID())

	assert.Equal(t, 1, f.repo.insertCalls)
	assert.Equal(t, 0, f.store.storeCalls, "no blob work without images")
	assert.Equal(t, 0, f.tx.calls, "no transaction is needed without images")
	assert.Equal(t, 0, f.imgRepo.findIDsCalls, "no pre-flight read either")
}

func TestRegistrarVisitaConImagenes_SinImagenesSigueSiendoIdempotente(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()
	by := uuid.New()

	first, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id), nil, by)
	require.NoError(t, err)
	f.repo.insertErr = domain.ErrVisitaYaExiste

	second, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id), nil, by)
	require.NoError(t, err, "a replay answers success, not a conflict")
	assert.Equal(t, first.ID(), second.ID())
}

// ─── Happy paths: one and several images ───────────────────────────────────

func TestRegistrarVisitaConImagenes_UnaImagen(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()
	imgID := uuid.New()

	v, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, imgID, "JPEGBYTES")}, uuid.New())
	require.NoError(t, err)

	assert.Equal(t, id, v.ID())
	require.Len(t, v.Imagenes(), 1)
	assert.Equal(t, imgID, v.Imagenes()[0].ID())

	assert.Equal(t, 1, f.tx.calls, "the write ran inside a transaction")
	assert.Equal(t, 1, f.imgRepo.count(id))
	assert.Equal(t, []string{"visitas/" + id.String() + "/" + imgID.String() + ".jpg"}, f.store.keys())
	assert.Equal(t, "JPEGBYTES", string(f.store.blobs[f.store.keys()[0]]))
	assert.Equal(t, 0, f.store.deleteCalls, "nothing failed, so nothing is cleaned up")
}

func TestRegistrarVisitaConImagenes_VariasImagenes(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	ups := make([]app.ImagenUploadInput, 0, len(ids))
	for i, imgID := range ids {
		ups = append(ups, upload(id, imgID, strings.Repeat("x", i+1)))
	}

	v, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id), ups, uuid.New())
	require.NoError(t, err)

	require.Len(t, v.Imagenes(), 3)
	assert.Equal(t, 3, f.imgRepo.count(id))
	assert.Len(t, f.store.keys(), 3)
	assert.Equal(t, 1, f.tx.calls, "all three land in ONE transaction")
}

func TestRegistrarVisitaConImagenes_AceptaTodaLaWhitelist(t *testing.T) {
	t.Parallel()

	for _, mime := range []string{
		domain.MimeJPEG, domain.MimePNG, domain.MimeGIF, domain.MimeWebP, domain.MimePDF,
	} {
		t.Run(mime, func(t *testing.T) {
			t.Parallel()
			f := newImagenesFixture(t)
			id := uuid.New()
			imgID := uuid.New()
			up := upload(id, imgID, "data", func(u *app.ImagenUploadInput) { u.Mime = mime })

			v, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
				[]app.ImagenUploadInput{up}, uuid.New())
			require.NoError(t, err)
			require.Len(t, v.Imagenes(), 1)
			assert.Equal(t, mime, v.Imagenes()[0].Mime())
		})
	}
}

func TestRegistrarVisitaConImagenes_NormalizaElMimeAMinusculas(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()
	up := upload(id, uuid.New(), "data", func(u *app.ImagenUploadInput) { u.Mime = "IMAGE/JPEG" })

	v, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{up}, uuid.New())
	require.NoError(t, err, "the store-side check must lower-case exactly like the domain does")
	assert.Equal(t, "image/jpeg", v.Imagenes()[0].Mime())
}

func TestRegistrarVisitaConImagenes_GuardaLaDescripcion(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()
	desc := "puerta"
	up := upload(id, uuid.New(), "d", func(u *app.ImagenUploadInput) { u.Descripcion = &desc })

	v, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{up}, uuid.New())
	require.NoError(t, err)
	require.NotNil(t, v.Imagenes()[0].Descripcion())
	assert.Equal(t, "puerta", *v.Imagenes()[0].Descripcion())
}

// ─── Idempotency WITH an image — the case the brief singles out ────────────

func TestRegistrarVisitaConImagenes_ReintentoIdenticoNoDuplicaNiHuerfana(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()
	imgID := uuid.New()
	by := uuid.New()
	key := "visitas/" + id.String() + "/" + imgID.String() + ".jpg"

	first, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, imgID, "FOTO")}, by)
	require.NoError(t, err)
	require.Len(t, first.Imagenes(), 1)

	storeCallsAfterFirst := f.store.storeCalls

	// The phone never saw the 201 and sends the byte-identical request again.
	second, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, imgID, "FOTO")}, by)
	require.NoError(t, err, "a replay must succeed, not conflict")

	assert.Equal(t, id, second.ID(), "same visita, not a second one")
	assert.Len(t, f.repo.byID, 1, "exactly one visita row")
	assert.Equal(t, 1, f.imgRepo.count(id), "exactly one imagen row — not duplicated")
	assert.Equal(t, 1, f.imgRepo.insertCalls, "the replay did not even attempt a second insert")

	assert.Equal(t, storeCallsAfterFirst, f.store.storeCalls,
		"the replay must not re-upload: the key is deterministic, so re-storing would "+
			"overwrite the live blob")
	assert.Equal(t, 0, f.store.deleteCalls,
		"and it must not delete anything either — that is how the blob would be orphaned")
	assert.Equal(t, []string{key}, f.store.keys(), "the one blob is still there")
	assert.Equal(t, "FOTO", string(f.store.blobs[key]), "with its original bytes")
	assert.Equal(t, 1, f.tx.calls,
		"and the replay opened no transaction at all — the pre-flight read answered everything")
}

func TestRegistrarVisitaConImagenes_DependenciasAMedioCablearTambienFallan(t *testing.T) {
	t.Parallel()

	// WithImagenes takes the three dependencies together so a half-wired
	// service cannot be built by accident — but nothing stops a caller
	// passing a nil among them, and each one alone must still be caught.
	// Anything else would let a request with photos run against a missing
	// storage or a missing transaction manager.
	repo := newFakeVisitasRepo()
	imgRepo := newFakeImagenesRepo()
	store := newFakeStorage()
	tx := &fakeTxRunner{}

	cases := map[string]app.Option{
		"sin repo de imágenes": app.WithImagenes(nil, store, tx),
		"sin storage":          app.WithImagenes(imgRepo, nil, tx),
		"sin transacción":      app.WithImagenes(imgRepo, store, nil),
	}
	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			svc := app.NewService(repo, &fakeClock{t: fixedNow}, opt)
			id := uuid.New()

			_, err := svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
				[]app.ImagenUploadInput{upload(id, uuid.New(), "x")}, uuid.New())

			require.Error(t, err)
			assert.Contains(t, err.Error(), "visitas_imagenes_no_configurado")
		})
	}
}

func TestRegistrarVisitaConImagenes_ReintentoConFotoNuevaLaAdjuntaALaVisitaExistente(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()
	by := uuid.New()
	primera := uuid.New()

	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, primera, "A")}, by)
	require.NoError(t, err)

	// The cobrador retried and this time the camera worked: same visita id,
	// the old photo plus a new one.
	segunda := uuid.New()
	v, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, primera, "A"), upload(id, segunda, "B")}, by)
	require.NoError(t, err)

	assert.Equal(t, id, v.ID())
	assert.Len(t, f.repo.byID, 1, "still one visita")
	assert.Equal(t, 2, f.imgRepo.count(id), "the new photo landed; the old one was not duplicated")
	assert.Len(t, f.store.keys(), 2)
	assert.Equal(t, 0, f.store.deleteCalls)
}

func TestRegistrarVisitaConImagenes_CarreraConcurrenteSeAbsorbe(t *testing.T) {
	t.Parallel()

	// The pre-flight read says "not there", but between it and the INSERT a
	// concurrent identical request commits. Insert answers ErrVisitaYaExiste
	// and the service must load that row and carry on, not fail.
	f := newImagenesFixture(t)
	id := uuid.New()
	imgID := uuid.New()

	// Seed the row the way the "other" request would have, then make Insert
	// report the collision.
	seeded, err := domain.NewVisita(domain.CrearVisitaParams{
		ID: id, Cobrador: "Otro", ClienteID: 1, Fecha: fixedNow,
		TipoVisita: string(domain.TipoVisitaNoSeEncontraba), CreatedBy: uuid.New(), Now: fixedNow,
	})
	require.NoError(t, err)

	f.repo.findByIDErr = domain.ErrVisitaNoEncontrada // pre-flight read misses
	_, err = f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, imgID, "X")}, uuid.New())
	require.NoError(t, err)

	// Now the second, racing call: the row exists and Insert collides.
	f.repo.findByIDErr = nil
	f.repo.byID[id] = seeded
	f.repo.insertErr = domain.ErrVisitaYaExiste

	otra := uuid.New()
	v, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, otra, "Y")}, uuid.New())
	require.NoError(t, err, "the collision is absorbed, not surfaced")
	assert.Equal(t, id, v.ID())
}

func TestRegistrarVisitaConImagenes_ImagenYaExisteEnElInsertSeAbsorbe(t *testing.T) {
	t.Parallel()

	// Same race, one level down: another attempt inserted this exact imagen
	// id first. That is the outcome we wanted, so it is success.
	f := newImagenesFixture(t)
	id := uuid.New()
	imgID := uuid.New()

	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, imgID, "A")}, uuid.New())
	require.NoError(t, err)

	// The pre-flight read reports nothing attached, so the service tries to
	// insert an imagen that is in fact already there.
	f.imgRepo.ocultarEnFindIDs = true

	v, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, imgID, "A")}, uuid.New())
	require.NoError(t, err)
	assert.Equal(t, id, v.ID())
	assert.Equal(t, 1, f.imgRepo.count(id))
}

// ─── Rejections ────────────────────────────────────────────────────────────

func TestRegistrarVisitaConImagenes_RechazaMimeFueraDeLaWhitelist(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()
	up := upload(id, uuid.New(), "x", func(u *app.ImagenUploadInput) { u.Mime = "image/bmp" })

	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{up}, uuid.New())
	require.ErrorIs(t, err, domain.ErrImagenMimeNoPermitido)

	assert.Empty(t, f.store.keys(), "a disallowed type must never reach the disk")
	assert.Empty(t, f.repo.byID, "and the visita must not be written either")
}

func TestRegistrarVisitaConImagenes_RechazaTipoVisitaFueraDelCatalogo(t *testing.T) {
	t.Parallel()

	// Task 7's rule survives the multipart path: the WRITE path validates the
	// closed catalogue, and it stays a distinguishable validation error.
	f := newImagenesFixture(t)
	id := uuid.New()
	in := imagenesInput(id)
	in.TipoVisita = "inventado por el cliente"

	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), in,
		[]app.ImagenUploadInput{upload(id, uuid.New(), "x")}, uuid.New())
	require.ErrorIs(t, err, domain.ErrVisitaTipoInvalido)
	assert.Empty(t, f.store.keys(), "nothing is written when the visita itself is invalid")
}

func TestRegistrarVisitaConImagenes_RechazaIDsDeImagenDuplicados(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()
	imgID := uuid.New()

	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, imgID, "A"), upload(id, imgID, "B")}, uuid.New())
	require.ErrorIs(t, err, domain.ErrImagenIDDuplicado)
	assert.Empty(t, f.store.keys())
}

func TestRegistrarVisitaConImagenes_RechazaStorageKeyInsegura(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()
	up := upload(id, uuid.New(), "x", func(u *app.ImagenUploadInput) {
		u.StorageKey = "visitas/../../etc/passwd"
	})

	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{up}, uuid.New())
	require.ErrorIs(t, err, domain.ErrImagenStorageKeyInvalida)
	assert.Empty(t, f.store.keys())
}

func TestRegistrarVisitaConImagenes_SinDependenciasCableadasEsErrorInterno(t *testing.T) {
	t.Parallel()

	// A Service built without WithImagenes still registers visits, but a
	// request carrying photos is a deployment problem, not a client one.
	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	id := uuid.New()

	_, err := svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, uuid.New(), "x")}, uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "visitas_imagenes_no_configurado")

	sinFoto, err := svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id), nil, uuid.New())
	require.NoError(t, err, "the same service still handles a visita without photos")
	assert.Equal(t, id, sinFoto.ID())
}

// ─── Failure paths: nothing is left behind ─────────────────────────────────

func TestRegistrarVisitaConImagenes_FalloDeStorageLimpiaLoYaEscrito(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()
	first := uuid.New()
	second := uuid.New()

	// The first blob lands, the second fails.
	calls := 0
	failing := &failingSecondStore{inner: f.store, failOn: 2, calls: &calls}
	svc := app.NewService(f.repo, &fakeClock{t: fixedNow}, app.WithImagenes(f.imgRepo, failing, f.tx))

	_, err := svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, first, "A"), upload(id, second, "B")}, uuid.New())
	require.Error(t, err)

	assert.Empty(t, f.store.keys(), "the blob that DID land was deleted again")
	assert.Empty(t, f.repo.byID, "and no visita row exists")
	assert.Equal(t, 0, f.tx.calls, "the transaction was never entered")
}

func TestRegistrarVisitaConImagenes_FalloDelRepoLimpiaLosBlobs(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	f.repo.insertErr = errBoom
	id := uuid.New()
	imgID := uuid.New()

	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, imgID, "A")}, uuid.New())
	require.ErrorIs(t, err, errBoom)

	assert.Empty(t, f.store.keys(), "no orphan blob survives a failed write")
	assert.Equal(t, 1, f.store.deleteCalls)
	assert.Equal(t,
		[]string{"visitas/" + id.String() + "/" + imgID.String() + ".jpg"},
		f.store.deletedKeys,
		"and it deleted exactly the key it had just written")
}

func TestRegistrarVisitaConImagenes_FalloAlInsertarLaImagenLimpiaLosBlobs(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	f.imgRepo.insertErr = errBoom
	id := uuid.New()

	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, uuid.New(), "A")}, uuid.New())
	require.ErrorIs(t, err, errBoom)
	assert.Empty(t, f.store.keys())
}

func TestRegistrarVisitaConImagenes_FalloAlHacerCommitLimpiaLosBlobs(t *testing.T) {
	t.Parallel()

	// The nastiest case: every step inside the closure succeeded and the
	// COMMIT itself failed. The rows are gone; the blobs must go too.
	f := newImagenesFixture(t)
	f.tx.failAfterFn = errBoom
	id := uuid.New()

	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, uuid.New(), "A")}, uuid.New())
	require.ErrorIs(t, err, errBoom)
	assert.Empty(t, f.store.keys())
}

func TestRegistrarVisitaConImagenes_FalloDeLaLecturaPreviaNoEscribeNada(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	f.repo.findByIDErr = errBoom
	id := uuid.New()

	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, uuid.New(), "A")}, uuid.New())
	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, 0, f.store.storeCalls, "the pre-flight read runs before any blob work")
}

func TestRegistrarVisitaConImagenes_FalloAlListarImagenesExistentesNoEscribeNada(t *testing.T) {
	t.Parallel()

	f := newImagenesFixture(t)
	id := uuid.New()
	by := uuid.New()
	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, uuid.New(), "A")}, by)
	require.NoError(t, err)

	storeCalls := f.store.storeCalls
	f.imgRepo.findIDsErr = errBoom

	_, err = f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, uuid.New(), "B")}, by)
	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, storeCalls, f.store.storeCalls, "no upload happens when the plan cannot be built")
}

// failingSecondStore wraps a storage provider and fails the Nth Store call,
// so a test can exercise a partial upload without hand-rolling a whole fake.
type failingSecondStore struct {
	inner  *fakeStorage
	failOn int
	calls  *int
}

func (s *failingSecondStore) Store(ctx context.Context, key, ct string, size int64, body io.Reader) error {
	*s.calls++
	if *s.calls == s.failOn {
		return errBoom
	}
	return s.inner.Store(ctx, key, ct, size, body)
}

func (s *failingSecondStore) Get(ctx context.Context, key string) (outbound.StorageObject, error) {
	return s.inner.Get(ctx, key)
}

func (s *failingSecondStore) Delete(ctx context.Context, key string) error {
	return s.inner.Delete(ctx, key)
}

var errBoom = errors.New("boom")
