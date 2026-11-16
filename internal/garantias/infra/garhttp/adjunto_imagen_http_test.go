//nolint:misspell // vocabulario español del módulo.
package garhttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garhttp"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

type fakeAdjuntoHTTPImagenRepo struct {
	imagen    *domain.Imagen
	registrar int
	obtener   int
}

func (f *fakeAdjuntoHTTPImagenRepo) Registrar(
	_ context.Context,
	img *domain.Imagen,
) error {
	f.registrar++
	f.imagen = img
	return nil
}

func (f *fakeAdjuntoHTTPImagenRepo) Obtener(
	_ context.Context,
	_ uuid.UUID,
) (*domain.Imagen, error) {
	f.obtener++
	if f.imagen == nil {
		return nil, domain.ErrImagenNoEncontrada
	}
	return f.imagen, nil
}

func (f *fakeAdjuntoHTTPImagenRepo) ListarPorEvento(
	context.Context,
	uuid.UUID,
) ([]*domain.Imagen, error) {
	return nil, nil
}

func (f *fakeAdjuntoHTTPImagenRepo) ListarPorEventos(
	context.Context,
	[]uuid.UUID,
) ([]*domain.Imagen, error) {
	return nil, nil
}

type fakeAdjuntoHTTPStorage struct {
	storeCalls  int
	deleteCalls int
	key         string
	contenido   []byte
}

func (f *fakeAdjuntoHTTPStorage) Store(
	_ context.Context,
	key string,
	_ string,
	_ int64,
	body io.Reader,
) error {
	f.storeCalls++
	f.key = key

	contenido, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.contenido = contenido

	return nil
}

func (f *fakeAdjuntoHTTPStorage) Get(
	context.Context,
	string,
) (outbound.StorageObject, error) {
	return outbound.StorageObject{}, nil
}

func (f *fakeAdjuntoHTTPStorage) Delete(
	context.Context,
	string,
) error {
	f.deleteCalls++
	return nil
}

type fakeAdjuntoHTTPTx struct {
	calls int
}

func (f *fakeAdjuntoHTTPTx) RunInTx(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	f.calls++
	return fn(ctx)
}

type fakeAdjuntoHTTPClock struct {
	now time.Time
}

func (f fakeAdjuntoHTTPClock) Now() time.Time {
	return f.now
}

func adjuntoHTTPRouter(
	identity outbound.Identity,
	eventos outbound.EventoRepo,
	imagenes outbound.ImagenRepo,
	storage outbound.StorageProvider,
	tx outbound.TxRunner,
	clock outbound.Clock,
) http.Handler {
	consultas := app.NewConsultas(app.ConsultasDeps{
		Eventos:  eventos,
		Imagenes: imagenes,
		Storage:  storage,
		Identity: identity,
	})

	service := app.NewService(app.Deps{
		EventoRepo: eventos,
		ImagenRepo: imagenes,
		TxRunner:   tx,
		Identity:   identity,
		Clock:      clock,
		Storage:    storage,
	})

	r := chi.NewRouter()
	garhttp.MountRouter(r, garhttp.Deps{
		Consultas: consultas,
		Comandos:  service,
	})

	return r
}

func adjuntoHTTPMultipart(
	t *testing.T,
	contenido []byte,
	descripcion string,
) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	require.NoError(
		t,
		writer.WriteField("descripcion", descripcion),
	)

	parte, err := writer.CreateFormFile("archivo", "evidencia.png")
	require.NoError(t, err)

	_, err = parte.Write(contenido)
	require.NoError(t, err)

	require.NoError(t, writer.Close())

	return &body, writer.FormDataContentType()
}

func adjuntoHTTPPNG() []byte {
	return append(
		[]byte("\x89PNG\r\n\x1a\n"),
		[]byte("evidencia")...,
	)
}

func adjuntoHTTPEvento(
	eventoID uuid.UUID,
	garantiaID uuid.UUID,
) *domain.Evento {
	return domain.HydrateEvento(domain.HydrateEventoParams{
		ID:         eventoID,
		GarantiaID: garantiaID,
	})
}

func TestAdjuntarImagenHTTP_CaminoFeliz(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()
	clave := uuid.NewString()
	contenido := adjuntoHTTPPNG()
	now := time.Date(2026, 11, 4, 8, 30, 0, 0, time.UTC)

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	eventos := &fakeHTTPEventoRepo{
		eventos: []*domain.Evento{
			adjuntoHTTPEvento(eventoID, garantiaID),
		},
	}

	imagenes := &fakeAdjuntoHTTPImagenRepo{}
	storage := &fakeAdjuntoHTTPStorage{}
	tx := &fakeAdjuntoHTTPTx{}

	r := adjuntoHTTPRouter(
		identity,
		eventos,
		imagenes,
		storage,
		tx,
		fakeAdjuntoHTTPClock{now: now},
	)

	body, contentType := adjuntoHTTPMultipart(
		t,
		contenido,
		"foto del daño",
	)

	req := httptest.NewRequest(
		http.MethodPost,
		"/garantias/"+garantiaID.String()+
			"/eventos/"+eventoID.String()+"/imagenes",
		body,
	)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Idempotency-Key", clave)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var respuesta map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &respuesta))

	require.Equal(t, clave, respuesta["id"])
	require.Equal(t, "evidencia", respuesta["tipo"])
	require.Equal(t, "foto del daño", respuesta["descripcion"])
	require.Equal(t, "Ruben", respuesta["subida_por"])
	require.Equal(t, now.Format(time.RFC3339), respuesta["created_at"])
	require.Equal(
		t,
		"/garantias/"+garantiaID.String()+
			"/eventos/"+eventoID.String()+
			"/imagenes/"+clave,
		respuesta["url"],
	)

	require.Equal(t, 1, storage.storeCalls)
	require.Equal(t, contenido, storage.contenido)
	require.Zero(t, storage.deleteCalls)
	require.Equal(t, 1, imagenes.registrar)
	require.Equal(t, 1, tx.calls)
}

func TestAdjuntarImagenHTTP_SinIdempotencyKey_422(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	storage := &fakeAdjuntoHTTPStorage{}
	imagenes := &fakeAdjuntoHTTPImagenRepo{}
	tx := &fakeAdjuntoHTTPTx{}

	r := adjuntoHTTPRouter(
		identity,
		&fakeHTTPEventoRepo{},
		imagenes,
		storage,
		tx,
		fakeAdjuntoHTTPClock{now: time.Now().UTC()},
	)

	body, contentType := adjuntoHTTPMultipart(
		t,
		adjuntoHTTPPNG(),
		"foto",
	)

	req := httptest.NewRequest(
		http.MethodPost,
		"/garantias/"+garantiaID.String()+
			"/eventos/"+eventoID.String()+"/imagenes",
		body,
	)
	req.Header.Set("Content-Type", contentType)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(
		t,
		http.StatusUnprocessableEntity,
		rec.Code,
		rec.Body.String(),
	)

	require.Zero(t, storage.storeCalls)
	require.Zero(t, imagenes.registrar)
	require.Zero(t, tx.calls)
}

func TestAdjuntarImagenHTTP_ArchivoVacio_422(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	storage := &fakeAdjuntoHTTPStorage{}

	r := adjuntoHTTPRouter(
		identity,
		&fakeHTTPEventoRepo{},
		&fakeAdjuntoHTTPImagenRepo{},
		storage,
		&fakeAdjuntoHTTPTx{},
		fakeAdjuntoHTTPClock{now: time.Now().UTC()},
	)

	body, contentType := adjuntoHTTPMultipart(t, nil, "foto")

	req := httptest.NewRequest(
		http.MethodPost,
		"/garantias/"+garantiaID.String()+
			"/eventos/"+eventoID.String()+"/imagenes",
		body,
	)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Idempotency-Key", uuid.NewString())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(
		t,
		http.StatusUnprocessableEntity,
		rec.Code,
		rec.Body.String(),
	)
	require.Contains(t, rec.Body.String(), "warranty_image_empty")
	require.Zero(t, storage.storeCalls)
}

func TestAdjuntarImagenHTTP_MayorA10MB_422(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}

	storage := &fakeAdjuntoHTTPStorage{}

	r := adjuntoHTTPRouter(
		identity,
		&fakeHTTPEventoRepo{},
		&fakeAdjuntoHTTPImagenRepo{},
		storage,
		&fakeAdjuntoHTTPTx{},
		fakeAdjuntoHTTPClock{now: time.Now().UTC()},
	)

	contenido := make([]byte, 10*1024*1024+1)
	copy(contenido, adjuntoHTTPPNG())

	body, contentType := adjuntoHTTPMultipart(t, contenido, "foto")

	req := httptest.NewRequest(
		http.MethodPost,
		"/garantias/"+garantiaID.String()+
			"/eventos/"+eventoID.String()+"/imagenes",
		body,
	)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Idempotency-Key", uuid.NewString())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(
		t,
		http.StatusUnprocessableEntity,
		rec.Code,
		rec.Body.String(),
	)
	require.Contains(t, rec.Body.String(), "warranty_image_too_large")
	require.Zero(t, storage.storeCalls)
}

// TestAdjuntarImagenHTTP_NoEsImagen_422 rechaza contenido no permitido.
func TestAdjuntarImagenHTTP_NoEsImagen_422(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()

	identity := &fakeHTTPIdentity{
		usuario:   outbound.Usuario{ID: "usuario-1", Nombre: "Operador"},
		permitido: true,
	}
	eventos := &fakeHTTPEventoRepo{
		eventos: []*domain.Evento{adjuntoHTTPEvento(eventoID, garantiaID)},
	}
	imagenes := &fakeAdjuntoHTTPImagenRepo{}
	storage := &fakeAdjuntoHTTPStorage{}
	tx := &fakeAdjuntoHTTPTx{}

	r := adjuntoHTTPRouter(
		identity, eventos, imagenes, storage, tx,
		fakeAdjuntoHTTPClock{now: time.Now().UTC()},
	)

	body, contentType := adjuntoHTTPMultipart(t, []byte("no soy una imagen"), "archivo invalido")
	req := httptest.NewRequest(
		http.MethodPost,
		"/garantias/"+garantiaID.String()+"/eventos/"+eventoID.String()+"/imagenes",
		body,
	)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Idempotency-Key", uuid.NewString())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Zero(t, storage.storeCalls)
	require.Zero(t, imagenes.registrar)
}

// TestAdjuntarImagenHTTP_EventoOtraGarantia_404 evita adjuntar a otro folio.

// fakeEventoFiltradoHTTP respeta el ID de garantía en ListarPorGarantia.
type fakeEventoFiltradoHTTP struct {
	*fakeHTTPEventoRepo
}

func (f *fakeEventoFiltradoHTTP) ListarPorGarantia(
	ctx context.Context,
	garantiaID uuid.UUID,
) ([]*domain.Evento, error) {
	eventos, err := f.fakeHTTPEventoRepo.ListarPorGarantia(ctx, garantiaID)
	if err != nil {
		return nil, err
	}

	var filtrados []*domain.Evento
	for _, evento := range eventos {
		if evento != nil && evento.GarantiaID() == garantiaID {
			filtrados = append(filtrados, evento)
		}
	}
	return filtrados, nil
}

func TestAdjuntarImagenHTTP_EventoOtraGarantia_404(t *testing.T) {
	t.Parallel()

	garantiaSolicitada := uuid.New()
	garantiaDelEvento := uuid.New()
	eventoID := uuid.New()

	identity := &fakeHTTPIdentity{
		usuario:   outbound.Usuario{ID: "usuario-1", Nombre: "Operador"},
		permitido: true,
	}
	eventos := &fakeEventoFiltradoHTTP{
		fakeHTTPEventoRepo: &fakeHTTPEventoRepo{
			eventos: []*domain.Evento{
				adjuntoHTTPEvento(eventoID, garantiaDelEvento),
			},
		},
	}
	imagenes := &fakeAdjuntoHTTPImagenRepo{}
	storage := &fakeAdjuntoHTTPStorage{}
	tx := &fakeAdjuntoHTTPTx{}

	r := adjuntoHTTPRouter(
		identity, eventos, imagenes, storage, tx,
		fakeAdjuntoHTTPClock{now: time.Now().UTC()},
	)

	body, contentType := adjuntoHTTPMultipart(t, adjuntoHTTPPNG(), "foto")
	req := httptest.NewRequest(
		http.MethodPost,
		"/garantias/"+garantiaSolicitada.String()+"/eventos/"+eventoID.String()+"/imagenes",
		body,
	)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Idempotency-Key", uuid.NewString())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Zero(t, imagenes.registrar)
}

// TestAdjuntarImagenHTTP_MismaClaveOtrosBytes_409 comprueba que
// una clave usada no acepta una imagen diferente.
func TestAdjuntarImagenHTTP_MismaClaveOtrosBytes_409(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()
	clave := uuid.NewString()

	identity := &fakeHTTPIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Operador",
		},
		permitido: true,
	}

	eventos := &fakeHTTPEventoRepo{
		eventos: []*domain.Evento{
			adjuntoHTTPEvento(eventoID, garantiaID),
		},
	}
	imagenes := &fakeAdjuntoHTTPImagenRepo{}
	storage := &fakeAdjuntoHTTPStorage{}
	tx := &fakeAdjuntoHTTPTx{}

	router := adjuntoHTTPRouter(
		identity, eventos, imagenes, storage, tx,
		fakeAdjuntoHTTPClock{now: time.Now().UTC()},
	)

	enviar := func(contenido []byte) *httptest.ResponseRecorder {
		t.Helper()

		body, contentType := adjuntoHTTPMultipart(
			t, contenido, "foto del daño",
		)

		req := httptest.NewRequest(
			http.MethodPost,
			"/garantias/"+garantiaID.String()+
				"/eventos/"+eventoID.String()+"/imagenes",
			body,
		)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Idempotency-Key", clave)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	primera := enviar(adjuntoHTTPPNG())
	require.Equal(t, http.StatusOK, primera.Code, primera.Body.String())
	require.Equal(t, 1, imagenes.registrar)

	imagenOriginal := imagenes.imagen
	require.NotNil(t, imagenOriginal)

	otrosBytes := append(adjuntoHTTPPNG(), []byte("-modificada")...)
	segunda := enviar(otrosBytes)

	require.Equal(t, http.StatusConflict, segunda.Code, segunda.Body.String())
	require.Contains(t, segunda.Body.String(), "warranty_idempotency_key_mismatch")

	require.Equal(t, 1, imagenes.registrar,
		"el reintento incorrecto no debe registrar otra imagen")
	require.Equal(t, imagenOriginal.ID(), imagenes.imagen.ID())
}
