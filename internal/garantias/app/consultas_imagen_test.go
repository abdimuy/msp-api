//nolint:misspell // vocabulario español del módulo y nombres del contrato.
package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

type fakeAdjuntoGarantiaRepo struct {
	eventos []*domain.Evento
	err     error
	calls   int
}

func (f *fakeAdjuntoGarantiaRepo) Crear(context.Context, *domain.Garantia) error {
	return nil
}

func (f *fakeAdjuntoGarantiaRepo) Guardar(context.Context, *domain.Garantia) error {
	return nil
}

func (f *fakeAdjuntoGarantiaRepo) ObtenerParaActualizar(
	context.Context,
	uuid.UUID,
) (*domain.Garantia, error) {
	return nil, nil
}

func (f *fakeAdjuntoGarantiaRepo) Obtener(
	context.Context,
	uuid.UUID,
) (*domain.Garantia, error) {
	return nil, nil
}

func (f *fakeAdjuntoGarantiaRepo) ObtenerDetalle(
	_ context.Context,
	_ uuid.UUID,
) (*domain.Garantia, []*domain.Evento, error) {
	f.calls++
	return &domain.Garantia{}, f.eventos, f.err
}

func (f *fakeAdjuntoGarantiaRepo) ObtenerPorFolio(
	context.Context,
	domain.Folio,
) (*domain.Garantia, error) {
	return nil, nil
}

type fakeAdjuntoImagenRepo struct {
	imagen *domain.Imagen
	err    error
	calls  int
}

func (f *fakeAdjuntoImagenRepo) Registrar(context.Context, *domain.Imagen) error {
	return nil
}

func (f *fakeAdjuntoImagenRepo) Obtener(
	_ context.Context,
	_ uuid.UUID,
) (*domain.Imagen, error) {
	f.calls++
	return f.imagen, f.err
}

func (f *fakeAdjuntoImagenRepo) ListarPorEvento(
	context.Context,
	uuid.UUID,
) ([]*domain.Imagen, error) {
	return nil, nil
}

func (f *fakeAdjuntoImagenRepo) ListarPorEventos(
	context.Context,
	[]uuid.UUID,
) ([]*domain.Imagen, error) {
	return nil, nil
}

type fakeAdjuntoStorage struct {
	objeto outbound.StorageObject
	err    error
	calls  int
	key    string
}

func (f *fakeAdjuntoStorage) Store(
	context.Context,
	string,
	string,
	int64,
	io.Reader,
) error {
	return nil
}

func (f *fakeAdjuntoStorage) Get(
	_ context.Context,
	key string,
) (outbound.StorageObject, error) {
	f.calls++
	f.key = key
	return f.objeto, f.err
}

func (f *fakeAdjuntoStorage) Delete(context.Context, string) error {
	return nil
}

func adjuntoIdentityLectura() *fakeConsultaIdentity {
	return &fakeConsultaIdentity{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}
}

func adjuntoEvento(eventoID, garantiaID uuid.UUID) *domain.Evento {
	return domain.HydrateEvento(domain.HydrateEventoParams{
		ID:         eventoID,
		GarantiaID: garantiaID,
	})
}

func adjuntoImagen(imagenID, eventoID uuid.UUID, ruta string) *domain.Imagen {
	return domain.HydrateImagen(domain.HydrateImagenParams{
		ID:        imagenID,
		EventoID:  eventoID,
		Tipo:      domain.TipoImagenEvidencia,
		Ruta:      ruta,
		SubidaPor: "Ruben",
		CreatedAt: time.Date(2026, time.November, 3, 12, 0, 0, 0, time.UTC),
	})
}

func TestConsultas_ObtenerImagen_CaminoFeliz(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()
	imagenID := uuid.New()
	ruta := "garantias/folio/evidencia.jpg"
	contenido := []byte("imagen-binaria")

	imagen := adjuntoImagen(imagenID, eventoID, ruta)

	garantias := &fakeAdjuntoGarantiaRepo{
		eventos: []*domain.Evento{
			adjuntoEvento(eventoID, garantiaID),
		},
	}
	imagenes := &fakeAdjuntoImagenRepo{
		imagen: imagen,
	}
	storage := &fakeAdjuntoStorage{
		objeto: outbound.StorageObject{
			Body:        io.NopCloser(bytes.NewReader(contenido)),
			ContentType: "image/jpeg",
			SizeBytes:   int64(len(contenido)),
		},
	}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: garantias,
		Imagenes:  imagenes,
		Storage:   storage,
		Identity:  adjuntoIdentityLectura(),
	})

	resultado, err := consultas.ObtenerImagen(
		context.Background(),
		garantiaID,
		eventoID,
		imagenID,
	)
	require.NoError(t, err)
	require.Same(t, imagen, resultado.Imagen)
	require.Equal(t, "image/jpeg", resultado.Objeto.ContentType)
	require.Equal(t, int64(len(contenido)), resultado.Objeto.SizeBytes)
	require.Equal(t, ruta, storage.key)
	require.Equal(t, 1, garantias.calls)
	require.Equal(t, 1, imagenes.calls)
	require.Equal(t, 1, storage.calls)

	bytesLeidos, err := io.ReadAll(resultado.Objeto.Body)
	require.NoError(t, err)
	require.Equal(t, contenido, bytesLeidos)
	require.NoError(t, resultado.Objeto.Body.Close())
}

func TestConsultas_ObtenerImagen_GarantiaNoEncontradaOcultaDetalle(t *testing.T) {
	t.Parallel()

	imagenes := &fakeAdjuntoImagenRepo{}
	storage := &fakeAdjuntoStorage{}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: &fakeAdjuntoGarantiaRepo{
			err: domain.ErrGarantiaNoEncontrada,
		},
		Imagenes: imagenes,
		Storage:  storage,
		Identity: adjuntoIdentityLectura(),
	})

	_, err := consultas.ObtenerImagen(
		context.Background(),
		uuid.New(),
		uuid.New(),
		uuid.New(),
	)

	require.ErrorIs(t, err, domain.ErrImagenNoEncontrada)
	require.Zero(t, imagenes.calls)
	require.Zero(t, storage.calls)
}

func TestConsultas_ObtenerImagen_EventoNoPerteneceAGarantia(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()

	imagenes := &fakeAdjuntoImagenRepo{}
	storage := &fakeAdjuntoStorage{}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: &fakeAdjuntoGarantiaRepo{
			eventos: []*domain.Evento{
				adjuntoEvento(eventoID, uuid.New()),
			},
		},
		Imagenes: imagenes,
		Storage:  storage,
		Identity: adjuntoIdentityLectura(),
	})

	_, err := consultas.ObtenerImagen(
		context.Background(),
		garantiaID,
		eventoID,
		uuid.New(),
	)

	require.ErrorIs(t, err, domain.ErrImagenNoEncontrada)
	require.Zero(t, imagenes.calls)
	require.Zero(t, storage.calls)
}

func TestConsultas_ObtenerImagen_ImagenNoEncontrada(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()

	storage := &fakeAdjuntoStorage{}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: &fakeAdjuntoGarantiaRepo{
			eventos: []*domain.Evento{
				adjuntoEvento(eventoID, garantiaID),
			},
		},
		Imagenes: &fakeAdjuntoImagenRepo{
			err: domain.ErrImagenNoEncontrada,
		},
		Storage:  storage,
		Identity: adjuntoIdentityLectura(),
	})

	_, err := consultas.ObtenerImagen(
		context.Background(),
		garantiaID,
		eventoID,
		uuid.New(),
	)

	require.ErrorIs(t, err, domain.ErrImagenNoEncontrada)
	require.Zero(t, storage.calls)
}

func TestConsultas_ObtenerImagen_ImagenEsDeOtroEvento(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()

	storage := &fakeAdjuntoStorage{}

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: &fakeAdjuntoGarantiaRepo{
			eventos: []*domain.Evento{
				adjuntoEvento(eventoID, garantiaID),
			},
		},
		Imagenes: &fakeAdjuntoImagenRepo{
			imagen: adjuntoImagen(
				uuid.New(),
				uuid.New(),
				"garantias/otra/imagen.jpg",
			),
		},
		Storage:  storage,
		Identity: adjuntoIdentityLectura(),
	})

	_, err := consultas.ObtenerImagen(
		context.Background(),
		garantiaID,
		eventoID,
		uuid.New(),
	)

	require.ErrorIs(t, err, domain.ErrImagenNoEncontrada)
	require.Zero(t, storage.calls)
}

func TestConsultas_ObtenerImagen_ErrorRepositorioSePropaga(t *testing.T) {
	t.Parallel()

	errRepo := errors.New("fallo de lectura")
	garantiaID := uuid.New()
	eventoID := uuid.New()

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: &fakeAdjuntoGarantiaRepo{
			err: errRepo,
		},
		Imagenes: &fakeAdjuntoImagenRepo{},
		Storage:  &fakeAdjuntoStorage{},
		Identity: adjuntoIdentityLectura(),
	})

	_, err := consultas.ObtenerImagen(
		context.Background(),
		garantiaID,
		eventoID,
		uuid.New(),
	)

	require.ErrorIs(t, err, errRepo)
}

func TestConsultas_ObtenerImagen_ErrorImagenSePropaga(t *testing.T) {
	t.Parallel()

	errRepo := errors.New("fallo imagen")
	garantiaID := uuid.New()
	eventoID := uuid.New()

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: &fakeAdjuntoGarantiaRepo{
			eventos: []*domain.Evento{
				adjuntoEvento(eventoID, garantiaID),
			},
		},
		Imagenes: &fakeAdjuntoImagenRepo{
			err: errRepo,
		},
		Storage:  &fakeAdjuntoStorage{},
		Identity: adjuntoIdentityLectura(),
	})

	_, err := consultas.ObtenerImagen(
		context.Background(),
		garantiaID,
		eventoID,
		uuid.New(),
	)

	require.ErrorIs(t, err, errRepo)
}

func TestConsultas_ObtenerImagen_ErrorStorageSePropaga(t *testing.T) {
	t.Parallel()

	errStorage := errors.New("fallo storage")
	garantiaID := uuid.New()
	eventoID := uuid.New()
	imagenID := uuid.New()

	consultas := app.NewConsultas(app.ConsultasDeps{
		Garantias: &fakeAdjuntoGarantiaRepo{
			eventos: []*domain.Evento{
				adjuntoEvento(eventoID, garantiaID),
			},
		},
		Imagenes: &fakeAdjuntoImagenRepo{
			imagen: adjuntoImagen(
				imagenID,
				eventoID,
				"garantias/folio/evidencia.jpg",
			),
		},
		Storage: &fakeAdjuntoStorage{
			err: errStorage,
		},
		Identity: adjuntoIdentityLectura(),
	})

	_, err := consultas.ObtenerImagen(
		context.Background(),
		garantiaID,
		eventoID,
		imagenID,
	)

	require.ErrorIs(t, err, errStorage)
}
