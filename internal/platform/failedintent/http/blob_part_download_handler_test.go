package failedintenthttp_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
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
	// XSS defense: every download is forced to `attachment` so the
	// browser never renders a captured part inline, regardless of
	// content type.
	assert.Equal(t, "attachment", rec.Header().Get("Content-Disposition"),
		"all downloads are forced to attachment, even fields without a filename")
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, partBodies[0], rec.Body.Bytes())
}

// TestBlobPartDownload_ForcesAttachmentAndAllowlistsContentType is the
// XSS-defense regression guard. A captured part with an attacker-chosen
// Content-Type (text/html, image/svg+xml, etc.) must never render
// inline in the admin's browser. The handler:
//
//   - Always sets Content-Disposition: attachment.
//   - Forces non-allowlisted content types to application/octet-stream.
//   - Sets X-Content-Type-Options: nosniff so browsers don't override.
//   - Sets a tight CSP sandbox.
func TestBlobPartDownload_ForcesAttachmentAndAllowlistsContentType(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()

	// Forge a captured blob with a malicious Content-Type on the first part.
	bodyBytes, ct := buildMultipartWithContentType(t, "text/html",
		[]byte("<script>alert(1)</script>"))
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/xss"
	intent.BodyContentType = ct
	seedIntent(t, store, intent)
	blobs.put("/blob/xss", bodyBytes)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/"+id.String()+"/blob-parts/0/download", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"),
		"non-allowlisted content type must be forced to octet-stream")
	assert.Equal(t, "attachment", rec.Header().Get("Content-Disposition"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "sandbox")
}

// buildMultipartWithContentType writes a single-part multipart body
// using an attacker-controlled Content-Type for the part. The handler
// must defang this on the way out.
func buildMultipartWithContentType(t *testing.T, contentType string, body []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="evil"`)
	hdr.Set("Content-Type", contentType)
	pw, err := w.CreatePart(hdr)
	require.NoError(t, err)
	_, err = pw.Write(body)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes(), "multipart/form-data; boundary=" + w.Boundary()
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
