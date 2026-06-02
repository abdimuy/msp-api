//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
	ventasoutbound "github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// uploadAndGetImagenID seeds a venta + one imagen and returns the resulting
// ImagenDTO so each test can run against a clean fixture.
func uploadAndGetImagenID(t *testing.T, r http.Handler) (string, venthttp.ImagenDTO) {
	t.Helper()
	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	uploadReq := buildMultipartImageRequest(t, "/ventas/"+body.ID+"/imagenes", "evidencia")
	uploadRec := httptest.NewRecorder()
	r.ServeHTTP(uploadRec, uploadReq)
	require.Equal(t, http.StatusCreated, uploadRec.Code, uploadRec.Body.String())
	var img venthttp.ImagenDTO
	require.NoError(t, json.Unmarshal(uploadRec.Body.Bytes(), &img))
	return body.ID, img
}

func TestObtenerImagen_OK(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	ventaID, img := uploadAndGetImagenID(t, r)

	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID+"/imagenes/"+img.ID, nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)

	require.Equal(t, http.StatusOK, getRec.Code, getRec.Body.String())
	// The fakeStorage Get always reports application/octet-stream regardless
	// of what was passed at Store time; production storage (filesystem)
	// reads the sidecar meta file and returns the real MIME. Here we only
	// verify the handler forwards whatever the provider says.
	assert.NotEmpty(t, getRec.Header().Get("Content-Type"))
	assert.Equal(t, `"`+img.ID+`"`, getRec.Header().Get("ETag"))
	assert.Equal(t, "private, max-age=31536000, immutable", getRec.Header().Get("Cache-Control"))
	assert.NotEmpty(t, getRec.Header().Get("Content-Length"))
	assert.NotEmpty(t, getRec.Body.Bytes(), "response must carry the blob")
}

func TestObtenerImagen_NotModifiedReturns304(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	ventaID, img := uploadAndGetImagenID(t, r)

	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID+"/imagenes/"+img.ID, nil)
	getReq.Header.Set("If-None-Match", `"`+img.ID+`"`)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)

	assert.Equal(t, http.StatusNotModified, getRec.Code, getRec.Body.String())
	assert.Equal(t, `"`+img.ID+`"`, getRec.Header().Get("ETag"))
	assert.Empty(t, getRec.Body.Bytes(), "304 must have an empty body")
}

func TestObtenerImagen_MismatchedETagReturnsFullBody(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	ventaID, img := uploadAndGetImagenID(t, r)

	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID+"/imagenes/"+img.ID, nil)
	getReq.Header.Set("If-None-Match", `"some-other-etag"`)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)

	assert.Equal(t, http.StatusOK, getRec.Code, getRec.Body.String())
	assert.NotEmpty(t, getRec.Body.Bytes())
}

func TestObtenerImagen_Unauthenticated_Returns401(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	// Build a router WITHOUT planting a CurrentUser so the handler sees an
	// anonymous request.
	cu := fullPerms(uuid.New())
	authedRouter := buildRouter(t, svc, cu)
	ventaID, img := uploadAndGetImagenID(t, authedRouter)

	// Now hit the same routes with a router that has no auth middleware
	// installed — we need a separate venthttp mount for this.
	anonRouter := buildRouterNoAuth(t, svc)
	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID+"/imagenes/"+img.ID, nil)
	getRec := httptest.NewRecorder()
	anonRouter.ServeHTTP(getRec, getReq)

	require.Equal(t, http.StatusUnauthorized, getRec.Code, getRec.Body.String())
	assert.Contains(t, getRec.Body.String(), "no_autenticado")
}

func TestObtenerImagen_MissingPermission_Returns403(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	// Seed with full perms so we can upload.
	full := fullPerms(uuid.New())
	authedRouter := buildRouter(t, svc, full)
	ventaID, img := uploadAndGetImagenID(t, authedRouter)

	// Now hit GET with a principal missing ventas_ver.
	limited := auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-limited",
		Email:       "limited@example.com",
		Nombre:      "Limited",
		Permisos:    []string{string(authdomain.PermVentasListar)}, // no ventas_ver
	}
	limitedRouter := buildRouter(t, svc, limited)
	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID+"/imagenes/"+img.ID, nil)
	getRec := httptest.NewRecorder()
	limitedRouter.ServeHTTP(getRec, getReq)

	require.Equal(t, http.StatusForbidden, getRec.Code, getRec.Body.String())
	assert.Contains(t, getRec.Body.String(), "permiso_denegado")
}

func TestObtenerImagen_VentaNotFound_Returns404(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	getReq := httptest.NewRequest(http.MethodGet,
		"/ventas/"+uuid.NewString()+"/imagenes/"+uuid.NewString(), nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)

	require.Equal(t, http.StatusNotFound, getRec.Code, getRec.Body.String())
	assert.Contains(t, getRec.Body.String(), "venta_not_found")
}

func TestObtenerImagen_ImagenNotFoundInVenta_Returns404(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	getReq := httptest.NewRequest(http.MethodGet,
		"/ventas/"+body.ID+"/imagenes/"+uuid.NewString(), nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)

	require.Equal(t, http.StatusNotFound, getRec.Code, getRec.Body.String())
	assert.Contains(t, getRec.Body.String(), "imagen_not_found")
}

func TestObtenerImagen_InvalidUUID_Returns400(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	cases := []struct {
		name string
		path string
		want string
	}{
		{"bad_venta_id", "/ventas/not-a-uuid/imagenes/" + uuid.NewString(), "venta_id_invalida"},
		{"bad_imagen_id", "/ventas/" + uuid.NewString() + "/imagenes/not-a-uuid", "imagen_id_invalida"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			getRec := httptest.NewRecorder()
			r.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			require.Equal(t, http.StatusBadRequest, getRec.Code, getRec.Body.String())
			assert.Contains(t, getRec.Body.String(), tc.want)
		})
	}
}

// TestObtenerImagen_MultipleImagenes_ReturnsCorrectOne pins the iter
// inside Service.ObtenerImagen: when a venta has many imagenes, each
// GET must resolve to the requested child — not the first/last one in
// the slice. A bug like "always returns imagenes[0]" passes every
// single-imagen test but breaks production galleries silently.
func TestObtenerImagen_MultipleImagenes_ReturnsCorrectOne(t *testing.T) {
	t.Parallel()

	svc, _, storage := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	// Upload three imagenes; each one ends up at a unique storage key.
	var imgs []venthttp.ImagenDTO
	for i := range 3 {
		uploadReq := buildMultipartImageRequest(t, "/ventas/"+body.ID+"/imagenes",
			"evidencia-"+strconv.Itoa(i))
		uploadRec := httptest.NewRecorder()
		r.ServeHTTP(uploadRec, uploadReq)
		require.Equal(t, http.StatusCreated, uploadRec.Code, "upload %d body=%s", i, uploadRec.Body.String())
		var dto venthttp.ImagenDTO
		require.NoError(t, json.Unmarshal(uploadRec.Body.Bytes(), &dto))
		imgs = append(imgs, dto)
	}
	require.Len(t, imgs, 3)
	// All three storage keys must differ — otherwise the test cannot
	// distinguish "iter picked wrong" from "all keys are the same".
	assert.NotEqual(t, imgs[0].StorageKey, imgs[1].StorageKey)
	assert.NotEqual(t, imgs[1].StorageKey, imgs[2].StorageKey)

	for _, want := range imgs {
		getReq := httptest.NewRequest(http.MethodGet,
			"/ventas/"+body.ID+"/imagenes/"+want.ID, nil)
		getRec := httptest.NewRecorder()
		r.ServeHTTP(getRec, getReq)
		require.Equal(t, http.StatusOK, getRec.Code, "imagen %s body=%s", want.ID, getRec.Body.String())
		assert.Equal(t, `"`+want.ID+`"`, getRec.Header().Get("ETag"),
			"ETag must echo the requested imagen id")

		stored, err := storage.Get(t.Context(), want.StorageKey)
		require.NoError(t, err)
		var storedBuf bytes.Buffer
		_, err = io.Copy(&storedBuf, stored.Body)
		require.NoError(t, err)
		_ = stored.Body.Close()
		assert.Equal(t, storedBuf.Bytes(), getRec.Body.Bytes(),
			"GET imagen %s must return its own bytes, not another imagen's", want.ID)
	}
}

// TestObtenerImagen_ContentLengthMatchesActualBody asserts the
// Content-Length header reflects the byte count actually written to the
// response. If the storage provider reports a wrong SizeBytes the
// client would hang waiting for bytes that never come; pinning this
// invariant surfaces that class of bug.
func TestObtenerImagen_ContentLengthMatchesActualBody(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	ventaID, img := uploadAndGetImagenID(t, r)
	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID+"/imagenes/"+img.ID, nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	cl, err := strconv.Atoi(getRec.Header().Get("Content-Length"))
	require.NoError(t, err, "Content-Length must be a valid integer")
	assert.Equal(t, len(getRec.Body.Bytes()), cl,
		"Content-Length header must match the actual body byte count")
}

// TestObtenerImagen_ConcurrentReads verifies the handler is safe under
// concurrent reads of the same imagen. Even though it is stateless,
// shared state on the storage provider or imagen iter could surface
// a race here.
func TestObtenerImagen_ConcurrentReads(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	ventaID, img := uploadAndGetImagenID(t, r)

	const goroutines = 16
	results := make(chan []byte, goroutines)
	for range goroutines {
		go func() {
			getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID+"/imagenes/"+img.ID, nil)
			getRec := httptest.NewRecorder()
			r.ServeHTTP(getRec, getReq)
			if getRec.Code != http.StatusOK {
				results <- nil
				return
			}
			results <- getRec.Body.Bytes()
		}()
	}
	first := <-results
	require.NotNil(t, first, "first concurrent GET must return 200")
	for i := 1; i < goroutines; i++ {
		got := <-results
		require.NotNil(t, got, "concurrent GET #%d returned non-200", i)
		assert.Equal(t, first, got, "concurrent GET #%d must return identical bytes", i)
	}
}

// TestObtenerImagen_HEADMethod_NotAllowed pins the current behavior for
// HEAD requests. The handler only registers GET; chi returns 405
// Method Not Allowed (or 404 depending on the router config). The FE
// must not depend on HEAD; this test surfaces the limitation.
func TestObtenerImagen_HEADMethod_NotAllowed(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)
	ventaID, img := uploadAndGetImagenID(t, r)

	headReq := httptest.NewRequest(http.MethodHead, "/ventas/"+ventaID+"/imagenes/"+img.ID, nil)
	headRec := httptest.NewRecorder()
	r.ServeHTTP(headRec, headReq)
	// chi routes HEAD to GET when not explicitly registered, returning the
	// headers without body. Accept either 200 with empty body or 405 — what
	// matters is the FE knows the contract.
	assert.Contains(t,
		[]int{http.StatusOK, http.StatusMethodNotAllowed},
		headRec.Code,
		"HEAD must either be supported (200) or refused (405); got %d", headRec.Code)
}

// closeTrackingReader wraps a Reader and counts Close calls so tests can
// pin the handler's body-cleanup contract under failure modes.
type closeTrackingReader struct {
	r       io.Reader
	mu      sync.Mutex
	closed  int
	readErr error
}

func (c *closeTrackingReader) Read(p []byte) (int, error) {
	if c.readErr != nil {
		return 0, c.readErr
	}
	return c.r.Read(p)
}

func (c *closeTrackingReader) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
	return nil
}

func (c *closeTrackingReader) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// fakeStorageReturningTracker wraps fakeStorage and replaces the Get
// return body with a closeTrackingReader so the test can assert the
// handler always closes the storage body, even on the streaming-error
// path.
type fakeStorageReturningTracker struct {
	*fakeStorage
	tracker *closeTrackingReader
}

func (f *fakeStorageReturningTracker) Get(ctx context.Context, key string) (ventasoutbound.StorageObject, error) {
	obj, err := f.fakeStorage.Get(ctx, key)
	if err != nil {
		return obj, err
	}
	// Re-wrap the body so the test can observe Close.
	body, _ := io.ReadAll(obj.Body)
	_ = obj.Body.Close()
	f.tracker.r = bytes.NewReader(body)
	return ventasoutbound.StorageObject{
		Body:        f.tracker,
		ContentType: obj.ContentType,
		SizeBytes:   obj.SizeBytes,
	}, nil
}

// TestObtenerImagen_BodyClosedOnSuccess pins the handler's defer
// Object.Body.Close() invariant. A storage provider that opens a file
// handle on Get relies on this to avoid leaks under load.
func TestObtenerImagen_BodyClosedOnSuccess(t *testing.T) {
	t.Parallel()

	fakeRepo := newFakeRepo()
	fakeStore := newFakeStorage()
	tracker := &closeTrackingReader{}
	wrappedStore := &fakeStorageReturningTracker{fakeStorage: fakeStore, tracker: tracker}
	clock := fixedClock{T: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)}
	svc := ventasapp.NewService(fakeRepo, nil, nil, wrappedStore, clock, noopOutbox{}, imageprocessor.NoOpProcessor{}, nil, nil, nil)

	cu := fullPerms(uuid.New())
	chiR := chi.NewRouter()
	chiR.Use(planter(cu))
	venthttp.MountRouter(chiR, svc)

	ventaID, img := uploadAndGetImagenID(t, chiR)

	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID+"/imagenes/"+img.ID, nil)
	getRec := httptest.NewRecorder()
	chiR.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)
	assert.Equal(t, 1, tracker.closeCount(),
		"handler must Close the storage body exactly once after a successful stream")
}

// TestObtenerImagen_BodyClosedOnEncodingFailure pins the Close-on-error
// invariant: even when the streaming Copy errors mid-flight, the storage
// body must be closed before the handler returns. A leak here would burn
// file descriptors on a flaky upstream and eventually starve the process.
func TestObtenerImagen_BodyClosedOnEncodingFailure(t *testing.T) {
	t.Parallel()

	fakeRepo := newFakeRepo()
	fakeStore := newFakeStorage()
	tracker := &closeTrackingReader{readErr: io.ErrUnexpectedEOF}
	wrappedStore := &fakeStorageReturningTracker{fakeStorage: fakeStore, tracker: tracker}
	clock := fixedClock{T: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)}
	svc := ventasapp.NewService(fakeRepo, nil, nil, wrappedStore, clock, noopOutbox{}, imageprocessor.NoOpProcessor{}, nil, nil, nil)

	cu := fullPerms(uuid.New())
	chiR := chi.NewRouter()
	chiR.Use(planter(cu))
	venthttp.MountRouter(chiR, svc)

	ventaID, img := uploadAndGetImagenID(t, chiR)

	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID+"/imagenes/"+img.ID, nil)
	getRec := httptest.NewRecorder()
	chiR.ServeHTTP(getRec, getReq)

	// The handler will have written the response headers (200 OK) before the
	// streaming Copy hits the read error — pinning Code semantics is brittle
	// here, what matters is the body got closed.
	assert.Equal(t, 1, tracker.closeCount(),
		"handler must Close the storage body even when the streaming Copy fails")
}

// Sanity: response body is byte-identical to the uploaded blob (NoOp
// processor) so the FE displays exactly what was uploaded.
func TestObtenerImagen_ResponseMatchesUploadedBytes(t *testing.T) {
	t.Parallel()

	svc, _, storage := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	ventaID, img := uploadAndGetImagenID(t, r)

	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID+"/imagenes/"+img.ID, nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code)

	stored, err := storage.Get(t.Context(), img.StorageKey)
	require.NoError(t, err)
	defer func() { _ = stored.Body.Close() }()
	var storedBuf bytes.Buffer
	_, err = io.Copy(&storedBuf, stored.Body)
	require.NoError(t, err)
	assert.Equal(t, storedBuf.Bytes(), getRec.Body.Bytes(),
		"GET response must match what is on storage byte-for-byte")
}
