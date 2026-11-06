//nolint:misspell // vocabulario español del módulo y nombres del contrato.
package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

type fakeAdjuntoEventoRepoD4 struct {
	eventos []*domain.Evento
	err     error
	calls   int
}

func (f *fakeAdjuntoEventoRepoD4) ListarPorGarantia(
	context.Context,
	uuid.UUID,
) ([]*domain.Evento, error) {
	f.calls++
	return f.eventos, f.err
}

func (f *fakeAdjuntoEventoRepoD4) ObtenerPorClaveIdempotencia(
	context.Context,
	string,
) (*domain.Evento, error) {
	return nil, nil
}

type fakeAdjuntoImagenRepoD4 struct {
	imagen       *domain.Imagen
	obtenerErr   error
	registrarErr error
	registrar    int
	obtener      int
	onRegistrar  func(*domain.Imagen)
}

func (f *fakeAdjuntoImagenRepoD4) Registrar(
	_ context.Context,
	img *domain.Imagen,
) error {
	f.registrar++

	if f.onRegistrar != nil {
		f.onRegistrar(img)
	}

	if f.registrarErr != nil {
		return f.registrarErr
	}

	f.imagen = img
	f.obtenerErr = nil
	return nil
}

func (f *fakeAdjuntoImagenRepoD4) Obtener(
	context.Context,
	uuid.UUID,
) (*domain.Imagen, error) {
	f.obtener++

	if f.imagen != nil {
		return f.imagen, nil
	}

	if f.obtenerErr != nil {
		return nil, f.obtenerErr
	}

	return nil, domain.ErrImagenNoEncontrada
}

func (f *fakeAdjuntoImagenRepoD4) ListarPorEvento(
	context.Context,
	uuid.UUID,
) ([]*domain.Imagen, error) {
	return nil, nil
}

func (f *fakeAdjuntoImagenRepoD4) ListarPorEventos(
	context.Context,
	[]uuid.UUID,
) ([]*domain.Imagen, error) {
	return nil, nil
}

type fakeAdjuntoStorageD4 struct {
	storeCalls int
	deleteKeys []string

	storeKey         string
	storeContentType string
	storeSize        int64
	storeBody        []byte
	storeErr         error
	deleteErr        error
}

func (f *fakeAdjuntoStorageD4) Store(
	_ context.Context,
	key string,
	contentType string,
	sizeBytes int64,
	body io.Reader,
) error {
	f.storeCalls++
	f.storeKey = key
	f.storeContentType = contentType
	f.storeSize = sizeBytes

	contenido, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.storeBody = contenido

	return f.storeErr
}

func (f *fakeAdjuntoStorageD4) Get(
	context.Context,
	string,
) (outbound.StorageObject, error) {
	return outbound.StorageObject{}, nil
}

func (f *fakeAdjuntoStorageD4) Delete(
	_ context.Context,
	key string,
) error {
	f.deleteKeys = append(f.deleteKeys, key)
	return f.deleteErr
}

type fakeAdjuntoIdentityD4 struct {
	usuario    outbound.Usuario
	usuarioErr error
	permitido  bool
	permisoErr error
	permisos   []domain.Permiso
}

func (f *fakeAdjuntoIdentityD4) UsuarioActual(
	context.Context,
) (outbound.Usuario, error) {
	if f.usuarioErr != nil {
		return outbound.Usuario{}, f.usuarioErr
	}
	return f.usuario, nil
}

func (f *fakeAdjuntoIdentityD4) TienePermiso(
	_ context.Context,
	p domain.Permiso,
) (bool, error) {
	f.permisos = append(f.permisos, p)

	if f.permisoErr != nil {
		return false, f.permisoErr
	}

	return f.permitido, nil
}

type fakeAdjuntoTxD4 struct {
	calls int
}

func (f *fakeAdjuntoTxD4) RunInTx(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	f.calls++
	return fn(ctx)
}

type fakeAdjuntoClockD4 struct {
	now time.Time
}

func (f fakeAdjuntoClockD4) Now() time.Time {
	return f.now
}

func adjuntoPNGPruebaD4() []byte {
	return append(
		[]byte("\x89PNG\r\n\x1a\n"),
		[]byte("evidencia de garantia")...,
	)
}

func adjuntoEventoD4(eventoID, garantiaID uuid.UUID) *domain.Evento {
	return domain.HydrateEvento(domain.HydrateEventoParams{
		ID:         eventoID,
		GarantiaID: garantiaID,
	})
}

func adjuntoImagenD4(
	id uuid.UUID,
	eventoID uuid.UUID,
	ruta string,
	huella string,
) *domain.Imagen {
	return domain.HydrateImagen(domain.HydrateImagenParams{
		ID:          id,
		EventoID:    eventoID,
		Tipo:        domain.TipoImagenEvidencia,
		Ruta:        ruta,
		Description: "foto inicial",
		SubidaPor:   "Ruben",
		Huella:      huella,
		CreatedAt:   time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC),
	})
}

func adjuntoServicioD4(
	eventos *fakeAdjuntoEventoRepoD4,
	imagenes *fakeAdjuntoImagenRepoD4,
	storage *fakeAdjuntoStorageD4,
	identity *fakeAdjuntoIdentityD4,
	tx *fakeAdjuntoTxD4,
	now time.Time,
) *app.Service {
	return app.NewService(app.Deps{
		EventoRepo: eventos,
		ImagenRepo: imagenes,
		TxRunner:   tx,
		Identity:   identity,
		Clock:      fakeAdjuntoClockD4{now: now},
		Storage:    storage,
	})
}

func adjuntoIdentityPermitidaD4() *fakeAdjuntoIdentityD4 {
	return &fakeAdjuntoIdentityD4{
		usuario: outbound.Usuario{
			ID:     "usuario-1",
			Nombre: "Ruben",
		},
		permitido: true,
	}
}

func TestAdjuntarImagen_SinUsuario_NoEscribe(t *testing.T) {
	t.Parallel()

	storage := &fakeAdjuntoStorageD4{}
	tx := &fakeAdjuntoTxD4{}
	identity := &fakeAdjuntoIdentityD4{
		usuarioErr: domain.ErrUsuarioNoAutenticado,
	}

	s := adjuntoServicioD4(
		&fakeAdjuntoEventoRepoD4{},
		&fakeAdjuntoImagenRepoD4{},
		storage,
		identity,
		tx,
		time.Now().UTC(),
	)

	_, err := s.AdjuntarImagen(context.Background(), app.AdjuntarImagenCmd{
		GarantiaID:        uuid.New(),
		EventoID:          uuid.New(),
		Contenido:         adjuntoPNGPruebaD4(),
		ClaveIdempotencia: uuid.NewString(),
	})

	require.ErrorIs(t, err, domain.ErrUsuarioNoAutenticado)
	require.Zero(t, storage.storeCalls)
	require.Zero(t, tx.calls)
	require.Empty(t, identity.permisos)
}

func TestAdjuntarImagen_SinPermiso_NoEscribe(t *testing.T) {
	t.Parallel()

	storage := &fakeAdjuntoStorageD4{}
	tx := &fakeAdjuntoTxD4{}
	identity := adjuntoIdentityPermitidaD4()
	identity.permitido = false

	s := adjuntoServicioD4(
		&fakeAdjuntoEventoRepoD4{},
		&fakeAdjuntoImagenRepoD4{},
		storage,
		identity,
		tx,
		time.Now().UTC(),
	)

	_, err := s.AdjuntarImagen(context.Background(), app.AdjuntarImagenCmd{
		GarantiaID:        uuid.New(),
		EventoID:          uuid.New(),
		Contenido:         adjuntoPNGPruebaD4(),
		ClaveIdempotencia: uuid.NewString(),
	})

	require.ErrorIs(t, err, domain.ErrPermisoDenegado)
	require.Zero(t, storage.storeCalls)
	require.Zero(t, tx.calls)
	require.Equal(t, []domain.Permiso{domain.PermisoActualizar}, identity.permisos)
}

func TestAdjuntarImagen_EventoAjeno_BorraBlob(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()

	storage := &fakeAdjuntoStorageD4{}
	imagenes := &fakeAdjuntoImagenRepoD4{
		obtenerErr: domain.ErrImagenNoEncontrada,
	}
	tx := &fakeAdjuntoTxD4{}

	s := adjuntoServicioD4(
		&fakeAdjuntoEventoRepoD4{},
		imagenes,
		storage,
		adjuntoIdentityPermitidaD4(),
		tx,
		time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC),
	)

	_, err := s.AdjuntarImagen(context.Background(), app.AdjuntarImagenCmd{
		GarantiaID:        garantiaID,
		EventoID:          eventoID,
		Description:       "foto",
		Contenido:         adjuntoPNGPruebaD4(),
		ClaveIdempotencia: uuid.NewString(),
	})

	require.ErrorIs(t, err, domain.ErrImagenEventoAjeno)
	require.Equal(t, 1, storage.storeCalls)
	require.Len(t, storage.deleteKeys, 1)
	require.Equal(t, storage.storeKey, storage.deleteKeys[0])
	require.Equal(t, 1, tx.calls)
	require.Zero(t, imagenes.registrar)
}

func TestAdjuntarImagen_Exito(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()
	clave := uuid.NewString()
	imagenID := uuid.MustParse(clave)
	now := time.Date(2026, 11, 4, 8, 30, 0, 0, time.UTC)
	contenido := adjuntoPNGPruebaD4()

	eventos := &fakeAdjuntoEventoRepoD4{
		eventos: []*domain.Evento{
			adjuntoEventoD4(eventoID, garantiaID),
		},
	}
	imagenes := &fakeAdjuntoImagenRepoD4{
		obtenerErr: domain.ErrImagenNoEncontrada,
	}
	storage := &fakeAdjuntoStorageD4{}
	tx := &fakeAdjuntoTxD4{}
	identity := adjuntoIdentityPermitidaD4()

	s := adjuntoServicioD4(
		eventos,
		imagenes,
		storage,
		identity,
		tx,
		now,
	)

	cmd := app.AdjuntarImagenCmd{
		GarantiaID:        garantiaID,
		EventoID:          eventoID,
		Description:       "foto inicial",
		Contenido:         contenido,
		ClaveIdempotencia: clave,
	}

	got, err := s.AdjuntarImagen(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, imagenID, got.ID())
	require.Equal(t, eventoID, got.EventoID())
	require.Equal(t, domain.TipoImagenEvidencia, got.Tipo())
	require.Equal(t, "foto inicial", got.Description())
	require.Equal(t, "Ruben", got.SubidaPor())
	require.Equal(t, now, got.CreatedAt())
	require.Equal(t, app.HuellaDeParaPruebas(cmd), got.Huella())

	require.Equal(t, 1, storage.storeCalls)
	require.Equal(t, "image/png", storage.storeContentType)
	require.Equal(t, int64(len(contenido)), storage.storeSize)
	require.True(t, bytes.Equal(contenido, storage.storeBody))
	require.True(
		t,
		strings.HasPrefix(
			storage.storeKey,
			"garantias/"+garantiaID.String()+"/",
		),
	)
	require.Empty(t, storage.deleteKeys)

	require.Equal(t, 1, imagenes.registrar)
	require.Equal(t, 1, tx.calls)
	require.Equal(t, []domain.Permiso{domain.PermisoActualizar}, identity.permisos)
}

func TestAdjuntarImagen_ReintentoMismoContenido(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()
	clave := uuid.NewString()
	imagenID := uuid.MustParse(clave)

	cmd := app.AdjuntarImagenCmd{
		GarantiaID:        garantiaID,
		EventoID:          eventoID,
		Description:       "foto inicial",
		Contenido:         adjuntoPNGPruebaD4(),
		ClaveIdempotencia: clave,
	}

	existente := adjuntoImagenD4(
		imagenID,
		eventoID,
		"garantias/"+garantiaID.String()+"/ganadora.png",
		app.HuellaDeParaPruebas(cmd),
	)

	imagenes := &fakeAdjuntoImagenRepoD4{
		imagen: existente,
	}
	storage := &fakeAdjuntoStorageD4{}

	s := adjuntoServicioD4(
		&fakeAdjuntoEventoRepoD4{
			eventos: []*domain.Evento{
				adjuntoEventoD4(eventoID, garantiaID),
			},
		},
		imagenes,
		storage,
		adjuntoIdentityPermitidaD4(),
		&fakeAdjuntoTxD4{},
		time.Now().UTC(),
	)

	got, err := s.AdjuntarImagen(context.Background(), cmd)
	require.NoError(t, err)
	require.Same(t, existente, got)

	require.Zero(t, imagenes.registrar)
	require.Equal(t, 1, storage.storeCalls)
	require.Len(t, storage.deleteKeys, 1)
	require.Equal(t, storage.storeKey, storage.deleteKeys[0])
}

func TestAdjuntarImagen_ReintentoContenidoDistinto_Conflicto(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nombre string
		mutar  func(*app.AdjuntarImagenCmd)
	}{
		{
			nombre: "bytes",
			mutar: func(cmd *app.AdjuntarImagenCmd) {
				cmd.Contenido = append(
					adjuntoPNGPruebaD4(),
					[]byte("-distinta")...,
				)
			},
		},
		{
			nombre: "descripcion",
			mutar: func(cmd *app.AdjuntarImagenCmd) {
				cmd.Description = "otra descripcion"
			},
		},
	}

	for _, tc := range casos {
		tc := tc
		t.Run(tc.nombre, func(t *testing.T) {
			t.Parallel()

			garantiaID := uuid.New()
			eventoID := uuid.New()
			clave := uuid.NewString()
			imagenID := uuid.MustParse(clave)

			original := app.AdjuntarImagenCmd{
				GarantiaID:        garantiaID,
				EventoID:          eventoID,
				Description:       "foto inicial",
				Contenido:         adjuntoPNGPruebaD4(),
				ClaveIdempotencia: clave,
			}

			existente := adjuntoImagenD4(
				imagenID,
				eventoID,
				"garantias/"+garantiaID.String()+"/ganadora.png",
				app.HuellaDeParaPruebas(original),
			)

			cmd := original
			cmd.Contenido = append([]byte(nil), original.Contenido...)
			tc.mutar(&cmd)

			imagenes := &fakeAdjuntoImagenRepoD4{
				imagen: existente,
			}
			storage := &fakeAdjuntoStorageD4{}

			s := adjuntoServicioD4(
				&fakeAdjuntoEventoRepoD4{
					eventos: []*domain.Evento{
						adjuntoEventoD4(eventoID, garantiaID),
					},
				},
				imagenes,
				storage,
				adjuntoIdentityPermitidaD4(),
				&fakeAdjuntoTxD4{},
				time.Now().UTC(),
			)

			_, err := s.AdjuntarImagen(context.Background(), cmd)

			require.ErrorIs(
				t,
				err,
				domain.ErrClaveIdempotenciaMismatch,
			)
			require.Zero(t, imagenes.registrar)
			require.Len(t, storage.deleteKeys, 1)
			require.Equal(t, storage.storeKey, storage.deleteKeys[0])
		})
	}
}

func TestAdjuntarImagen_ErrorRepositorio_BorraBlob(t *testing.T) {
	t.Parallel()

	errDB := errors.New("firebird no disponible")

	garantiaID := uuid.New()
	eventoID := uuid.New()

	imagenes := &fakeAdjuntoImagenRepoD4{
		obtenerErr:   domain.ErrImagenNoEncontrada,
		registrarErr: errDB,
	}
	storage := &fakeAdjuntoStorageD4{}

	s := adjuntoServicioD4(
		&fakeAdjuntoEventoRepoD4{
			eventos: []*domain.Evento{
				adjuntoEventoD4(eventoID, garantiaID),
			},
		},
		imagenes,
		storage,
		adjuntoIdentityPermitidaD4(),
		&fakeAdjuntoTxD4{},
		time.Now().UTC(),
	)

	_, err := s.AdjuntarImagen(context.Background(), app.AdjuntarImagenCmd{
		GarantiaID:        garantiaID,
		EventoID:          eventoID,
		Description:       "foto",
		Contenido:         adjuntoPNGPruebaD4(),
		ClaveIdempotencia: uuid.NewString(),
	})

	require.ErrorIs(t, err, errDB)
	require.Equal(t, 1, imagenes.registrar)
	require.Len(t, storage.deleteKeys, 1)
	require.Equal(t, storage.storeKey, storage.deleteKeys[0])
}

func TestAdjuntarImagen_DuplicadaConcurrente_ResuelveReintento(t *testing.T) {
	t.Parallel()

	garantiaID := uuid.New()
	eventoID := uuid.New()
	clave := uuid.NewString()
	imagenID := uuid.MustParse(clave)

	cmd := app.AdjuntarImagenCmd{
		GarantiaID:        garantiaID,
		EventoID:          eventoID,
		Description:       "foto",
		Contenido:         adjuntoPNGPruebaD4(),
		ClaveIdempotencia: clave,
	}

	huella := app.HuellaDeParaPruebas(cmd)
	ganadora := adjuntoImagenD4(
		imagenID,
		eventoID,
		"garantias/"+garantiaID.String()+"/ganadora.png",
		huella,
	)

	imagenes := &fakeAdjuntoImagenRepoD4{
		obtenerErr:   domain.ErrImagenNoEncontrada,
		registrarErr: domain.ErrImagenDuplicada,
	}
	imagenes.onRegistrar = func(*domain.Imagen) {
		imagenes.imagen = ganadora
		imagenes.obtenerErr = nil
	}

	storage := &fakeAdjuntoStorageD4{}

	s := adjuntoServicioD4(
		&fakeAdjuntoEventoRepoD4{
			eventos: []*domain.Evento{
				adjuntoEventoD4(eventoID, garantiaID),
			},
		},
		imagenes,
		storage,
		adjuntoIdentityPermitidaD4(),
		&fakeAdjuntoTxD4{},
		time.Now().UTC(),
	)

	got, err := s.AdjuntarImagen(context.Background(), cmd)
	require.NoError(t, err)
	require.Same(t, ganadora, got)

	require.Equal(t, 1, imagenes.registrar)
	require.Len(t, storage.deleteKeys, 1)
	require.Equal(t, storage.storeKey, storage.deleteKeys[0])
}
