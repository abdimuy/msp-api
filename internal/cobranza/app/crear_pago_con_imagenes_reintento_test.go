//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	cobranzaoutbound "github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// localTestStorage implementa el puerto StorageProvider de cobranza sin
// importar el modulo de ventas, usando un directorio base temporal.
type localTestStorage struct {
	baseDir string
}

func (s *localTestStorage) Store(_ context.Context, key, contentType string, sizeBytes int64, body io.Reader) error {
	fullPath := filepath.Join(s.baseDir, key)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, body)
	return err
}

func (s *localTestStorage) Get(_ context.Context, key string) (cobranzaoutbound.StorageObject, error) {
	fullPath := filepath.Join(s.baseDir, key)
	info, err := os.Stat(fullPath)
	if err != nil {
		return cobranzaoutbound.StorageObject{}, err
	}
	f, err := os.Open(fullPath)
	if err != nil {
		return cobranzaoutbound.StorageObject{}, err
	}
	return cobranzaoutbound.StorageObject{
		Body:      f,
		SizeBytes: info.Size(),
	}, nil
}

func (s *localTestStorage) Delete(_ context.Context, key string) error {
	fullPath := filepath.Join(s.baseDir, key)
	return os.Remove(fullPath)
}

func newSvcConRealStorage(
	t *testing.T,
	now time.Time,
	baseDir string,
	pagosRepo *fakePagosRecibidosRepo,
	imagenesRepo *fakePagosImagenesRepo,
) *app.Service {
	t.Helper()
	adapter := &localTestStorage{baseDir: baseDir}
	saldos := seedCargoSaldo(t)
	writer := &fakeMicrosipPagoWriter{result: validWriterResult()}

	svc := app.NewService(
		saldos,
		newFakePagosRepo(),
		nil, // ventas — unused
		fixedClock{T: now},
		pagosRepo,
		imagenesRepo,
		writer,
		adapter,
		nil, // imageProc not needed for PDF
		fakeTxRunner{},
	)
	return svc
}

// Evidencia E-COB-019: documenta que el reintento borra el comprobante original.
func TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	baseDir := t.TempDir()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	svc := newSvcConRealStorage(t, now, baseDir, pagosRepo, imagenesRepo)

	in := baseCrearInput(now)
	payload := []byte("%PDF-1.4 payment receipt proof content")
	imgID := uuid.New()
	storageKey := "pagos/" + in.ID.String() + "/" + imgID.String() + ".pdf"

	imgFirst := app.ImagenUploadInput{
		ImagenID:    imgID,
		StorageKind: domain.StorageKindFilesystem,
		StorageKey:  storageKey,
		Mime:        domain.MimePDF,
		SizeBytes:   int64(len(payload)),
		Body:        bytes.NewReader(payload),
	}
	by := uuid.New()

	// 1. Primera llamada: creación exitosa del pago con imagen.
	firstPago, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{imgFirst}, by)
	require.NoError(t, err)
	require.NotNil(t, firstPago)
	assert.Equal(t, in.ID, firstPago.ID())

	// Verificar físicamente en disco que el archivo existe en t.TempDir().
	targetPath := filepath.Join(baseDir, storageKey)
	info, err := os.Stat(targetPath)
	require.NoError(t, err, "el archivo físico debe existir en t.TempDir() tras la primera llamada")
	assert.False(t, info.IsDir())
	assert.Equal(t, int64(len(payload)), info.Size())

	// 2. Segunda llamada con los MISMOS datos exactos (simulando un reintento).
	imgRetry := app.ImagenUploadInput{
		ImagenID:    imgID,
		StorageKind: domain.StorageKindFilesystem,
		StorageKey:  storageKey,
		Mime:        domain.MimePDF,
		SizeBytes:   int64(len(payload)),
		Body:        bytes.NewReader(payload),
	}

	secondPago, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{imgRetry}, by)
	require.NoError(t, err, "el error devuelto debe ser nil en un reintento idempotente")
	require.NotNil(t, secondPago)
	assert.Equal(t, firstPago.ID(), secondPago.ID(), "debe retornar el pago existente")

	// 3. Verificar que el archivo físico YA NO EXISTE (documentación del defecto E-COB-019).
	_, statErr := os.Stat(targetPath)
	require.Error(t, statErr, "Evidencia E-COB-019: os.Stat debe devolver error porque el archivo físico ya no existe tras el reintento debido al borrado de cleanupBlobs")
	require.True(t, errors.Is(statErr, os.ErrNotExist) || os.IsNotExist(statErr))
}

func TestCrearPagoConImagenes_Normal_ConservaElComprobante(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	baseDir := t.TempDir()

	pagosRepo := newFakePagosRecibidosRepo()
	imagenesRepo := newFakePagosImagenesRepo()
	svc := newSvcConRealStorage(t, now, baseDir, pagosRepo, imagenesRepo)

	in := baseCrearInput(now)
	payload := []byte("%PDF-1.4 normal payment single call")
	imgID := uuid.New()
	storageKey := "pagos/" + in.ID.String() + "/" + imgID.String() + ".pdf"

	img := app.ImagenUploadInput{
		ImagenID:    imgID,
		StorageKind: domain.StorageKindFilesystem,
		StorageKey:  storageKey,
		Mime:        domain.MimePDF,
		SizeBytes:   int64(len(payload)),
		Body:        bytes.NewReader(payload),
	}
	by := uuid.New()

	pago, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img}, by)
	require.NoError(t, err)
	require.NotNil(t, pago)
	assert.Equal(t, in.ID, pago.ID())

	// Verificar que el archivo físico existe al final.
	targetPath := filepath.Join(baseDir, storageKey)
	info, err := os.Stat(targetPath)
	require.NoError(t, err, "el archivo físico debe existir al final de una creación normal")
	assert.False(t, info.IsDir())
	assert.Equal(t, int64(len(payload)), info.Size())
}
