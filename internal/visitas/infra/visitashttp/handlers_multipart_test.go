//nolint:misspell // visitas vocabulary is Spanish (imagen, descripcion) per project convention.
package visitashttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp"
	"github.com/abdimuy/msp-api/internal/visitas/ports/outbound"
)

// ─── Fakes for the photo path ──────────────────────────────────────────────

// httpFakeImagenesRepo is the HTTP tests' stand-in for
// outbound.VisitasImagenesRepo.
type httpFakeImagenesRepo struct {
	porVisita map[uuid.UUID][]*domain.Imagen
}

func newHTTPFakeImagenesRepo() *httpFakeImagenesRepo {
	return &httpFakeImagenesRepo{porVisita: make(map[uuid.UUID][]*domain.Imagen)}
}

func (r *httpFakeImagenesRepo) InsertImagen(_ context.Context, visitaID uuid.UUID, img *domain.Imagen) error {
	for _, existing := range r.porVisita[visitaID] {
		if existing.ID() == img.ID() {
			return domain.ErrImagenYaExiste
		}
	}
	r.porVisita[visitaID] = append(r.porVisita[visitaID], img)
	return nil
}

func (r *httpFakeImagenesRepo) FindImagenIDs(_ context.Context, visitaID uuid.UUID) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(r.porVisita[visitaID]))
	for _, img := range r.porVisita[visitaID] {
		out = append(out, img.ID())
	}
	return out, nil
}

func (r *httpFakeImagenesRepo) count(visitaID uuid.UUID) int { return len(r.porVisita[visitaID]) }

// httpFakeStorage records every blob written and every key deleted.
type httpFakeStorage struct {
	blobs       map[string][]byte
	deleteCalls int
}

func newHTTPFakeStorage() *httpFakeStorage {
	return &httpFakeStorage{blobs: make(map[string][]byte)}
}

func (s *httpFakeStorage) Store(_ context.Context, key, _ string, _ int64, body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.blobs[key] = data
	return nil
}

func (s *httpFakeStorage) Get(_ context.Context, key string) (outbound.StorageObject, error) {
	data, ok := s.blobs[key]
	if !ok {
		return outbound.StorageObject{}, http.ErrMissingFile
	}
	return outbound.StorageObject{Body: io.NopCloser(bytes.NewReader(data)), SizeBytes: int64(len(data))}, nil
}

func (s *httpFakeStorage) Delete(_ context.Context, key string) error {
	s.deleteCalls++
	delete(s.blobs, key)
	return nil
}

func (s *httpFakeStorage) keys() []string { return slices.Sorted(maps.Keys(s.blobs)) }

// httpFakeTx runs the closure in-process.
type httpFakeTx struct{ calls int }

func (r *httpFakeTx) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	r.calls++
	return fn(ctx)
}

// multipartFixture bundles a service with the photo path fully wired plus the
// fakes behind it, so a test can assert on rows AND on blobs.
type multipartFixture struct {
	svc     *app.Service
	repo    *fakeVisitasRepo
	imgRepo *httpFakeImagenesRepo
	store   *httpFakeStorage
	tx      *httpFakeTx
}

func newMultipartFixture(t *testing.T) multipartFixture {
	t.Helper()
	repo := newFakeVisitasRepo()
	imgRepo := newHTTPFakeImagenesRepo()
	store := newHTTPFakeStorage()
	tx := &httpFakeTx{}
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithImagenes(imgRepo, store, tx))
	return multipartFixture{svc: svc, repo: repo, imgRepo: imgRepo, store: store, tx: tx}
}

// ─── Multipart body builder ────────────────────────────────────────────────

// testImagen describes one `imagen` part to write into a multipart body.
type testImagen struct {
	Filename    string
	ContentType string
	Body        []byte
	ID          string // written as id_<n> when non-empty
	Descripcion string // written as descripcion_<n> when non-empty
}

// buildMultipart writes a multipart body with a `datos` field and N `imagen`
// parts, returning the bytes and the envelope Content-Type.
//
// Each file part gets an explicit Content-Type through CreatePart: Go's
// CreateFormFile does NOT set one, and a part without it is rejected by the
// MIME whitelist — the same trap HUMA_WIRING.md documents for cobranza.
func buildMultipart(t *testing.T, datos string, imgs []testImagen) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	if datos != "" {
		require.NoError(t, mw.WriteField("datos", datos))
	}
	for i, img := range imgs {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition",
			`form-data; name="imagen"; filename="`+img.Filename+`"`)
		if img.ContentType != "" {
			h.Set("Content-Type", img.ContentType)
		}
		part, err := mw.CreatePart(h)
		require.NoError(t, err)
		_, err = part.Write(img.Body)
		require.NoError(t, err)

		if img.ID != "" {
			require.NoError(t, mw.WriteField("id_"+strconv.Itoa(i), img.ID))
		}
		if img.Descripcion != "" {
			require.NoError(t, mw.WriteField("descripcion_"+strconv.Itoa(i), img.Descripcion))
		}
	}
	require.NoError(t, mw.Close())
	return buf.Bytes(), mw.FormDataContentType()
}

func jpeg(id, desc string) testImagen {
	return testImagen{
		Filename:    "foto.jpg",
		ContentType: domain.MimeJPEG,
		Body:        []byte("\xff\xd8\xff\xe0JFIF-bytes"),
		ID:          id,
		Descripcion: desc,
	}
}

func postMultipart(t *testing.T, f multipartFixture, body []byte, ct, idemKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), f.svc).ServeHTTP(rec, req)
	return rec
}

// ═══════════════════════════════════════════════════════════════════════════
// THE COEXISTENCE TEST
// ═══════════════════════════════════════════════════════════════════════════

// TestHTTP_CrearVisita_Coexistencia_JSONYMultipart is the most important test
// in this change.
//
// ONE route, ONE handler, TWO Content-Types, both answering 201 with the same
// DTO shape. The JSON half is the contract the phones already in the street
// speak; if it ever stops working, collectors stop being able to register
// visits. The multipart half is the new one. They are asserted side by side,
// in a single test, so nobody can "fix" one and quietly break the other.
//
// The positive control is built in: both halves assert 201 AND a body that
// echoes the id they sent. A route that answered 201 to everything, or that
// silently dropped the body, would fail here.
func TestHTTP_CrearVisita_Coexistencia_JSONYMultipart(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)

	// ── Half 1: the legacy JSON contract, unchanged ───────────────────────
	jsonID := uuid.New()
	jsonBody := visitaBodyJSON(jsonID.String(), fecha)

	reqJSON := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewBufferString(jsonBody))
	reqJSON.Header.Set("Content-Type", "application/json")
	reqJSON.Header.Set("Idempotency-Key", jsonID.String())
	recJSON := httptest.NewRecorder()
	mountWithUser(visitaUser(), f.svc).ServeHTTP(recJSON, reqJSON)

	require.Equal(t, http.StatusCreated, recJSON.Code,
		"the JSON contract MUST keep working — a phone running the old build sends this; body=%s",
		recJSON.Body.String())
	var dtoJSON visitashttp.VisitaDTO
	require.NoError(t, json.Unmarshal(recJSON.Body.Bytes(), &dtoJSON))
	assert.Equal(t, jsonID.String(), dtoJSON.ID)
	assert.Equal(t, "No se encontraba", dtoJSON.TipoVisita)

	// ── Half 2: the new multipart contract, same route ────────────────────
	mpID := uuid.New()
	imgID := uuid.New()
	body, ct := buildMultipart(t,
		visitaBodyJSON(mpID.String(), fecha),
		[]testImagen{jpeg(imgID.String(), "puerta")})

	recMP := postMultipart(t, f, body, ct, mpID.String())

	require.Equal(t, http.StatusCreated, recMP.Code,
		"the multipart contract must work on the SAME route; body=%s", recMP.Body.String())
	var dtoMP visitashttp.VisitaDTO
	require.NoError(t, json.Unmarshal(recMP.Body.Bytes(), &dtoMP))
	assert.Equal(t, mpID.String(), dtoMP.ID)
	assert.Equal(t, "No se encontraba", dtoMP.TipoVisita)

	// ── Both landed, and only the multipart one carried a photo ───────────
	assert.Len(t, f.repo.byID, 2, "two visits, one per Content-Type")
	assert.Equal(t, 0, f.imgRepo.count(jsonID), "the JSON request carries no photos")
	assert.Equal(t, 1, f.imgRepo.count(mpID))
	assert.Equal(t,
		[]string{"visitas/" + mpID.String() + "/" + imgID.String() + ".jpg"},
		f.store.keys(),
		"and the blob key follows the visitas/<visita_id>/<imagen_id><ext> convention")
}

// TestHTTP_CrearVisita_JSONConCharset acepta el header tal como lo manda un
// cliente HTTP normal, con parámetros.
func TestHTTP_CrearVisita_JSONConCharset(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	id := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)

	req := httptest.NewRequest(http.MethodPost, "/visitas",
		bytes.NewBufferString(visitaBodyJSON(id.String(), fecha)))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), f.svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
}

// ─── Zero, one, several images ─────────────────────────────────────────────

func TestHTTP_CrearVisita_Multipart_CeroImagenes(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	id := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	body, ct := buildMultipart(t, visitaBodyJSON(id.String(), fecha), nil)

	rec := postMultipart(t, f, body, ct, id.String())

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, 0, f.imgRepo.count(id))
	assert.Empty(t, f.store.keys())
	assert.Equal(t, 0, f.tx.calls, "a photo-less multipart needs no transaction either")
}

func TestHTTP_CrearVisita_Multipart_VariasImagenes(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	id := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	body, ct := buildMultipart(t, visitaBodyJSON(id.String(), fecha), []testImagen{
		jpeg(ids[0], "puerta"),
		{Filename: "firma.png", ContentType: domain.MimePNG, Body: []byte("PNGDATA"), ID: ids[1]},
		{Filename: "recibo.pdf", ContentType: domain.MimePDF, Body: []byte("%PDF-1.4"), ID: ids[2]},
	})

	rec := postMultipart(t, f, body, ct, id.String())

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, 3, f.imgRepo.count(id))
	require.Len(t, f.store.keys(), 3)
	assert.Equal(t, 1, f.tx.calls, "all three in ONE transaction")

	// The extension comes from the filename, so each key is distinguishable.
	assert.Contains(t, f.store.keys(), "visitas/"+id.String()+"/"+ids[0]+".jpg")
	assert.Contains(t, f.store.keys(), "visitas/"+id.String()+"/"+ids[1]+".png")
	assert.Contains(t, f.store.keys(), "visitas/"+id.String()+"/"+ids[2]+".pdf")
}

// TestHTTP_CrearVisita_Multipart_SinExtensionUsaLaDelMime covers the client
// that names its part "IMG20260901" with no extension — Android's camera
// intent does exactly that. The key then takes the MIME's canonical
// extension instead of ending in a bare uuid.
func TestHTTP_CrearVisita_Multipart_SinExtensionUsaLaDelMime(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	id := uuid.New()
	imgID := uuid.NewString()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	body, ct := buildMultipart(t, visitaBodyJSON(id.String(), fecha), []testImagen{
		{Filename: "IMG20260901", ContentType: domain.MimeWebP, Body: []byte("WEBP"), ID: imgID},
	})

	rec := postMultipart(t, f, body, ct, id.String())

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, []string{"visitas/" + id.String() + "/" + imgID + ".webp"}, f.store.keys())
}

func TestHTTP_CrearVisita_Multipart_GuardaLaDescripcionPosicional(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	id := uuid.New()
	imgID := uuid.NewString()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	body, ct := buildMultipart(t, visitaBodyJSON(id.String(), fecha),
		[]testImagen{jpeg(imgID, "el candado de la puerta")})

	rec := postMultipart(t, f, body, ct, id.String())
	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())

	imgs := f.imgRepo.porVisita[id]
	require.Len(t, imgs, 1)
	require.NotNil(t, imgs[0].Descripcion())
	assert.Equal(t, "el candado de la puerta", *imgs[0].Descripcion())
}

// ─── Idempotency with an image ─────────────────────────────────────────────

// TestHTTP_CrearVisita_Multipart_ReintentoConFotoNoDuplicaNiHuerfana is the
// second test the brief calls out by name: the same visita UUID retried WITH
// a photo must not create a second visit, must not attach the image twice,
// and must not leave a stray object in storage.
func TestHTTP_CrearVisita_Multipart_ReintentoConFotoNoDuplicaNiHuerfana(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	id := uuid.New()
	imgID := uuid.NewString()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	key := "visitas/" + id.String() + "/" + imgID + ".jpg"

	// The phone builds the request once and re-sends the same bytes, which is
	// what an offline queue actually does.
	body, ct := buildMultipart(t, visitaBodyJSON(id.String(), fecha),
		[]testImagen{jpeg(imgID, "puerta")})

	first := postMultipart(t, f, body, ct, id.String())
	require.Equal(t, http.StatusCreated, first.Code, "body=%s", first.Body.String())

	second := postMultipart(t, f, body, ct, id.String())
	require.Equal(t, http.StatusCreated, second.Code,
		"a replay answers 201 with the stored visita, never 409; body=%s", second.Body.String())

	var dto1, dto2 visitashttp.VisitaDTO
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &dto1))
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &dto2))
	assert.Equal(t, dto1.ID, dto2.ID)

	assert.Len(t, f.repo.byID, 1, "ONE visita, not two")
	assert.Equal(t, 1, f.imgRepo.count(id), "ONE imagen row, not two")
	assert.Equal(t, []string{key}, f.store.keys(), "ONE blob, still there")
	assert.Equal(t, 0, f.store.deleteCalls,
		"and nothing was deleted — deleting on replay is exactly how the live blob gets orphaned")
}

func TestHTTP_CrearVisita_Multipart_ReintentoConFotoNuevaLaAdjunta(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	id := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	primera := uuid.NewString()

	body1, ct1 := buildMultipart(t, visitaBodyJSON(id.String(), fecha), []testImagen{jpeg(primera, "")})
	require.Equal(t, http.StatusCreated, postMultipart(t, f, body1, ct1, id.String()).Code)

	segunda := uuid.NewString()
	body2, ct2 := buildMultipart(t, visitaBodyJSON(id.String(), fecha),
		[]testImagen{jpeg(primera, ""), jpeg(segunda, "")})
	rec := postMultipart(t, f, body2, ct2, id.String())

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	assert.Len(t, f.repo.byID, 1)
	assert.Equal(t, 2, f.imgRepo.count(id), "the new photo landed on the existing visita")
	assert.Equal(t, 0, f.store.deleteCalls)
}

func TestHTTP_CrearVisita_Multipart_SinIDDeImagenElServidorLoGenera(t *testing.T) {
	t.Parallel()

	// A client that omits id_<n> still gets its photo stored — it just loses
	// per-image replay safety, which the DTO doc spells out.
	f := newMultipartFixture(t)
	id := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	body, ct := buildMultipart(t, visitaBodyJSON(id.String(), fecha),
		[]testImagen{jpeg("", "")})

	rec := postMultipart(t, f, body, ct, id.String())

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, 1, f.imgRepo.count(id))
	require.Len(t, f.store.keys(), 1)
	assert.Regexp(t, `^visitas/`+id.String()+`/[0-9a-f-]{36}\.jpg$`, f.store.keys()[0])
}

// ─── Whitelist ─────────────────────────────────────────────────────────────

func TestHTTP_CrearVisita_Multipart_RechazaMimeFueraDeLaWhitelist(t *testing.T) {
	t.Parallel()

	for _, mime := range []string{"image/bmp", "image/svg+xml", "text/html", "application/zip"} {
		t.Run(mime, func(t *testing.T) {
			t.Parallel()

			f := newMultipartFixture(t)
			id := uuid.New()
			fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
			body, ct := buildMultipart(t, visitaBodyJSON(id.String(), fecha), []testImagen{
				{Filename: "x.bin", ContentType: mime, Body: []byte("data"), ID: uuid.NewString()},
			})

			rec := postMultipart(t, f, body, ct, id.String())

			require.GreaterOrEqual(t, rec.Code, 400, "body=%s", rec.Body.String())
			require.Less(t, rec.Code, 500, "a bad file type is the client's mistake, never a 500")
			assert.Contains(t, rec.Body.String(), "visita_imagen_mime_no_permitido",
				"the client must get a distinguishable code")
			assert.Empty(t, f.store.keys(), "nothing reached the disk")
			assert.Empty(t, f.repo.byID, "and the visita was not written either")
		})
	}
}

func TestHTTP_CrearVisita_Multipart_RechazaParteSinContentType(t *testing.T) {
	t.Parallel()

	// Go's CreateFormFile omits the per-part Content-Type. Clients MUST set
	// it (CreatePart with an explicit header); a part without one has no
	// declared type and cannot be checked against the whitelist.
	f := newMultipartFixture(t)
	id := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	body, ct := buildMultipart(t, visitaBodyJSON(id.String(), fecha), []testImagen{
		{Filename: "x.jpg", ContentType: "", Body: []byte("data"), ID: uuid.NewString()},
	})

	rec := postMultipart(t, f, body, ct, id.String())

	require.GreaterOrEqual(t, rec.Code, 400)
	require.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "visita_imagen_mime_no_permitido")
}

// ─── Malformed input never yields a 500 ────────────────────────────────────

func TestHTTP_CrearVisita_Multipart_EntradasMalformadasNuncaSon500(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		body        []byte
		contentType string
		wantCode    string
	}{
		{
			name:        "boundary ausente en el Content-Type",
			body:        []byte("--x\r\nContent-Disposition: form-data; name=\"datos\"\r\n\r\n{}\r\n--x--\r\n"),
			contentType: "multipart/form-data",
			wantCode:    "multipart_boundary_faltante",
		},
		{
			name:        "cuerpo truncado a la mitad de una parte",
			body:        []byte("--abc123\r\nContent-Disposition: form-data; name=\"datos\"\r\n\r\n{\"id\""),
			contentType: `multipart/form-data; boundary=abc123`,
			wantCode:    "multipart_invalido",
		},
		{
			name:        "boundary que no aparece en el cuerpo",
			body:        []byte("no soy multipart"),
			contentType: `multipart/form-data; boundary=noexiste`,
			wantCode:    "multipart_invalido",
		},
		{
			// Huma rejects an empty body before the handler ever runs. The
			// point of the row is that it is still a clean 4xx, not which
			// layer produced it, so it asserts no code.
			name:        "cuerpo vacío",
			body:        []byte(""),
			contentType: `multipart/form-data; boundary=abc123`,
			wantCode:    "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMultipartFixture(t)
			rec := postMultipart(t, f, tc.body, tc.contentType, "")

			require.GreaterOrEqual(t, rec.Code, 400, "body=%s", rec.Body.String())
			require.Less(t, rec.Code, 500,
				"a malformed multipart is a client mistake; a 500 here would read as the server "+
					"losing a visit; body=%s", rec.Body.String())
			if tc.wantCode != "" {
				assert.Contains(t, rec.Body.String(), tc.wantCode)
			}
			assert.Empty(t, f.repo.byID)
			assert.Empty(t, f.store.keys())
		})
	}
}

func TestHTTP_CrearVisita_Multipart_DatosFaltante(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	body, ct := buildMultipart(t, "", []testImagen{jpeg(uuid.NewString(), "")})

	rec := postMultipart(t, f, body, ct, "")

	require.GreaterOrEqual(t, rec.Code, 400)
	require.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "datos_requerido")
}

func TestHTTP_CrearVisita_Multipart_DatosNoEsJSON(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	body, ct := buildMultipart(t, "{no soy json", nil)

	rec := postMultipart(t, f, body, ct, "")

	require.GreaterOrEqual(t, rec.Code, 400)
	require.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "visita_body_json_invalido")
}

func TestHTTP_CrearVisita_Multipart_IDDeImagenNoEsUUID(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	id := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	body, ct := buildMultipart(t, visitaBodyJSON(id.String(), fecha),
		[]testImagen{jpeg("no-soy-un-uuid", "")})

	rec := postMultipart(t, f, body, ct, id.String())

	require.GreaterOrEqual(t, rec.Code, 400)
	require.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "imagen_id_invalido")
	assert.Empty(t, f.store.keys())
}

func TestHTTP_CrearVisita_Multipart_IDsDeImagenDuplicados(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	id := uuid.New()
	imgID := uuid.NewString()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	body, ct := buildMultipart(t, visitaBodyJSON(id.String(), fecha),
		[]testImagen{jpeg(imgID, ""), jpeg(imgID, "")})

	rec := postMultipart(t, f, body, ct, id.String())

	require.GreaterOrEqual(t, rec.Code, 400)
	require.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "visita_imagen_id_duplicado")
	assert.Empty(t, f.store.keys())
}

func TestHTTP_CrearVisita_Multipart_TipoFueraDelCatalogo(t *testing.T) {
	t.Parallel()

	// Task 7's closed catalogue still governs the WRITE path when the request
	// arrives as multipart — with the same distinguishable code, never a 500.
	f := newMultipartFixture(t)
	id := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	datos := strings.Replace(visitaBodyJSON(id.String(), fecha),
		`"tipo_visita":"No se encontraba"`, `"tipo_visita":"inventado"`, 1)
	body, ct := buildMultipart(t, datos, []testImagen{jpeg(uuid.NewString(), "")})

	rec := postMultipart(t, f, body, ct, id.String())

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "visita_tipo_invalido")
	assert.Empty(t, f.store.keys(), "an invalid visita writes no blob")
}

func TestHTTP_CrearVisita_Multipart_IdempotencyKeyQueNoCoincide(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	id := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	body, ct := buildMultipart(t, visitaBodyJSON(id.String(), fecha), []testImagen{jpeg(uuid.NewString(), "")})

	rec := postMultipart(t, f, body, ct, uuid.NewString())

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "idempotency_key_mismatch")
}

// ─── Content-Type negotiation ──────────────────────────────────────────────

func TestHTTP_CrearVisita_ContentTypeNoSoportado_Returns415(t *testing.T) {
	t.Parallel()

	for _, ct := range []string{"", "text/plain", "application/xml", "application/x-www-form-urlencoded"} {
		t.Run("ct="+ct, func(t *testing.T) {
			t.Parallel()

			f := newMultipartFixture(t)
			id := uuid.New()
			fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)

			req := httptest.NewRequest(http.MethodPost, "/visitas",
				bytes.NewBufferString(visitaBodyJSON(id.String(), fecha)))
			if ct != "" {
				req.Header.Set("Content-Type", ct)
			}
			rec := httptest.NewRecorder()
			mountWithUser(visitaUser(), f.svc).ServeHTTP(rec, req)

			assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code, "body=%s", rec.Body.String())
			assert.Contains(t, rec.Body.String(), "content_type_no_soportado")
			assert.Empty(t, f.repo.byID)
		})
	}
}

// ─── The published contract ────────────────────────────────────────────────

func TestOpenAPI_CrearVisita_DeclaraLosDosContentTypes(t *testing.T) {
	t.Parallel()

	f := newMultipartFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), f.svc).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var spec map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &spec))

	paths, _ := spec["paths"].(map[string]any)
	visitas, _ := paths["/visitas"].(map[string]any)
	post, _ := visitas["post"].(map[string]any)
	requestBody, _ := post["requestBody"].(map[string]any)
	content, _ := requestBody["content"].(map[string]any)

	require.Contains(t, content, "application/json",
		"the legacy contract must stay documented, not just implemented")
	require.Contains(t, content, "multipart/form-data")
	assert.NotContains(t, content, "application/octet-stream",
		"the opaque body Huma infers for a []byte RawBody must not leak into the contract")
}
