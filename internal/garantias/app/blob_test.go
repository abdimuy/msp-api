package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

type blobStorageStub struct {
	storeErr  error
	deleteErr error

	storedKey         string
	storedContentType string
	storedSize        int64
	storedBody        []byte

	deletedKeys []string
}

func (s *blobStorageStub) Store(
	_ context.Context,
	key string,
	contentType string,
	sizeBytes int64,
	body io.Reader,
) error {
	if s.storeErr != nil {
		return s.storeErr
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	s.storedKey = key
	s.storedContentType = contentType
	s.storedSize = sizeBytes
	s.storedBody = data

	return nil
}

func (s *blobStorageStub) Get(
	_ context.Context,
	_ string,
) (outbound.StorageObject, error) {
	return outbound.StorageObject{}, errors.New("Get no usado en blob_test")
}

func (s *blobStorageStub) Delete(
	_ context.Context,
	key string,
) error {
	s.deletedKeys = append(s.deletedKeys, key)
	return s.deleteErr
}

type blobImagenRepoStub struct {
	imagen     *domain.Imagen
	obtenerErr error
}

func (r *blobImagenRepoStub) Registrar(
	_ context.Context,
	_ *domain.Imagen,
) error {
	return nil
}

func (r *blobImagenRepoStub) Obtener(
	_ context.Context,
	_ uuid.UUID,
) (*domain.Imagen, error) {
	if r.obtenerErr != nil {
		return nil, r.obtenerErr
	}
	return r.imagen, nil
}

func (r *blobImagenRepoStub) ListarPorEvento(
	_ context.Context,
	_ uuid.UUID,
) ([]*domain.Imagen, error) {
	return nil, nil
}

func (r *blobImagenRepoStub) ListarPorEventos(
	_ context.Context,
	_ []uuid.UUID,
) ([]*domain.Imagen, error) {
	return nil, nil
}

func nuevaImagenBlob(id uuid.UUID, ruta string) *domain.Imagen {
	return domain.HydrateImagen(domain.HydrateImagenParams{
		ID:          id,
		EventoID:    uuid.New(),
		Tipo:        domain.TipoImagenEvidencia,
		Ruta:        ruta,
		Description: "",
		SubidaPor:   "test",
		CreatedAt:   time.Now().UTC(),
	})
}

func TestGuardarBlob_Vacio(t *testing.T) {
	t.Parallel()

	s := NewService(Deps{
		Storage: &blobStorageStub{},
	})

	_, err := s.guardarBlob(
		context.Background(),
		"garantias/test",
		nil,
		1024,
	)

	if !errors.Is(err, domain.ErrImagenVacia) {
		t.Fatalf("guardarBlob() error = %v; se esperaba %v", err, domain.ErrImagenVacia)
	}
}

func TestGuardarBlob_MuyGrande(t *testing.T) {
	t.Parallel()

	s := NewService(Deps{
		Storage: &blobStorageStub{},
	})

	_, err := s.guardarBlob(
		context.Background(),
		"garantias/test",
		[]byte{1, 2, 3, 4},
		3,
	)

	if !errors.Is(err, domain.ErrImagenMuyGrande) {
		t.Fatalf("guardarBlob() error = %v; se esperaba %v", err, domain.ErrImagenMuyGrande)
	}
}

func TestGuardarBlob_FormatoInvalido(t *testing.T) {
	t.Parallel()

	s := NewService(Deps{
		Storage: &blobStorageStub{},
	})

	_, err := s.guardarBlob(
		context.Background(),
		"garantias/test",
		[]byte("esto no es una imagen"),
		1024,
	)

	if !errors.Is(err, domain.ErrImagenFormatoInvalido) {
		t.Fatalf(
			"guardarBlob() error = %v; se esperaba %v",
			err,
			domain.ErrImagenFormatoInvalido,
		)
	}
}

func TestGuardarBlob_JPEG(t *testing.T) {
	t.Parallel()

	storage := &blobStorageStub{}
	s := NewService(Deps{
		Storage: storage,
	})

	contenido := append(
		[]byte{0xff, 0xd8, 0xff, 0xe0},
		bytes.Repeat([]byte{0}, 32)...,
	)

	ruta, err := s.guardarBlob(
		context.Background(),
		"garantias/abc",
		contenido,
		1024,
	)
	if err != nil {
		t.Fatalf("guardarBlob() error inesperado: %v", err)
	}

	if !strings.HasPrefix(ruta, "garantias/abc/") {
		t.Fatalf("ruta = %q; prefijo incorrecto", ruta)
	}

	if !strings.HasSuffix(ruta, ".jpg") {
		t.Fatalf("ruta = %q; se esperaba extensión .jpg", ruta)
	}

	if storage.storedKey != ruta {
		t.Fatalf("Store key = %q; se esperaba %q", storage.storedKey, ruta)
	}

	if storage.storedContentType != "image/jpeg" {
		t.Fatalf(
			"content type = %q; se esperaba image/jpeg",
			storage.storedContentType,
		)
	}

	if storage.storedSize != int64(len(contenido)) {
		t.Fatalf(
			"size = %d; se esperaba %d",
			storage.storedSize,
			len(contenido),
		)
	}

	if !bytes.Equal(storage.storedBody, contenido) {
		t.Fatal("Store recibió contenido distinto")
	}
}

func TestGuardarBlob_PNG(t *testing.T) {
	t.Parallel()

	storage := &blobStorageStub{}
	s := NewService(Deps{
		Storage: storage,
	})

	contenido := append(
		[]byte{
			0x89, 0x50, 0x4e, 0x47,
			0x0d, 0x0a, 0x1a, 0x0a,
		},
		bytes.Repeat([]byte{0}, 32)...,
	)

	ruta, err := s.guardarBlob(
		context.Background(),
		"garantias/abc/",
		contenido,
		1024,
	)
	if err != nil {
		t.Fatalf("guardarBlob() error inesperado: %v", err)
	}

	if !strings.HasPrefix(ruta, "garantias/abc/") {
		t.Fatalf("ruta = %q; prefijo incorrecto", ruta)
	}

	if strings.Contains(ruta, "//") {
		t.Fatalf("ruta = %q; contiene //", ruta)
	}

	if !strings.HasSuffix(ruta, ".png") {
		t.Fatalf("ruta = %q; se esperaba extensión .png", ruta)
	}

	if storage.storedContentType != "image/png" {
		t.Fatalf(
			"content type = %q; se esperaba image/png",
			storage.storedContentType,
		)
	}
}

func TestGuardarBlob_ErrorStorage(t *testing.T) {
	t.Parallel()

	errStorage := errors.New("storage caido")

	s := NewService(Deps{
		Storage: &blobStorageStub{
			storeErr: errStorage,
		},
	})

	contenido := append(
		[]byte{0xff, 0xd8, 0xff, 0xe0},
		bytes.Repeat([]byte{0}, 32)...,
	)

	_, err := s.guardarBlob(
		context.Background(),
		"garantias/test",
		contenido,
		1024,
	)

	if !errors.Is(err, errStorage) {
		t.Fatalf("guardarBlob() error = %v; se esperaba %v", err, errStorage)
	}
}

func TestGuardarBlob_GeneraRutaNuevaCadaIntento(t *testing.T) {
	t.Parallel()

	s := NewService(Deps{
		Storage: &blobStorageStub{},
	})

	contenido := append(
		[]byte{0xff, 0xd8, 0xff, 0xe0},
		bytes.Repeat([]byte{0}, 32)...,
	)

	ruta1, err := s.guardarBlob(
		context.Background(),
		"garantias/test",
		contenido,
		1024,
	)
	if err != nil {
		t.Fatalf("primer guardarBlob(): %v", err)
	}

	ruta2, err := s.guardarBlob(
		context.Background(),
		"garantias/test",
		contenido,
		1024,
	)
	if err != nil {
		t.Fatalf("segundo guardarBlob(): %v", err)
	}

	if ruta1 == ruta2 {
		t.Fatalf("dos intentos generaron la misma ruta: %q", ruta1)
	}
}

func TestDescartarBlob_ConservaSiFilaApuntaALaRuta(t *testing.T) {
	t.Parallel()

	imagenID := uuid.New()
	ruta := "garantias/test/conservar.jpg"

	storage := &blobStorageStub{}
	repo := &blobImagenRepoStub{
		imagen: nuevaImagenBlob(imagenID, ruta),
	}

	s := NewService(Deps{
		ImagenRepo: repo,
		Storage:    storage,
	})

	s.descartarBlobSiNoQuedo(
		context.Background(),
		imagenID,
		ruta,
	)

	if len(storage.deletedKeys) != 0 {
		t.Fatalf(
			"Delete fue llamado cuando debía conservar el blob: %v",
			storage.deletedKeys,
		)
	}
}

func TestDescartarBlob_EliminaSiImagenNoExiste(t *testing.T) {
	t.Parallel()

	imagenID := uuid.New()
	ruta := "garantias/test/eliminar.jpg"

	storage := &blobStorageStub{}
	repo := &blobImagenRepoStub{
		obtenerErr: domain.ErrImagenNoEncontrada,
	}

	s := NewService(Deps{
		ImagenRepo: repo,
		Storage:    storage,
	})

	s.descartarBlobSiNoQuedo(
		context.Background(),
		imagenID,
		ruta,
	)

	if len(storage.deletedKeys) != 1 {
		t.Fatalf(
			"Delete llamados = %d; se esperaba 1",
			len(storage.deletedKeys),
		)
	}

	if storage.deletedKeys[0] != ruta {
		t.Fatalf(
			"Delete key = %q; se esperaba %q",
			storage.deletedKeys[0],
			ruta,
		)
	}
}

func TestDescartarBlob_EliminaSiFilaApuntaAOtraRuta(t *testing.T) {
	t.Parallel()

	imagenID := uuid.New()
	rutaNueva := "garantias/test/nueva.jpg"
	rutaPersistida := "garantias/test/anterior.jpg"

	storage := &blobStorageStub{}
	repo := &blobImagenRepoStub{
		imagen: nuevaImagenBlob(imagenID, rutaPersistida),
	}

	s := NewService(Deps{
		ImagenRepo: repo,
		Storage:    storage,
	})

	s.descartarBlobSiNoQuedo(
		context.Background(),
		imagenID,
		rutaNueva,
	)

	if len(storage.deletedKeys) != 1 {
		t.Fatalf(
			"Delete llamados = %d; se esperaba 1",
			len(storage.deletedKeys),
		)
	}

	if storage.deletedKeys[0] != rutaNueva {
		t.Fatalf(
			"Delete key = %q; se esperaba %q",
			storage.deletedKeys[0],
			rutaNueva,
		)
	}
}

func TestDescartarBlob_NoEliminaSiFallaConsulta(t *testing.T) {
	t.Parallel()

	imagenID := uuid.New()
	ruta := "garantias/test/no-eliminar.jpg"

	storage := &blobStorageStub{}
	repo := &blobImagenRepoStub{
		obtenerErr: errors.New("firebird no disponible"),
	}

	s := NewService(Deps{
		ImagenRepo: repo,
		Storage:    storage,
	})

	s.descartarBlobSiNoQuedo(
		context.Background(),
		imagenID,
		ruta,
	)

	if len(storage.deletedKeys) != 0 {
		t.Fatalf(
			"Delete fue llamado después de error de consulta: %v",
			storage.deletedKeys,
		)
	}
}

func TestDescartarBlob_ErrorDeleteNoPropaga(t *testing.T) {
	t.Parallel()

	imagenID := uuid.New()
	ruta := "garantias/test/delete-falla.jpg"

	storage := &blobStorageStub{
		deleteErr: errors.New("no se pudo borrar"),
	}

	repo := &blobImagenRepoStub{
		obtenerErr: domain.ErrImagenNoEncontrada,
	}

	s := NewService(Deps{
		ImagenRepo: repo,
		Storage:    storage,
	})

	// descartarBlobSiNoQuedo es best-effort: el error de Delete se registra
	// pero no se devuelve ni provoca panic.
	s.descartarBlobSiNoQuedo(
		context.Background(),
		imagenID,
		ruta,
	)

	if len(storage.deletedKeys) != 1 {
		t.Fatalf(
			"Delete llamados = %d; se esperaba 1",
			len(storage.deletedKeys),
		)
	}
}

func TestDescartarBlobSiNoQuedo_MismaRutaConservaBlob(t *testing.T) {
	t.Parallel()

	imagenID := uuid.New()
	ruta := "garantias/test/evidencia.jpg"

	storage := &blobStorageStub{}
	imagenes := &blobImagenRepoStub{
		imagen: nuevaImagenBlob(imagenID, ruta),
	}

	s := NewService(Deps{
		ImagenRepo: imagenes,
		Storage:    storage,
	})

	s.descartarBlobSiNoQuedo(
		context.Background(),
		imagenID,
		ruta,
	)

	if len(storage.deletedKeys) != 0 {
		t.Fatalf(
			"Delete fue llamado %d veces; se esperaba 0",
			len(storage.deletedKeys),
		)
	}
}

func TestDescartarBlobSiNoQuedo_RutaDistintaEliminaBlobNuevo(t *testing.T) {
	t.Parallel()

	imagenID := uuid.New()
	rutaNueva := "garantias/test/nueva.jpg"
	rutaPersistida := "garantias/test/existente.jpg"

	storage := &blobStorageStub{}
	imagenes := &blobImagenRepoStub{
		imagen: nuevaImagenBlob(imagenID, rutaPersistida),
	}

	s := NewService(Deps{
		ImagenRepo: imagenes,
		Storage:    storage,
	})

	s.descartarBlobSiNoQuedo(
		context.Background(),
		imagenID,
		rutaNueva,
	)

	if len(storage.deletedKeys) != 1 {
		t.Fatalf(
			"Delete fue llamado %d veces; se esperaba 1",
			len(storage.deletedKeys),
		)
	}

	if storage.deletedKeys[0] != rutaNueva {
		t.Fatalf(
			"Delete key = %q; se esperaba %q",
			storage.deletedKeys[0],
			rutaNueva,
		)
	}
}

func TestDescartarBlobSiNoQuedo_ImagenNoEncontradaEliminaBlob(t *testing.T) {
	t.Parallel()

	imagenID := uuid.New()
	ruta := "garantias/test/no-persistida.jpg"

	storage := &blobStorageStub{}
	imagenes := &blobImagenRepoStub{
		obtenerErr: domain.ErrImagenNoEncontrada,
	}

	s := NewService(Deps{
		ImagenRepo: imagenes,
		Storage:    storage,
	})

	s.descartarBlobSiNoQuedo(
		context.Background(),
		imagenID,
		ruta,
	)

	if len(storage.deletedKeys) != 1 {
		t.Fatalf(
			"Delete fue llamado %d veces; se esperaba 1",
			len(storage.deletedKeys),
		)
	}

	if storage.deletedKeys[0] != ruta {
		t.Fatalf(
			"Delete key = %q; se esperaba %q",
			storage.deletedKeys[0],
			ruta,
		)
	}
}

func TestDescartarBlobSiNoQuedo_ErrorRepositorioNoElimina(t *testing.T) {
	t.Parallel()

	imagenID := uuid.New()
	ruta := "garantias/test/evidencia.jpg"
	errRepo := errors.New("fallo inesperado de repositorio")

	storage := &blobStorageStub{}
	imagenes := &blobImagenRepoStub{
		obtenerErr: errRepo,
	}

	s := NewService(Deps{
		ImagenRepo: imagenes,
		Storage:    storage,
	})

	s.descartarBlobSiNoQuedo(
		context.Background(),
		imagenID,
		ruta,
	)

	if len(storage.deletedKeys) != 0 {
		t.Fatalf(
			"Delete fue llamado %d veces; se esperaba 0",
			len(storage.deletedKeys),
		)
	}
}

func TestDescartarBlobSiNoQuedo_ErrorDeleteNoPropaga(t *testing.T) {
	t.Parallel()

	imagenID := uuid.New()
	ruta := "garantias/test/evidencia.jpg"
	errDelete := errors.New("fallo al borrar")

	storage := &blobStorageStub{
		deleteErr: errDelete,
	}
	imagenes := &blobImagenRepoStub{
		obtenerErr: domain.ErrImagenNoEncontrada,
	}

	s := NewService(Deps{
		ImagenRepo: imagenes,
		Storage:    storage,
	})

	s.descartarBlobSiNoQuedo(
		context.Background(),
		imagenID,
		ruta,
	)

	if len(storage.deletedKeys) != 1 {
		t.Fatalf(
			"Delete fue llamado %d veces; se esperaba 1",
			len(storage.deletedKeys),
		)
	}

	if storage.deletedKeys[0] != ruta {
		t.Fatalf(
			"Delete key = %q; se esperaba %q",
			storage.deletedKeys[0],
			ruta,
		)
	}
}
