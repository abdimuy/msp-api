package failedintenthttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
)

// fakeBlobs is a Save/Open/Delete-tracking BlobStorage for replay tests.
type fakeBlobs struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func newFakeBlobs() *fakeBlobs {
	return &fakeBlobs{blobs: make(map[string][]byte)}
}

func (f *fakeBlobs) put(path string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(body))
	copy(cp, body)
	f.blobs[path] = cp
}

func (f *fakeBlobs) Save(
	_ context.Context, intentID uuid.UUID, body io.Reader, _ int64,
) (string, error) {
	buf, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	path := "/fake/" + intentID.String() + ".bin"
	f.put(path, buf)
	return path, nil
}

func (f *fakeBlobs) Open(_ context.Context, path string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.blobs[path]
	if !ok {
		return nil, failedintent.ErrBlobNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f *fakeBlobs) Delete(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.blobs, path)
	return nil
}

// TestReplay_FromBlob_StreamsBodyAndPreservesContentType verifies the replay
// path opens the blob and forwards both the bytes and the original
// Content-Type (boundary intact) to the downstream dispatcher.
func TestReplay_FromBlob_StreamsBodyAndPreservesContentType(t *testing.T) {
	t.Parallel()

	blobs := newFakeBlobs()
	const blobPath = "/fake/00000000-0000-0000-0000-000000000001.bin"
	bodyBytes := []byte("--BOUNDARY\r\nContent-Disposition: form-data; name=\"x\"\r\n\r\nhi\r\n--BOUNDARY--")
	blobs.put(blobPath, bodyBytes)

	usuarioID := uuid.New()
	store := newMemoryStore()
	intent := failedintent.Intent{
		ID:              uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Method:          http.MethodPost,
		Path:            "/v2/ventas",
		UsuarioID:       &usuarioID,
		Body:            json.RawMessage(`null`),
		BodyBlobPath:    blobPath,
		BodyContentType: "multipart/form-data; boundary=BOUNDARY",
		HTTPStatus:      http.StatusUnprocessableEntity,
		Status:          failedintent.StatusNew,
	}
	require.NoError(t, store.Save(context.Background(), intent))

	dispatcher := &fakeDispatcher{respondStatus: http.StatusCreated}
	lookup := &stubUsuarioLookup{user: auth.CurrentUser{ID: usuarioID}}
	svc := failedintenthttp.NewService(store, dispatcher, lookup, blobs, nil, nil)

	router := newRouter(t, svc, &auth.CurrentUser{ID: uuid.New()})

	req := httptest.NewRequest(http.MethodPost, "/"+intent.ID.String()+"/replay", http.NoBody)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code, "body=%s", rw.Body.String())

	assert.Equal(t, 1, dispatcher.callCount())
	assert.Equal(t, bodyBytes, dispatcher.lastBody(),
		"dispatcher must see the byte-exact blob payload")
	assert.Equal(
		t,
		"multipart/form-data; boundary=BOUNDARY",
		dispatcher.lastRequest().Header.Get("Content-Type"),
		"Content-Type with original boundary must be forwarded",
	)
}

// TestReplayWith_BlobIntent_Rejected verifies the explicit guard against
// applying a JSON override on top of a multipart-captured body.
func TestReplayWith_BlobIntent_Rejected(t *testing.T) {
	t.Parallel()

	usuarioID := uuid.New()
	store := newMemoryStore()
	intent := failedintent.Intent{
		ID:              uuid.New(),
		Method:          http.MethodPost,
		Path:            "/v2/ventas",
		UsuarioID:       &usuarioID,
		Body:            json.RawMessage(`null`),
		BodyBlobPath:    "/fake/something.bin",
		BodyContentType: "multipart/form-data; boundary=ABC",
		HTTPStatus:      http.StatusUnprocessableEntity,
		Status:          failedintent.StatusNew,
	}
	require.NoError(t, store.Save(context.Background(), intent))

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, newFakeBlobs(), nil, nil)
	router := newRouter(t, svc, &auth.CurrentUser{ID: uuid.New()})

	overrideBody := strings.NewReader(`{"body":{"venta_id":"x"}}`)
	req := httptest.NewRequest(http.MethodPost,
		"/"+intent.ID.String()+"/replay-with", overrideBody)
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	require.Equal(t, http.StatusUnprocessableEntity, rw.Code,
		"blob-backed intents must reject /replay-with; body=%s", rw.Body.String())
	assert.Contains(t, rw.Body.String(), "blob_intent_replay_with_unsupported")
}

// TestReplay_BlobOpenFailure_ReturnsApperror verifies that an Open failure
// from the blob storage surfaces as a 5xx apperror with a clear code rather
// than a panic.
func TestReplay_BlobOpenFailure_ReturnsApperror(t *testing.T) {
	t.Parallel()

	usuarioID := uuid.New()
	store := newMemoryStore()
	intent := failedintent.Intent{
		ID:              uuid.New(),
		Method:          http.MethodPost,
		Path:            "/v2/ventas",
		UsuarioID:       &usuarioID,
		Body:            json.RawMessage(`null`),
		BodyBlobPath:    "/fake/missing.bin",
		BodyContentType: "multipart/form-data; boundary=B",
		HTTPStatus:      http.StatusUnprocessableEntity,
		Status:          failedintent.StatusNew,
	}
	require.NoError(t, store.Save(context.Background(), intent))

	dispatcher := &fakeDispatcher{}
	lookup := &stubUsuarioLookup{user: auth.CurrentUser{ID: usuarioID}}
	svc := failedintenthttp.NewService(store, dispatcher, lookup, newFakeBlobs(), nil, nil)

	router := newRouter(t, svc, &auth.CurrentUser{ID: uuid.New()})

	req := httptest.NewRequest(http.MethodPost, "/"+intent.ID.String()+"/replay", http.NoBody)
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	// Replay returns OK with synthetic retried_fail when the request can't
	// be built; the dispatcher is never invoked.
	require.Equal(t, http.StatusOK, rw.Code, "Replay returns 200 + synthetic fail outcome on build error")

	var resp struct {
		Outcome          string `json:"outcome"`
		ReplayHTTPStatus int    `json:"replay_http_status"`
	}
	require.NoError(t, json.Unmarshal(rw.Body.Bytes(), &resp))
	assert.Equal(t, "retried_fail", resp.Outcome)
	assert.Equal(t, http.StatusInternalServerError, resp.ReplayHTTPStatus)
	assert.Equal(t, 0, dispatcher.callCount(), "dispatcher must not be called when build fails")
}
