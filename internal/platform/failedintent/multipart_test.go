package failedintent_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// fakeBlobStore is an in-memory BlobStorage for middleware unit tests.
type fakeBlobStore struct {
	mu      sync.Mutex
	blobs   map[string][]byte // path → bytes
	saveErr error
	limit   int64
}

func newFakeBlobStore() *fakeBlobStore {
	return &fakeBlobStore{blobs: make(map[string][]byte)}
}

func (f *fakeBlobStore) Save(
	_ context.Context, intentID uuid.UUID, body io.Reader, limitBytes int64,
) (string, error) {
	if f.saveErr != nil {
		_, _ = io.Copy(io.Discard, body)
		return "", f.saveErr
	}
	buf, err := io.ReadAll(io.LimitReader(body, limitBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(buf)) > limitBytes {
		return "", failedintent.ErrBlobTooLarge
	}
	path := "/fake/" + intentID.String() + ".bin"
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blobs[path] = buf
	return path, nil
}

func (f *fakeBlobStore) Open(_ context.Context, path string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.blobs[path]
	if !ok {
		return nil, failedintent.ErrBlobNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f *fakeBlobStore) Delete(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.blobs, path)
	return nil
}

func (f *fakeBlobStore) hasBlob(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.blobs[path]
	return ok
}

func (f *fakeBlobStore) blobBytes(path string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]byte, len(f.blobs[path]))
	copy(out, f.blobs[path])
	return out
}

func (f *fakeBlobStore) blobCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.blobs)
}

// buildMultipartRequest constructs a POST /v2/ventas multipart request whose
// body contains a JSON form field and one binary file part.
func buildMultipartRequest(t *testing.T, imagePayload []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("payload", `{"venta_id":"v1","precio":0}`))
	fileWriter, err := mw.CreateFormFile("evidencia", "ine.jpg")
	require.NoError(t, err)
	_, err = fileWriter.Write(imagePayload)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func newMultipartTestConfig(store *fakeStore, blobs failedintent.BlobStorage) failedintent.Config {
	return failedintent.Config{
		Store:             store,
		Blob:              blobs,
		PathPrefixes:      []string{"/v2/ventas"},
		Methods:           []string{http.MethodPost, http.MethodPatch, http.MethodPut},
		BodyCapBytes:      4096,
		MaxMultipartBytes: 10 * 1024 * 1024,
	}
}

// TestMultipart_CaptureOn422_PersistsBlobAndIntent verifies the full happy
// path: handler reads the multipart body, returns 422, the middleware saves
// the blob to disk and persists an intent pointing at it.
func TestMultipart_CaptureOn422_PersistsBlobAndIntent(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	mw := failedintent.CaptureMiddleware(newMultipartTestConfig(store, blobs))

	var bodyBytesSeenByHandler []byte
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain the body to ensure the TeeReader pumps everything to the blob.
		var err error
		bodyBytesSeenByHandler, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, problemBody422)
	})

	imageBytes := bytes.Repeat([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 256)
	req := buildMultipartRequest(t, imageBytes)
	rw := httptest.NewRecorder()
	mw(inner).ServeHTTP(rw, req)

	require.Equal(t, http.StatusUnprocessableEntity, rw.Code)
	require.Equal(t, 1, store.count())
	got := store.first()

	assert.NotEmpty(t, got.BodyBlobPath, "captured intent must point at a blob path")
	assert.True(t, strings.HasPrefix(got.BodyContentType, "multipart/form-data"),
		"captured Content-Type must preserve the multipart boundary")
	assert.True(t, blobs.hasBlob(got.BodyBlobPath), "blob must exist")
	assert.Equal(t, "venta_precio_invalido", got.ErrorCode)
	assert.False(t, got.BodyTruncated)
	// Body inline column must be empty JSON null (not bytes from the blob).
	assert.JSONEq(t, "null", string(got.Body))

	// The blob must contain the same bytes the handler observed.
	assert.Equal(t, bodyBytesSeenByHandler, blobs.blobBytes(got.BodyBlobPath),
		"blob bytes must match what the handler read")
}

// TestMultipart_2xx_DeletesBlob verifies that a successful upload does not
// leave a blob orphaned on disk.
func TestMultipart_2xx_DeletesBlob(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	mw := failedintent.CaptureMiddleware(newMultipartTestConfig(store, blobs))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusCreated)
	})

	req := buildMultipartRequest(t, []byte("ok image"))
	rw := httptest.NewRecorder()
	mw(inner).ServeHTTP(rw, req)

	require.Equal(t, http.StatusCreated, rw.Code)
	assert.Equal(t, 0, store.count(), "no intent must be persisted on 2xx")
	assert.Equal(t, 0, blobs.blobCount(), "blob must be cleaned up on 2xx")
}

// TestMultipart_SaveFailureStillPersistsIntent verifies the contract that
// when the blob save fails on a >=400 response, the audit row is still
// produced with BodyTruncated=true and an empty BodyBlobPath.
func TestMultipart_SaveFailureStillPersistsIntent(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	blobs.saveErr = errors.New("disk full")
	mw := failedintent.CaptureMiddleware(newMultipartTestConfig(store, blobs))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, problemBody422)
	})

	req := buildMultipartRequest(t, []byte("payload"))
	rw := httptest.NewRecorder()
	mw(inner).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count(), "intent must still be persisted when blob save fails")
	got := store.first()
	assert.Empty(t, got.BodyBlobPath, "BodyBlobPath must be empty on save failure")
	assert.True(t, got.BodyTruncated, "BodyTruncated must signal the missing body")
}

// TestMultipart_HandlerNeverReadsBody verifies that the goroutine does not
// leak when the handler ignores the body (e.g. early auth rejection).
func TestMultipart_HandlerNeverReadsBody(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	mw := failedintent.CaptureMiddleware(newMultipartTestConfig(store, blobs))

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Note: do NOT read r.Body. The middleware must close the pipe
		// writer so the Save goroutine returns.
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, problemBody422)
	})

	req := buildMultipartRequest(t, []byte("payload"))
	rw := httptest.NewRecorder()
	mw(inner).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count())
	got := store.first()
	// When the handler does not drain, Save sees only the bytes the handler
	// pulled (none) — the blob is empty but exists. The contract is that
	// the goroutine completes, not that the blob is byte-exact in this
	// degenerate case.
	assert.NotEmpty(t, got.BodyBlobPath)
	assert.True(t, blobs.hasBlob(got.BodyBlobPath))
}

// TestMultipart_SkippedWhenNoBlobConfigured verifies the legacy behaviour is
// preserved when Config.Blob is nil.
func TestMultipart_SkippedWhenNoBlobConfigured(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 1024) // Blob == nil
	mw := failedintent.CaptureMiddleware(cfg)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	})

	req := buildMultipartRequest(t, []byte("payload"))
	rw := httptest.NewRecorder()
	mw(inner).ServeHTTP(rw, req)

	assert.Equal(t, 0, store.count(), "multipart must be skipped when Blob is nil")
}
