//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// makeJPEGBody generates a synthetic JPEG of the requested dimensions for
// HTTP-level tests. Each pixel carries a deterministic gradient so encoder
// output is stable across runs.
func makeJPEGBody(t *testing.T, w, h, quality int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 80, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}))
	return buf.Bytes()
}

// makePNGBody generates a synthetic PNG.
func makePNGBody(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8((x + y) % 256), G: 220, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// buildMultipartRequestWithBody packages payload as a multipart upload with
// the supplied content-type. Used to exercise the AdjuntarImagen handler
// end-to-end with various MIMEs and sizes.
func buildMultipartRequestWithBody(t *testing.T, target, filename, contentType string, payload []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	hdr.Set("Content-Type", contentType)
	fh, err := mw.CreatePart(hdr)
	require.NoError(t, err)
	_, err = fh.Write(payload)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, target, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// TestAdjuntarImagen_WithStandardProcessor_ResizesAndShrinks is the
// end-to-end integration the plan calls for: upload a large JPEG, verify
// the persisted blob is smaller than the source AND its dimensions
// respect MaxLongSidePx.
func TestAdjuntarImagen_WithStandardProcessor_ResizesAndShrinks(t *testing.T) {
	t.Parallel()

	opts := imageprocessor.DefaultOptions()
	opts.MaxLongSidePx = 320
	opts.JPEGQuality = 75
	opts.PreserveSmallImages = false
	proc := imageprocessor.NewStandardProcessor(opts)

	svc, _, storage := testServiceWith(proc)
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	// Seed a venta.
	body := validCreateBody()
	createReq := jsonRequest(t, http.MethodPost, "/ventas", body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	// Source: 1600x1200, quality 95 — a phone-camera-shaped JPEG.
	srcBytes := makeJPEGBody(t, 1600, 1200, 95)
	uploadReq := buildMultipartRequestWithBody(t, "/ventas/"+body.ID+"/imagenes",
		"big.jpg", "image/jpeg", srcBytes)
	uploadRec := httptest.NewRecorder()
	r.ServeHTTP(uploadRec, uploadReq)

	require.Equal(t, http.StatusCreated, uploadRec.Code, uploadRec.Body.String())
	var img venthttp.ImagenDTO
	require.NoError(t, json.Unmarshal(uploadRec.Body.Bytes(), &img))
	assert.Equal(t, "image/jpeg", img.Mime)

	// The persisted blob must be the processor's output, NOT the source.
	require.True(t, storage.has(img.StorageKey), "storage must hold the processed blob")
	stored, err := storage.Get(t.Context(), img.StorageKey)
	require.NoError(t, err)
	storedBuf := bytes.Buffer{}
	_, err = storedBuf.ReadFrom(stored.Body)
	require.NoError(t, err)
	require.NoError(t, stored.Body.Close())

	assert.Less(t, storedBuf.Len(), len(srcBytes),
		"processed blob (%d bytes) must be smaller than source (%d bytes)",
		storedBuf.Len(), len(srcBytes))
	assert.Equal(t, int64(storedBuf.Len()), img.SizeBytes,
		"ImagenDTO.SizeBytes must reflect the processed payload size")

	// Verify the stored payload is a valid JPEG whose long side ≤ 320.
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(storedBuf.Bytes()))
	require.NoError(t, err)
	assert.LessOrEqual(t, cfg.Width, 320)
	assert.LessOrEqual(t, cfg.Height, 320)
}

// TestAdjuntarImagen_WithStandardProcessor_OversizeReturns422 verifies an
// oversize upload is rejected with HTTP 422 carrying the stable
// `imagen_too_large` code.
func TestAdjuntarImagen_WithStandardProcessor_OversizeReturns422(t *testing.T) {
	t.Parallel()

	opts := imageprocessor.DefaultOptions()
	opts.MaxInputBytes = 4 * 1024 // 4 KB cap so a tiny JPEG already busts it
	opts.PreserveSmallImages = false
	proc := imageprocessor.NewStandardProcessor(opts)

	svc, _, storage := testServiceWith(proc)
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := jsonRequest(t, http.MethodPost, "/ventas", body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	// 600x600 JPEG at quality 95 ≈ 60 KB — well over the 4 KB cap.
	srcBytes := makeJPEGBody(t, 600, 600, 95)
	uploadReq := buildMultipartRequestWithBody(t, "/ventas/"+body.ID+"/imagenes",
		"big.jpg", "image/jpeg", srcBytes)
	uploadRec := httptest.NewRecorder()
	r.ServeHTTP(uploadRec, uploadReq)

	require.Equal(t, http.StatusUnprocessableEntity, uploadRec.Code, uploadRec.Body.String())
	assert.Contains(t, uploadRec.Body.String(), "imagen_too_large",
		"response body must carry the stable error code so clients can branch")
	// Nothing must reach storage when the processor rejects.
	storage.mu.Lock()
	defer storage.mu.Unlock()
	assert.Empty(t, storage.blobs, "no blob may be written when the processor rejects an upload")
}

// TestAdjuntarImagen_HumaRejectsUnsupportedMIME confirms Huma's own
// content-type validator filters obviously wrong MIMEs at the boundary
// before the handler ever runs. This is the first line of defense — the
// processor's ErrUnsupportedMIME is the second.
func TestAdjuntarImagen_HumaRejectsUnsupportedMIME(t *testing.T) {
	t.Parallel()

	proc := imageprocessor.NewStandardProcessor(imageprocessor.DefaultOptions())
	svc, _, storage := testServiceWith(proc)
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := jsonRequest(t, http.MethodPost, "/ventas", body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	heicLike := append([]byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c'}, bytes.Repeat([]byte{0}, 256)...)
	uploadReq := buildMultipartRequestWithBody(t, "/ventas/"+body.ID+"/imagenes",
		"photo.heic", "image/heic", heicLike)
	uploadRec := httptest.NewRecorder()
	r.ServeHTTP(uploadRec, uploadReq)

	require.Equal(t, http.StatusUnprocessableEntity, uploadRec.Code, uploadRec.Body.String())
	assert.Contains(t, uploadRec.Body.String(), "Invalid mime type",
		"Huma's accept-list validation rejects HEIC at the boundary")
	storage.mu.Lock()
	defer storage.mu.Unlock()
	assert.Empty(t, storage.blobs, "Huma rejection short-circuits before storage")
}

// TestAdjuntarImagen_ProcessorCatchesSpoofedMIME exercises the processor's
// second-layer defense: a client that declares image/jpeg in the
// multipart Content-Type (passing Huma's accept-list) but sends bytes
// that do not sniff as any supported image. Huma accepts, the processor
// rejects with ErrUnsupportedMIME → HTTP 422 + mime_no_permitido.
func TestAdjuntarImagen_ProcessorCatchesSpoofedMIME(t *testing.T) {
	t.Parallel()

	proc := imageprocessor.NewStandardProcessor(imageprocessor.DefaultOptions())
	svc, _, storage := testServiceWith(proc)
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := jsonRequest(t, http.MethodPost, "/ventas", body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	// Plain text body labeled as image/jpeg in the multipart header.
	textBody := bytes.Repeat([]byte("plain text not an image "), 8)
	uploadReq := buildMultipartRequestWithBody(t, "/ventas/"+body.ID+"/imagenes",
		"fake.jpg", "image/jpeg", textBody)
	uploadRec := httptest.NewRecorder()
	r.ServeHTTP(uploadRec, uploadReq)

	require.Equal(t, http.StatusUnprocessableEntity, uploadRec.Code, uploadRec.Body.String())
	assert.Contains(t, uploadRec.Body.String(), "mime_no_permitido",
		"processor must reject content whose sniffed MIME differs from any allowed image MIME")
	storage.mu.Lock()
	defer storage.mu.Unlock()
	assert.Empty(t, storage.blobs)
}

// TestAdjuntarImagen_WithStandardProcessor_CorruptJPEGReturns422 verifies
// that a body whose magic bytes pass the MIME sniff but whose content is
// undecodable surfaces a 422 with the decode-failed code, never a 500.
func TestAdjuntarImagen_WithStandardProcessor_CorruptJPEGReturns422(t *testing.T) {
	t.Parallel()

	opts := imageprocessor.DefaultOptions()
	opts.PreserveSmallImages = false // force decode
	proc := imageprocessor.NewStandardProcessor(opts)
	svc, _, _ := testServiceWith(proc)
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := jsonRequest(t, http.MethodPost, "/ventas", body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	// JPEG SOI marker followed by garbage — passes sniff, fails decode.
	corrupt := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x42}, 256)...)
	uploadReq := buildMultipartRequestWithBody(t, "/ventas/"+body.ID+"/imagenes",
		"corrupt.jpg", "image/jpeg", corrupt)
	uploadRec := httptest.NewRecorder()
	r.ServeHTTP(uploadRec, uploadReq)

	require.Equal(t, http.StatusUnprocessableEntity, uploadRec.Code, uploadRec.Body.String())
	assert.Contains(t, uploadRec.Body.String(), "imagen_decode_failed")
}

// TestAdjuntarImagen_WithStandardProcessor_PNGStaysPNG verifies that a
// PNG upload is re-encoded as PNG (not silently switched to JPEG) so
// callers retain alpha/transparency semantics.
func TestAdjuntarImagen_WithStandardProcessor_PNGStaysPNG(t *testing.T) {
	t.Parallel()

	opts := imageprocessor.DefaultOptions()
	opts.MaxLongSidePx = 200
	opts.PreserveSmallImages = false
	proc := imageprocessor.NewStandardProcessor(opts)

	svc, _, storage := testServiceWith(proc)
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := jsonRequest(t, http.MethodPost, "/ventas", body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	uploadReq := buildMultipartRequestWithBody(t, "/ventas/"+body.ID+"/imagenes",
		"alpha.png", "image/png", makePNGBody(t, 800, 600))
	uploadRec := httptest.NewRecorder()
	r.ServeHTTP(uploadRec, uploadReq)

	require.Equal(t, http.StatusCreated, uploadRec.Code, uploadRec.Body.String())
	var img venthttp.ImagenDTO
	require.NoError(t, json.Unmarshal(uploadRec.Body.Bytes(), &img))
	assert.Equal(t, "image/png", img.Mime, "PNG inputs must stay PNG")
	require.True(t, storage.has(img.StorageKey))
}

// TestAdjuntarImagen_NoOpProcessor_PassthroughBitIdentical verifies the
// IMAGEPROCESSOR_ENABLED=false escape hatch: with the NoOp processor the
// stored blob is byte-identical to the source. This is the contract the
// operator opt-out relies on.
func TestAdjuntarImagen_NoOpProcessor_PassthroughBitIdentical(t *testing.T) {
	t.Parallel()

	svc, _, storage := testServiceWith(imageprocessor.NoOpProcessor{})
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := jsonRequest(t, http.MethodPost, "/ventas", body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	srcBytes := makeJPEGBody(t, 400, 300, 90)
	uploadReq := buildMultipartRequestWithBody(t, "/ventas/"+body.ID+"/imagenes",
		"photo.jpg", "image/jpeg", srcBytes)
	uploadRec := httptest.NewRecorder()
	r.ServeHTTP(uploadRec, uploadReq)
	require.Equal(t, http.StatusCreated, uploadRec.Code, uploadRec.Body.String())

	var img venthttp.ImagenDTO
	require.NoError(t, json.Unmarshal(uploadRec.Body.Bytes(), &img))
	stored, err := storage.Get(t.Context(), img.StorageKey)
	require.NoError(t, err)
	defer func() { _ = stored.Body.Close() }()
	var storedBuf bytes.Buffer
	_, err = storedBuf.ReadFrom(stored.Body)
	require.NoError(t, err)
	assert.Equal(t, srcBytes, storedBuf.Bytes(),
		"NoOp processor must persist the upload bytes verbatim")
}
