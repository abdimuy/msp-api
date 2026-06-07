package failedintenthttp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
)

// ─── memoryBlobs ──────────────────────────────────────────────────────────────

type memoryBlobs struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func newMemoryBlobs() *memoryBlobs { return &memoryBlobs{blobs: map[string][]byte{}} }

func (m *memoryBlobs) Save(context.Context, uuid.UUID, io.Reader, int64) (string, error) {
	return "", nil
}

func (m *memoryBlobs) Open(_ context.Context, path string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.blobs[path]
	if !ok {
		return nil, failedintent.ErrBlobNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memoryBlobs) Delete(_ context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blobs, path)
	return nil
}

func (m *memoryBlobs) put(path string, b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobs[path] = b
}

// buildMultipartFixture writes a sample body with one JSON field + two
// file parts. Returns the bytes, the matching content-type header, and
// the wantHashes for download verification.
func buildMultipartFixture(t *testing.T) ([]byte, string, [][]byte) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Field: venta_json
	h1 := textproto.MIMEHeader{}
	h1.Set("Content-Disposition", `form-data; name="venta_json"`)
	h1.Set("Content-Type", "application/json")
	p1, err := w.CreatePart(h1)
	require.NoError(t, err)
	jsonBody := []byte(`{"cliente":"Carlos","monto":8500}`)
	_, err = p1.Write(jsonBody)
	require.NoError(t, err)

	// File: ine_frente
	h2 := textproto.MIMEHeader{}
	h2.Set("Content-Disposition", `form-data; name="ine_frente"; filename="ine.jpg"`)
	h2.Set("Content-Type", "image/jpeg")
	p2, err := w.CreatePart(h2)
	require.NoError(t, err)
	ineBody := bytes.Repeat([]byte{0xFF, 0xD8}, 2000)
	_, err = p2.Write(ineBody)
	require.NoError(t, err)

	// File: evidencia
	h3 := textproto.MIMEHeader{}
	h3.Set("Content-Disposition", `form-data; name="evidencia"; filename="firma.png"`)
	h3.Set("Content-Type", "image/png")
	p3, err := w.CreatePart(h3)
	require.NoError(t, err)
	firmaBody := bytes.Repeat([]byte{0x89, 0x50}, 500)
	_, err = p3.Write(firmaBody)
	require.NoError(t, err)

	require.NoError(t, w.Close())
	return buf.Bytes(), "multipart/form-data; boundary=" + w.Boundary(),
		[][]byte{jsonBody, ineBody, firmaBody}
}

// ─── BlobParts handler ────────────────────────────────────────────────────────

func TestBlobParts_NotFound_Returns404(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	missing := uuid.New().String()
	req := httptest.NewRequest(http.MethodGet, "/"+missing+"/blob-parts", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "failed_intent_not_found", p.Code)
}

func TestBlobParts_NoBlob_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	// JSON intent — no blob.
	seedIntent(t, store, makeIntent(id, time.Now().UTC()))

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/"+id.String()+"/blob-parts", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "failed_intent_no_blob", p.Code)
}

func TestBlobParts_BlobMissingFromDisk_Returns500BlobUnavailable(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/missing"
	intent.BodyContentType = "multipart/form-data; boundary=---x"
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/"+id.String()+"/blob-parts", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "failed_intent_blob_unavailable", p.Code)
}

func TestBlobParts_NonMultipartContentType_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/x"
	intent.BodyContentType = "application/json"
	seedIntent(t, store, intent)
	blobs.put("/blob/x", []byte("{}"))

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/"+id.String()+"/blob-parts", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "blob_intent_not_multipart", p.Code)
}

func TestBlobParts_HappyPath_ReturnsStructuredParts(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()

	body, ct, partBodies := buildMultipartFixture(t)
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/ok"
	intent.BodyContentType = ct
	seedIntent(t, store, intent)
	blobs.put("/blob/ok", body)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/"+id.String()+"/blob-parts", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.BlobPartsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, ct, resp.ContentType)
	require.Len(t, resp.Parts, 3)

	// Part 0: JSON field — inline value, no filename.
	assert.Equal(t, 0, resp.Parts[0].Index)
	assert.Equal(t, "venta_json", resp.Parts[0].Name)
	assert.Equal(t, "field", resp.Parts[0].Kind)
	assert.Equal(t, "application/json", resp.Parts[0].ContentType)
	assert.Empty(t, resp.Parts[0].Filename)
	assert.Equal(t, int64(len(partBodies[0])), resp.Parts[0].SizeBytes)
	decoded, err := base64.StdEncoding.DecodeString(resp.Parts[0].Value)
	require.NoError(t, err)
	assert.Equal(t, partBodies[0], decoded)

	// Part 1: file — metadata only.
	assert.Equal(t, "file", resp.Parts[1].Kind)
	assert.Equal(t, "ine.jpg", resp.Parts[1].Filename)
	assert.Equal(t, "image/jpeg", resp.Parts[1].ContentType)
	assert.Equal(t, int64(len(partBodies[1])), resp.Parts[1].SizeBytes)
	assert.Empty(t, resp.Parts[1].Value, "file parts must not carry inline base64")

	// Part 2: file — metadata only.
	assert.Equal(t, "file", resp.Parts[2].Kind)
	assert.Equal(t, "firma.png", resp.Parts[2].Filename)
	assert.Equal(t, "image/png", resp.Parts[2].ContentType)
	assert.Equal(t, int64(len(partBodies[2])), resp.Parts[2].SizeBytes)
}
