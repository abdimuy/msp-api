//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

// Multipart upload edge cases the existing _test files don't cover:
// partial / failing body. The MIME-mismatch and oversize cases are already
// pinned in handlers_imagenes_processor_test.go.

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// truncatedReader returns the first prefix bytes from underlying, then a
// fixed error on the next Read. Mimics a client that drops the connection
// mid-upload — the multipart parser sees the part header but encounters an
// I/O error before the part body is fully delivered.
type truncatedReader struct {
	underlying io.Reader
	prefix     int
	read       int
	err        error
}

func (r *truncatedReader) Read(p []byte) (int, error) {
	if r.read >= r.prefix {
		return 0, r.err
	}
	remaining := r.prefix - r.read
	if len(p) > remaining {
		p = p[:remaining]
	}
	n, err := r.underlying.Read(p)
	r.read += n
	if err != nil {
		return n, err
	}
	return n, nil
}

// TestAdjuntarImagen_PartialUpload_NoOrphanInStorage verifies the upload
// pipeline is resilient to a client that drops the connection mid-stream:
// the response surfaces a 4xx (not a 500), and the storage backend ends up
// holding nothing — no half-blob, no zero-byte stub.
func TestAdjuntarImagen_PartialUpload_NoOrphanInStorage(t *testing.T) {
	t.Parallel()

	svc, _, store := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := jsonRequest(t, http.MethodPost, "/ventas", body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	// Build a complete multipart envelope, then truncate the underlying
	// reader so the request body is cut off mid-payload. Huma's parser
	// surfaces this as a malformed-body error.
	var full bytes.Buffer
	mw := multipart.NewWriter(&full)
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="evidence.jpg"`)
	hdr.Set("Content-Type", "image/jpeg")
	part, err := mw.CreatePart(hdr)
	require.NoError(t, err)
	_, err = part.Write(bytes.Repeat([]byte("payload-bytes-"), 64)) // ~900 bytes
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	truncated := &truncatedReader{
		underlying: bytes.NewReader(full.Bytes()),
		prefix:     200, // cut off well before the body section ends
		err:        errors.New("simulated client disconnect"),
	}
	req := httptest.NewRequest(http.MethodPost, "/ventas/"+body.ID+"/imagenes", truncated)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400, "partial body must surface as a 4xx")
	assert.Less(t, rec.Code, 500, "partial body is a client problem, not a server one")

	store.mu.Lock()
	defer store.mu.Unlock()
	assert.Empty(t, store.blobs,
		"no blob may be persisted from a truncated multipart upload — the partial bytes are not a valid image")
}
