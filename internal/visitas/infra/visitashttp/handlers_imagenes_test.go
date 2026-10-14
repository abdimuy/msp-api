package visitashttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp"
)

// fotoSembrada guarda una imagen JPEG en la visita y su blob en storage.
func fotoSembrada(t *testing.T, f multipartFixture, visitaID uuid.UUID, desc *string) *domain.Imagen {
	t.Helper()
	id := uuid.New()
	key := "visitas/" + visitaID.String() + "/" + id.String() + ".jpg"
	st, err := domain.NewImagenStorage(domain.StorageKindFilesystem, key)
	require.NoError(t, err)
	img := domain.HydrateImagen(domain.HydrateImagenParams{
		ID: id, Storage: st, Mime: domain.MimeJPEG, SizeBytes: 4, Descripcion: desc,
		CreatedAt: time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC),
		CreatedBy: uuid.New(), UpdatedBy: uuid.New(),
	})
	require.NoError(t, f.imgRepo.InsertImagen(context.Background(), visitaID, img))
	require.NoError(t, f.store.Store(context.Background(), key, domain.MimeJPEG, 4, strings.NewReader("JPEG")))
	return img
}

func TestHTTP_ListarImagenesVisita(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	visita := seedVisita(t, f.svc, f.repo)
	fachada := "fachada"
	fotoSembrada(t, f, visita, &fachada)
	fotoSembrada(t, f, visita, nil)

	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), f.svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/visitas/"+visita.String()+"/imagenes", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 2)
	for _, k := range []string{"id", "mime", "size_bytes", "descripcion", "created_at", "updated_at"} {
		assert.Contains(t, got[1], k, "clave %s del contrato §6", k)
	}
	assert.Nil(t, got[1]["descripcion"], "descripcion null explícita, no omitida")
	assert.Equal(t, "fachada", got[0]["descripcion"])
	assert.Equal(t, "2026-10-14T16:00:00Z", got[0]["created_at"])

	var dtos []visitashttp.ImagenVisitaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dtos))
	assert.Equal(t, int64(4), dtos[0].SizeBytes)
}

func TestHTTP_ListarImagenesVisita_Errores(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	visita := seedVisita(t, f.svc, f.repo)
	casos := []struct {
		nombre string
		h      http.Handler
		path   string
		status int
	}{
		{"sin permiso", mountWithUser(noPermUser(), f.svc), "/visitas/" + visita.String() + "/imagenes", http.StatusForbidden},
		{"visita inexistente", mountWithUser(visitaUser(), f.svc), "/visitas/" + uuid.NewString() + "/imagenes", http.StatusNotFound},
		{"id inválido", mountWithUser(visitaUser(), f.svc), "/visitas/1%20OR%201=1/imagenes", http.StatusUnprocessableEntity},
	}
	for _, c := range casos {
		rec := httptest.NewRecorder()
		c.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		assert.Equal(t, c.status, rec.Code, "%s: %s", c.nombre, rec.Body.String())
	}
}

func TestHTTP_ObtenerImagenVisita_BinarioETagY304(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	visita := seedVisita(t, f.svc, f.repo)
	img := fotoSembrada(t, f, visita, nil)
	h := mountWithUser(visitaUser(), f.svc)
	path := "/visitas/" + visita.String() + "/imagenes/" + img.ID().String()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "JPEG", rec.Body.String())
	assert.Equal(t, domain.MimeJPEG, rec.Header().Get("Content-Type"))
	assert.Equal(t, "4", rec.Header().Get("Content-Length"))
	etag := `"` + img.ID().String() + `"`
	assert.Equal(t, etag, rec.Header().Get("ETag"))
	assert.Equal(t, "private, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotModified, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestHTTP_ObtenerImagenVisita_DeOtraVisitaEs404(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	visitaA := seedVisita(t, f.svc, f.repo)
	visitaB := seedVisita(t, f.svc, f.repo)
	img := fotoSembrada(t, f, visitaA, nil)

	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), f.svc).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/visitas/"+visitaB.String()+"/imagenes/"+img.ID().String(), nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "code=visita_imagen_no_encontrada")
	assert.NotContains(t, rec.Body.String(), "JPEG", "ni un byte de la foto ajena")
}

func TestHTTP_ObtenerImagenVisita_AuthYParametros(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	visita := seedVisita(t, f.svc, f.repo)
	img := fotoSembrada(t, f, visita, nil)
	path := "/visitas/" + visita.String() + "/imagenes/" + img.ID().String()

	rec := httptest.NewRecorder()
	mountWithUser(noPermUser(), f.svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = httptest.NewRecorder()
	mountBare(f.svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	for _, malo := range []string{"/visitas/no-uuid/imagenes/" + img.ID().String(), "/visitas/" + visita.String() + "/imagenes/no-uuid"} {
		rec = httptest.NewRecorder()
		mountWithUser(visitaUser(), f.svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, malo, nil))
		assert.Equal(t, http.StatusBadRequest, rec.Code, malo)
	}
}

// mountBare monta el router sin usuario plantado (camino 401).
func mountBare(svc *app.Service) http.Handler {
	r := chi.NewRouter()
	visitashttp.MountRouter(r, svc)
	return r
}
