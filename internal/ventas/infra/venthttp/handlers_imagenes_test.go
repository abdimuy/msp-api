//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// buildMultipartImageRequest returns an httptest request whose body is a
// minimal multipart/form-data envelope carrying a small PNG-like blob and
// an optional descripcion field.
func buildMultipartImageRequest(t *testing.T, target, descripcion string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// Use CreatePart so we can attach Content-Type to the file part — Huma's
	// MIME validator reads the part's declared Content-Type header to decide
	// whether the upload matches the accept list configured on the field.
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="evidence.png"`)
	hdr.Set("Content-Type", "image/png")
	fh, err := mw.CreatePart(hdr)
	require.NoError(t, err)
	_, err = fh.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	require.NoError(t, err)

	if descripcion != "" {
		require.NoError(t, mw.WriteField("descripcion", descripcion))
	}
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, target, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestAdjuntarImagen_OK(t *testing.T) {
	t.Parallel()

	svc, _, storage := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	// Seed a venta.
	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	// Upload an image.
	uploadReq := buildMultipartImageRequest(t, "/ventas/"+body.ID+"/imagenes", "evidencia entrega")
	uploadRec := httptest.NewRecorder()
	r.ServeHTTP(uploadRec, uploadReq)

	require.Equal(t, http.StatusCreated, uploadRec.Code, uploadRec.Body.String())
	var img venthttp.ImagenDTO
	require.NoError(t, json.Unmarshal(uploadRec.Body.Bytes(), &img))
	assert.NotEmpty(t, img.ID)
	assert.Equal(t, "image/png", img.Mime)
	require.NotNil(t, img.Descripcion)
	// Descripcion is folded to ALL CAPS by the domain (Microsip convention).
	assert.Equal(t, "EVIDENCIA ENTREGA", *img.Descripcion)
	assert.True(t, storage.has(img.StorageKey), "storage must hold the uploaded blob at the returned key")
}

func TestEliminarImagen_OK(t *testing.T) {
	t.Parallel()

	svc, _, storage := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	uploadReq := buildMultipartImageRequest(t, "/ventas/"+body.ID+"/imagenes", "")
	uploadRec := httptest.NewRecorder()
	r.ServeHTTP(uploadRec, uploadReq)
	require.Equal(t, http.StatusCreated, uploadRec.Code, uploadRec.Body.String())

	var img venthttp.ImagenDTO
	require.NoError(t, json.Unmarshal(uploadRec.Body.Bytes(), &img))
	require.True(t, storage.has(img.StorageKey))

	delReq := httptest.NewRequest(http.MethodDelete, "/ventas/"+body.ID+"/imagenes/"+img.ID, nil)
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)
	assert.Equal(t, http.StatusNoContent, delRec.Code, delRec.Body.String())
	assert.False(t, storage.has(img.StorageKey), "storage should be cleaned up after delete")
}
