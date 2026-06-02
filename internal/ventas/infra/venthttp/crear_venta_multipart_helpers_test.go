//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/stretchr/testify/require"
)

// crearVentaStubJPEG returns a valid 1×1 JPEG encoded at quality 90.
// Using a real JPEG (not a magic-byte stub) means the StandardProcessor can
// decode it even when PreserveSmallImages is disabled, so this helper works
// across all processor configurations.
func crearVentaStubJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 100, G: 150, B: 200, A: 255})
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}))
	return buf.Bytes()
}

// crearVentaMultipartRequest builds an httptest.Request for POST /ventas with
// the new multipart/form-data contract:
//   - `datos`  field → JSON-serialised body.
//   - `imagen` field → a minimal valid 1×1 JPEG (passes Huma's Content-Type
//     check AND the StandardProcessor's decode step).
//
// Every existing test that called jsonRequest(t, http.MethodPost, "/ventas",
// body) should call this helper instead so it hits the multipart handler path.
func crearVentaMultipartRequest(t *testing.T, body any) *http.Request {
	t.Helper()
	datosJSON, err := json.Marshal(body)
	require.NoError(t, err)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// datos field — plain-text JSON.
	require.NoError(t, mw.WriteField("datos", string(datosJSON)))

	// imagen field — real 1×1 JPEG; passes both Huma's accept-list check and
	// the StandardProcessor's decode step regardless of PreserveSmallImages.
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", `form-data; name="imagen"; filename="evidencia.jpg"`)
	hdr.Set("Content-Type", "image/jpeg")
	fw, err := mw.CreatePart(hdr)
	require.NoError(t, err)
	_, err = fw.Write(crearVentaStubJPEG(t))
	require.NoError(t, err)

	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/ventas", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// drainBody reads the request body into memory and returns it along with the
// Content-Type header. Used by tests that need to send the SAME bytes more
// than once — multipart boundaries are random per build, so reuse-by-rebuild
// would defeat any byte-identity check (e.g. the idempotency middleware's
// body hash). Caller can then construct fresh requests from bytes.NewReader.
func drainBody(t *testing.T, req *http.Request) ([]byte, string) {
	t.Helper()
	ct := req.Header.Get("Content-Type")
	buf, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.NoError(t, req.Body.Close())
	return buf, ct
}

// crearVentaMultipartRequestNoImg builds a POST /ventas multipart request that
// intentionally omits the `imagen` part. Use this only in tests that verify
// the "at least one imagen required" error path — all other POST /ventas tests
// should use crearVentaMultipartRequest.
func crearVentaMultipartRequestNoImg(t *testing.T, body any) *http.Request {
	t.Helper()
	datosJSON, err := json.Marshal(body)
	require.NoError(t, err)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("datos", string(datosJSON)))
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/ventas", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}
