//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// ─── Multipart helper ─────────────────────────────────────────────────────────

// buildMultipart creates a multipart/form-data body with a "file" field and an
// optional "descripcion" field. Returns the body buffer and the Content-Type
// header value (including boundary).
func buildMultipart(t *testing.T, mime string, content []byte, filename, descripcion string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	h.Set("Content-Type", mime)
	fw, err := w.CreatePart(h)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)

	if descripcion != "" {
		require.NoError(t, w.WriteField("descripcion", descripcion))
	}
	require.NoError(t, w.Close())
	return body, w.FormDataContentType()
}

// seedHTTPImagen inserts a minimal Imagen into the fake imagen repo and seeds
// its blob into the storage provider. Returns the Imagen.
func seedHTTPImagen(t *testing.T, imagenes *fakePagosImagenesRepo, storage *fakeStorageProvider, pagoID uuid.UUID) *domain.Imagen {
	t.Helper()
	imagenID := uuid.New()
	storageKey := "pagos/" + pagoID.String() + "/" + imagenID.String() + ".jpg"
	s, err := domain.NewImagenStorage(domain.StorageKindFilesystem, storageKey)
	require.NoError(t, err)

	img := domain.HydrateImagen(domain.HydrateImagenParams{
		ID:        imagenID,
		Storage:   s,
		Mime:      domain.MimeJPEG,
		SizeBytes: 512,
		CreatedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		CreatedBy: uuid.New(),
		UpdatedBy: uuid.New(),
	})

	require.NoError(t, imagenes.InsertImagen(context.Background(), pagoID, img))

	// Seed blob data into storage so ObtenerImagenPago can stream it.
	blobData := bytes.Repeat([]byte("X"), 512)
	storage.objects[storageKey] = blobData
	storage.mimes[storageKey] = domain.MimeJPEG

	return img
}

// buildBareReadRouter builds a read router with no CurrentUser planted —
// used to test the 401 path on the raw chi endpoint.
func buildBareReadRouter(svc *cobranzaapp.Service) http.Handler {
	r := chi.NewRouter()
	cobranzahttp.MountReadRouter(r, svc)
	return r
}

// fakeImageProcOutput bundles the output the fakeImageProcHTTP returns.
type fakeImageProcOutput struct {
	contentType string
	sizeBytes   int64
	body        []byte
}

// fakeImageProcHTTP satisfies outbound.ImageProcessor for HTTP handler tests.
type fakeImageProcHTTP struct {
	out fakeImageProcOutput
	err error
}

func (f *fakeImageProcHTTP) Process(_ context.Context, _ outbound.ImageProcessorInput) (outbound.ImageProcessorOutput, error) {
	if f.err != nil {
		return outbound.ImageProcessorOutput{}, f.err
	}
	return outbound.ImageProcessorOutput{
		Body:        bytes.NewReader(f.out.body),
		ContentType: f.out.contentType,
		SizeBytes:   f.out.sizeBytes,
	}, nil
}

// ─── AdjuntarImagenPago ───────────────────────────────────────────────────────

func TestHTTP_AdjuntarImagenPago_HappyPath_PDF(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	pagoID := seedHTTPPago(t, pagosRepo)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, imagenesRepo, nil, storage, nil, fakeTxRunner{})

	content := bytes.Repeat([]byte("%PDF-1.4 "), 100)
	body, ct := buildMultipart(t, "application/pdf", content, "recibo.pdf", "Comprobante de pago")

	req := httptest.NewRequest(http.MethodPost, "/pagos/"+pagoID.String()+"/imagenes", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Equal(t, 1, storage.storeCalls, "storage.Store must be called once")
	assert.Len(t, imagenesRepo.images, 1, "one imagen row must be stored")

	var dto cobranzahttp.ImagenPagoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, "application/pdf", dto.Mime)
}

func TestHTTP_AdjuntarImagenPago_HappyPath_JPEG(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	pagoID := seedHTTPPago(t, pagosRepo)

	// fakeImageProcHTTP returns a processed JPEG output.
	proc := &fakeImageProcHTTP{
		out: fakeImageProcOutput{
			contentType: domain.MimeJPEG,
			sizeBytes:   256,
			body:        bytes.Repeat([]byte("P"), 256),
		},
	}

	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, imagenesRepo, nil, storage, proc, fakeTxRunner{})

	content := bytes.Repeat([]byte("\xFF\xD8\xFF"), 100) // fake JPEG magic bytes
	body, ct := buildMultipart(t, "image/jpeg", content, "foto.jpg", "")

	req := httptest.NewRequest(http.MethodPost, "/pagos/"+pagoID.String()+"/imagenes", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Equal(t, 1, storage.storeCalls)

	var dto cobranzahttp.ImagenPagoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, "image/jpeg", dto.Mime)
}

func TestHTTP_AdjuntarImagenPago_MIMERejected_BMP(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	pagoID := seedHTTPPago(t, pagosRepo)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	content := bytes.Repeat([]byte("BM"), 50) // fake BMP header
	body, ct := buildMultipart(t, "image/bmp", content, "foto.bmp", "")

	req := httptest.NewRequest(http.MethodPost, "/pagos/"+pagoID.String()+"/imagenes", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	// Huma rejects via contentType constraint (415) or domain rejects via ErrMimeNoPermitido (422).
	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	assert.NotEmpty(t, rec.Body.String())
}

func TestHTTP_AdjuntarImagenPago_MIMERejected_TextPlain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	pagoID := seedHTTPPago(t, pagosRepo)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	body, ct := buildMultipart(t, "text/plain", []byte("hello world"), "notes.txt", "")

	req := httptest.NewRequest(http.MethodPost, "/pagos/"+pagoID.String()+"/imagenes", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
}

func TestHTTP_AdjuntarImagenPago_FileMissing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	pagoID := seedHTTPPago(t, pagosRepo)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	// Build multipart with only a descripcion field, no file.
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	require.NoError(t, w.WriteField("descripcion", "sin archivo"))
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/pagos/"+pagoID.String()+"/imagenes", buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
}

func TestHTTP_AdjuntarImagenPago_PagoNotFound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	// Empty repo — no pago exists.
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	content := bytes.Repeat([]byte("%PDF"), 10)
	body, ct := buildMultipart(t, "application/pdf", content, "recibo.pdf", "")

	req := httptest.NewRequest(http.MethodPost, "/pagos/"+uuid.New().String()+"/imagenes", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHTTP_AdjuntarImagenPago_InvalidPagoIDInPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	content := bytes.Repeat([]byte("%PDF"), 10)
	body, ct := buildMultipart(t, "application/pdf", content, "recibo.pdf", "")

	req := httptest.NewRequest(http.MethodPost, "/pagos/not-a-uuid/imagenes", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
}

func TestHTTP_AdjuntarImagenPago_PermDenied(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	pagoID := seedHTTPPago(t, pagosRepo)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	content := bytes.Repeat([]byte("%PDF"), 10)
	body, ct := buildMultipart(t, "application/pdf", content, "recibo.pdf", "")

	req := httptest.NewRequest(http.MethodPost, "/pagos/"+pagoID.String()+"/imagenes", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	mountReadWithUser(noPermUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ─── ListarImagenesPago ───────────────────────────────────────────────────────

func TestHTTP_ListarImagenesPago_HappyPath_Empty(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	pagoID := seedHTTPPago(t, pagosRepo)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, imagenesRepo, nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	req := httptest.NewRequest(http.MethodGet, "/pagos/"+pagoID.String()+"/imagenes", nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var items []cobranzahttp.ImagenPagoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &items))
	assert.Empty(t, items)
}

func TestHTTP_ListarImagenesPago_HappyPath_Populated(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	pagoID := seedHTTPPago(t, pagosRepo)

	// Insert 2 imagenes in deterministic order.
	var expectedIDs []string
	for i := range 2 {
		imgID := uuid.New()
		storageKey := fmt.Sprintf("pagos/%s/%s.jpg", pagoID, imgID)
		s, err := domain.NewImagenStorage(domain.StorageKindFilesystem, storageKey)
		require.NoError(t, err)
		img := domain.HydrateImagen(domain.HydrateImagenParams{
			ID:        imgID,
			Storage:   s,
			Mime:      domain.MimeJPEG,
			SizeBytes: int64(100 + i),
			CreatedAt: time.Date(2026, 6, 1, 10, i, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 6, 1, 10, i, 0, 0, time.UTC),
			CreatedBy: uuid.New(),
			UpdatedBy: uuid.New(),
		})
		require.NoError(t, imagenesRepo.InsertImagen(context.Background(), pagoID, img))
		expectedIDs = append(expectedIDs, imgID.String())
	}

	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, imagenesRepo, nil, storage, nil, fakeTxRunner{})

	req := httptest.NewRequest(http.MethodGet, "/pagos/"+pagoID.String()+"/imagenes", nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var items []cobranzahttp.ImagenPagoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &items))
	require.Len(t, items, 2, "must return exactly 2 imagenes")
	for i, item := range items {
		assert.Equal(t, expectedIDs[i], item.ID, "insertion order preserved at index %d", i)
	}
}

// ─── EliminarImagenPago ───────────────────────────────────────────────────────

func TestHTTP_EliminarImagenPago_HappyPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	pagoID := seedHTTPPago(t, pagosRepo)
	img := seedHTTPImagen(t, imagenesRepo, storage, pagoID)

	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, imagenesRepo, nil, storage, nil, fakeTxRunner{})

	url := fmt.Sprintf("/pagos/%s/imagenes/%s", pagoID, img.ID())
	req := httptest.NewRequest(http.MethodDelete, url, nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	// Huma maps EliminarImagenPagoOutput{} (empty struct) to 200 (DefaultStatus from op() helper).
	assert.Less(t, rec.Code, 300, "expected 2xx status, got %d body=%s", rec.Code, rec.Body.String())
	assert.Empty(t, imagenesRepo.images, "imagen must be deleted from repo")
	assert.Equal(t, 1, storage.deleteCalls, "blob must be deleted from storage")
}

func TestHTTP_EliminarImagenPago_NotFound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	url := fmt.Sprintf("/pagos/%s/imagenes/%s", uuid.New(), uuid.New())
	req := httptest.NewRequest(http.MethodDelete, url, nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHTTP_EliminarImagenPago_InvalidImgID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	url := fmt.Sprintf("/pagos/%s/imagenes/not-a-uuid", uuid.New())
	req := httptest.NewRequest(http.MethodDelete, url, nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
}

// ─── obtenerImagenPagoHandler (raw chi) ──────────────────────────────────────

func TestHTTP_ObtenerImagenPagoHandler_HappyPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	pagoID := seedHTTPPago(t, pagosRepo)
	img := seedHTTPImagen(t, imagenesRepo, storage, pagoID)

	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, imagenesRepo, nil, storage, nil, fakeTxRunner{})

	url := fmt.Sprintf("/pagos/%s/imagenes/%s", pagoID, img.ID())
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Equal(t, domain.MimeJPEG, rec.Header().Get("Content-Type"))
	assert.Equal(t, "512", rec.Header().Get("Content-Length"))
	assert.Equal(t, `"`+img.ID().String()+`"`, rec.Header().Get("ETag"))
	assert.Equal(t, "private, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))

	blobData := bytes.Repeat([]byte("X"), 512)
	assert.Equal(t, blobData, rec.Body.Bytes())
}

func TestHTTP_ObtenerImagenPagoHandler_ETag_NotModified(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()

	pagoID := seedHTTPPago(t, pagosRepo)
	img := seedHTTPImagen(t, imagenesRepo, storage, pagoID)

	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, imagenesRepo, nil, storage, nil, fakeTxRunner{})

	url := fmt.Sprintf("/pagos/%s/imagenes/%s", pagoID, img.ID())
	req := httptest.NewRequest(http.MethodGet, url, nil)
	// Send matching ETag.
	req.Header.Set("If-None-Match", `"`+img.ID().String()+`"`)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotModified, rec.Code)
	assert.Equal(t, `"`+img.ID().String()+`"`, rec.Header().Get("ETag"))
	assert.Equal(t, "private, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))
	assert.Empty(t, rec.Body.Bytes(), "304 must have no body")
}

func TestHTTP_ObtenerImagenPagoHandler_Unauthenticated(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	pagoID := seedHTTPPago(t, pagosRepo)
	img := seedHTTPImagen(t, imagenesRepo, storage, pagoID)

	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, imagenesRepo, nil, storage, nil, fakeTxRunner{})

	url := fmt.Sprintf("/pagos/%s/imagenes/%s", pagoID, img.ID())
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()

	// No planter — unauthenticated request reaches the raw chi handler.
	buildBareReadRouter(svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHTTP_ObtenerImagenPagoHandler_PermDenied(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	storage := newFakeStorageProvider()
	pagoID := seedHTTPPago(t, pagosRepo)
	img := seedHTTPImagen(t, imagenesRepo, storage, pagoID)

	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, imagenesRepo, nil, storage, nil, fakeTxRunner{})

	url := fmt.Sprintf("/pagos/%s/imagenes/%s", pagoID, img.ID())
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(noPermUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHTTP_ObtenerImagenPagoHandler_InvalidPagoUUID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	url := fmt.Sprintf("/pagos/not-a-uuid/imagenes/%s", uuid.New())
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "pago_id_invalido")
}

func TestHTTP_ObtenerImagenPagoHandler_InvalidImgUUID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	url := fmt.Sprintf("/pagos/%s/imagenes/not-a-uuid", uuid.New())
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "imagen_id_invalida")
}

func TestHTTP_ObtenerImagenPagoHandler_NotFound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	// Empty repos — no imagen exists.
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	url := fmt.Sprintf("/pagos/%s/imagenes/%s", uuid.New(), uuid.New())
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
