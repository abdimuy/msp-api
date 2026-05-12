//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// uploadAndGetImagenID seeds a venta + one imagen and returns the resulting
// ImagenDTO so each test can run against a clean fixture.
func uploadAndGetImagenID(t *testing.T, r http.Handler) (string, venthttp.ImagenDTO) {
	t.Helper()
	body := validCreateBody()
	createReq := jsonRequest(t, http.MethodPost, "/ventas", body)
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
	createReq := jsonRequest(t, http.MethodPost, "/ventas", body)
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
