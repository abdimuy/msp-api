package failedintenthttp_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
)

func TestBlobPartDownload_HappyPath_StreamsBytesAndSetsHeaders(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()

	body, ct, partBodies := buildMultipartFixture(t)
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/dl"
	intent.BodyContentType = ct
	seedIntent(t, store, intent)
	blobs.put("/blob/dl", body)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	// Index 1 is the JPEG image part.
	req := httptest.NewRequest(http.MethodGet, "/"+id.String()+"/blob-parts/1/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "image/jpeg", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	disp := rec.Header().Get("Content-Disposition")
	assert.Contains(t, disp, "attachment")
	assert.Contains(t, disp, "ine.jpg")
	assert.True(t, bytes.Equal(partBodies[1], rec.Body.Bytes()),
		"streamed bytes must match the original part exactly")
}

func TestBlobPartDownload_StreamsJSONFieldAsWell(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()

	body, ct, partBodies := buildMultipartFixture(t)
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/fld"
	intent.BodyContentType = ct
	seedIntent(t, store, intent)
	blobs.put("/blob/fld", body)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	// Index 0 is the JSON field part — no filename → no Content-Disposition.
	req := httptest.NewRequest(http.MethodGet, "/"+id.String()+"/blob-parts/0/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Empty(t, rec.Header().Get("Content-Disposition"),
		"field parts have no filename so no Content-Disposition header is set")
	assert.Equal(t, partBodies[0], rec.Body.Bytes())
}

func TestBlobPartDownload_NegativeIndex_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/"+id.String()+"/blob-parts/-1/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_part_index", p.Code)
}

func TestBlobPartDownload_NonIntegerIndex_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/"+id.String()+"/blob-parts/notanumber/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

func TestBlobPartDownload_OutOfRange_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()

	body, ct, _ := buildMultipartFixture(t)
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/oor"
	intent.BodyContentType = ct
	seedIntent(t, store, intent)
	blobs.put("/blob/oor", body)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	// Fixture has 3 parts, so index 7 is out of range.
	req := httptest.NewRequest(http.MethodGet, "/"+id.String()+"/blob-parts/7/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "part_index_out_of_range", p.Code)
}

func TestBlobPartDownload_NoBlob_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	seedIntent(t, store, makeIntent(id, time.Now().UTC()))

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/"+id.String()+"/blob-parts/0/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "failed_intent_no_blob", p.Code)
}

func TestBlobPartDownload_NotFound_Returns404(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	missing := uuid.New().String()
	req := httptest.NewRequest(http.MethodGet,
		"/"+missing+"/blob-parts/0/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "failed_intent_not_found", p.Code)
}

// Ensure large indices don't cause an integer panic — chi's URL param is
// already a string, so we just round-trip a 9-digit number.
func TestBlobPartDownload_LargeIndexFormat(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	body, ct, _ := buildMultipartFixture(t)
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/big"
	intent.BodyContentType = ct
	seedIntent(t, store, intent)
	blobs.put("/blob/big", body)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	index := strconv.Itoa(999_999_999)
	req := httptest.NewRequest(http.MethodGet,
		"/"+id.String()+"/blob-parts/"+index+"/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "part_index_out_of_range", p.Code)
	// The error message contains the requested index.
	assert.True(t, strings.Contains(rec.Body.String(), "índice") ||
		strings.Contains(rec.Body.String(), "indice"))
}
